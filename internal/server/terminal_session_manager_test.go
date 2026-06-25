package server

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestTerminalIntegrationStripsOSCAndParsesCompletion(t *testing.T) {
	session := &terminalSession{
		screen:  newTerminalScreenBuffer(24),
		clients: map[*terminalWSWriter]bool{},
	}
	waiter := &terminalCommandWaiter{
		id:     "cmd-1",
		output: make(chan string, 8),
		done:   make(chan int, 1),
	}
	session.commandWaiters = []*terminalCommandWaiter{waiter}

	session.writeOutput("output", []byte("hello"))
	session.writeOutput("output", []byte("\x1b]633;D;cmd-1;2\a"))

	result, err := collectCommandOutput(context.Background(), context.Background(), waiter)
	if err != nil {
		t.Fatalf("collect command output: %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q, want %q", result.Output, "hello")
	}
	if result.ExitCode != 2 {
		t.Fatalf("exit code = %d, want 2", result.ExitCode)
	}
	if got := session.currentScreen(); strings.Contains(got, "633;D") {
		t.Fatalf("screen leaked integration sequence: %q", got)
	}
}

func TestTerminalIntegrationParsesSplitOSCSequence(t *testing.T) {
	session := &terminalSession{
		screen:  newTerminalScreenBuffer(24),
		clients: map[*terminalWSWriter]bool{},
	}
	waiter := &terminalCommandWaiter{
		id:     "cmd-2",
		output: make(chan string, 8),
		done:   make(chan int, 1),
	}
	session.commandWaiters = []*terminalCommandWaiter{waiter}

	session.writeOutput("output", []byte("before\x1b]633;D;cmd"))
	session.writeOutput("output", []byte("-2;0\aafter"))

	result, err := collectCommandOutput(context.Background(), context.Background(), waiter)
	if err != nil {
		t.Fatalf("collect command output: %v", err)
	}
	if result.Output != "beforeafter" {
		t.Fatalf("output = %q, want %q", result.Output, "beforeafter")
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestCollectCommandOutputWaitsForCompletionEvent(t *testing.T) {
	waiter := &terminalCommandWaiter{
		id:     "cmd-3",
		output: make(chan string, 8),
		done:   make(chan int, 1),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		waiter.output <- "long running\n"
		time.Sleep(25 * time.Millisecond)
		waiter.output <- "done\n"
		waiter.done <- 0
	}()

	result, err := collectCommandOutput(ctx, context.Background(), waiter)
	if err != nil {
		t.Fatalf("collect command output: %v", err)
	}
	if result.Output != "long running\ndone\n" {
		t.Fatalf("output = %q", result.Output)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestTerminalScreenBufferPreservesCRLFLines(t *testing.T) {
	buffer := newTerminalScreenBuffer(8)

	buffer.write([]byte("line one\r\nline two\r\n[root@host ~]# "))

	screen := buffer.String()
	if !strings.Contains(screen, "line one") || !strings.Contains(screen, "line two") {
		t.Fatalf("screen lost CRLF output lines: %q", screen)
	}
	if !strings.Contains(screen, "[root@host ~]# ") {
		t.Fatalf("screen lost current prompt: %q", screen)
	}
}

func TestBashShellIntegrationCommandInstallsHooks(t *testing.T) {
	script := bashShellIntegrationCommand()
	if strings.Contains(script, "__gosshd_mcp_rc") || strings.Contains(script, "GOSSHD_MCP_DONE") {
		t.Fatalf("script should not include the old command wrapper: %q", script)
	}
	if !strings.Contains(script, "trap '__gosshd_preexec' DEBUG") {
		t.Fatalf("script missing DEBUG preexec hook: %q", script)
	}
	if !strings.Contains(script, "printf '\\033]633;D;%s\\007'") {
		t.Fatalf("script missing command-finish OSC: %q", script)
	}
}

func TestEarliestOnlineForUserTargetRequiresReadyClient(t *testing.T) {
	manager := newTerminalSessionManager()
	target := store.SSHTarget{ID: "target-1", Alias: "box"}
	oldest := manager.create("oldest", "user-1", target, "127.0.0.1", 80, 24, nil)
	newer := manager.create("newer", "user-1", target, "127.0.0.1", 80, 24, nil)
	defer oldest.close("")
	defer newer.close("")

	if got := manager.earliestOnlineForUserTarget("user-1", target.ID); got != nil {
		t.Fatalf("session without input/client should not be selected: %+v", got)
	}
	oldest.input = io.Discard
	oldest.clients[&terminalWSWriter{}] = true
	newer.input = io.Discard
	newer.clients[&terminalWSWriter{}] = true

	if got := manager.earliestOnlineForUserTarget("user-1", target.ID); got != oldest {
		t.Fatalf("expected oldest ready session, got %v", got)
	}
}

func TestTrySendCommandDoesNotQueueWhenBusy(t *testing.T) {
	session := &terminalSession{
		id:      "session-1",
		ctx:     context.Background(),
		input:   &strings.Builder{},
		clients: map[*terminalWSWriter]bool{},
		screen:  newTerminalScreenBuffer(24),
	}
	session.commandMu.Lock()
	defer session.commandMu.Unlock()

	attempt := session.trySendCommand(context.Background(), "echo busy")
	if attempt.Err != nil || attempt.Acquired || attempt.Sent {
		t.Fatalf("busy command should not acquire lock, attempt=%+v", attempt)
	}
}

func TestTrySendCommandReportsSentWhenWaitingFails(t *testing.T) {
	input := &strings.Builder{}
	sessionCtx, cancel := context.WithCancel(context.Background())
	session := &terminalSession{
		id:      "session-1",
		ctx:     sessionCtx,
		input:   input,
		clients: map[*terminalWSWriter]bool{},
		screen:  newTerminalScreenBuffer(24),
	}
	cancel()

	attempt := session.trySendCommand(context.Background(), "echo once")
	if !attempt.Acquired || !attempt.Sent || attempt.Err == nil {
		t.Fatalf("expected sent command with wait error, attempt=%+v", attempt)
	}
	if got := input.String(); got != "echo once\n" {
		t.Fatalf("command input = %q, want %q", got, "echo once\n")
	}
}
