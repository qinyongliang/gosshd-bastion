package server

import (
	"context"
	"errors"
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

func TestTerminalIntegrationPromptEventClearsBusy(t *testing.T) {
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

	session.writeOutput("output", []byte("\x1b]633;C\a"))
	if !session.shellBusy {
		t.Fatal("command-start event should mark shell busy")
	}
	session.writeOutput("output", []byte("\x1b]633;A\a"))
	if session.shellBusy {
		t.Fatal("prompt event should clear shell busy")
	}
	select {
	case code := <-waiter.done:
		t.Fatalf("prompt event should not complete command waiter, got exit %d", code)
	default:
	}
}

func TestParseTerminalIntegrationPromptEvent(t *testing.T) {
	event, ok := parseTerminalIntegrationEvent("A")
	if !ok || event.Kind != "A" {
		t.Fatalf("prompt event parse failed: %+v ok=%t", event, ok)
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

func TestCollectCommandOutputStripsCommandEchoFromReturnedOutput(t *testing.T) {
	waiter := &terminalCommandWaiter{
		command: "docker exec -it",
		output:  make(chan string, 8),
		done:    make(chan int, 1),
	}
	waiter.output <- "docker exec -it\r\n"
	waiter.output <- "docker: 'docker exec' requires at least 2 arguments\r\n"
	waiter.done <- 1

	result, err := collectCommandOutput(context.Background(), context.Background(), waiter)
	if err != nil {
		t.Fatalf("collect command output: %v", err)
	}
	if strings.Contains(result.Output, "docker exec -it\r\n") {
		t.Fatalf("returned output should not include command echo: %q", result.Output)
	}
	if !strings.Contains(result.Output, "requires at least 2 arguments") {
		t.Fatalf("returned output missing command result: %q", result.Output)
	}
	if result.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", result.ExitCode)
	}
}

func TestStripCommandEchoPreservesSimilarOutput(t *testing.T) {
	output := "docker exec -it failed before echo\r\n"
	if got := stripCommandEcho(output, "docker exec -it"); got != output {
		t.Fatalf("output should be preserved:\n got: %q\nwant: %q", got, output)
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
	if strings.Contains(script, `return "$__gosshd_rc"`) {
		t.Fatalf("script should not re-trigger DEBUG trap from precmd return: %q", script)
	}
}

func TestEarliestOnlineForUserTargetRequiresReadyClient(t *testing.T) {
	manager := newTerminalSessionManager()
	target := store.SSHTarget{ID: "target-1", Alias: "box"}
	oldest := manager.create("oldest", "user-1", target, "127.0.0.1", 80, 24, nil)
	newer := manager.create("newer", "user-1", target, "127.0.0.1", 80, 24, nil)
	oldest.startedAt = time.Now().Add(-time.Second)
	newer.startedAt = time.Now()
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

	oldest.writeOutput("output", []byte("\x1b]633;C\a"))
	if got := manager.earliestOnlineForUserTarget("user-1", target.ID); got != oldest {
		t.Fatalf("expected oldest online session even while shell-busy, got %v", got)
	}
	oldest.writeOutput("output", []byte("\x1b]633;D;0\a"))
	if got := manager.earliestOnlineForUserTarget("user-1", target.ID); got != oldest {
		t.Fatalf("expected oldest session after shell completion, got %v", got)
	}
}

func TestEarliestOnlineForUserTargetDiagnosticsExplainMisses(t *testing.T) {
	manager := newTerminalSessionManager()
	target := store.SSHTarget{ID: "target-1", Alias: "box"}
	noClient := manager.create("no-client", "user-1", target, "127.0.0.1", 80, 24, nil)
	closed := manager.create("closed", "user-1", target, "127.0.0.1", 80, 24, nil)
	stale := manager.create("stale", "user-1", target, "127.0.0.1", 80, 24, nil)
	ready := manager.create("ready", "user-1", target, "127.0.0.1", 80, 24, nil)
	defer noClient.close("")
	defer closed.close("")
	defer stale.close("")
	defer ready.close("")

	noClient.input = io.Discard
	closed.input = io.Discard
	closed.clients[&terminalWSWriter{}] = true
	closed.closed = true
	stale.input = io.Discard
	stale.clients[&terminalWSWriter{}] = true
	stale.lastHeartbeat = time.Now().Add(-terminalSessionHeartbeatTimeout - time.Second)
	ready.input = io.Discard
	ready.clients[&terminalWSWriter{}] = true

	lookup := manager.earliestOnlineForUserTargetWithDiagnostics("user-1", target.ID)
	if lookup.Session != ready {
		t.Fatalf("expected ready session, got %v", lookup.Session)
	}
	reasons := map[string]string{}
	for _, snapshot := range lookup.Snapshots {
		reasons[snapshot.ID] = snapshot.Reason
	}
	for id, want := range map[string]string{
		"no-client": "no-client",
		"closed":    "closed",
		"stale":     "stale-heartbeat",
		"ready":     "candidate",
	} {
		if got := reasons[id]; got != want {
			t.Fatalf("session %s reason = %q, want %q; snapshots=%+v", id, got, want, lookup.Snapshots)
		}
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

func TestTrySendCommandDoesNotSendWhenShellBusy(t *testing.T) {
	input := &strings.Builder{}
	session := &terminalSession{
		id:      "session-1",
		ctx:     context.Background(),
		input:   input,
		clients: map[*terminalWSWriter]bool{},
		screen:  newTerminalScreenBuffer(24),
	}
	session.writeOutput("output", []byte("\x1b]633;C\a"))

	attempt := session.trySendCommand(context.Background(), "echo busy")
	if !attempt.Acquired || attempt.Sent || !errors.Is(attempt.Err, errTerminalSessionBusy) {
		t.Fatalf("busy shell should be acquired but not sent, attempt=%+v", attempt)
	}
	if input.Len() != 0 {
		t.Fatalf("busy shell should not receive command input, got %q", input.String())
	}
}

func TestRunCommandInTerminalSessionNonBlockingFallsBackWhenShellBusy(t *testing.T) {
	input := &strings.Builder{}
	session := &terminalSession{
		id:       "session-1",
		userID:   "user-1",
		target:   store.SSHTarget{ID: "target-1"},
		sourceIP: "127.0.0.1",
		ctx:      context.Background(),
		input:    input,
		clients:  map[*terminalWSWriter]bool{},
		screen:   newTerminalScreenBuffer(24),
	}
	session.writeOutput("output", []byte("\x1b]633;C\a"))

	run := (&App{}).runCommandInTerminalSession(context.Background(), session, "echo busy", terminalSessionCommandOptions{
		UserID:           "user-1",
		NonBlocking:      true,
		SkipPolicyReview: true,
	})
	if run.Routed || run.Err != nil {
		t.Fatalf("busy session should fall back without surfacing an error: %+v", run)
	}
	if input.Len() != 0 {
		t.Fatalf("busy shell should not receive command input, got %q", input.String())
	}
}

func TestRunCommandInTerminalSessionBlockingReportsShellBusyForMCP(t *testing.T) {
	input := &strings.Builder{}
	session := &terminalSession{
		id:       "session-1",
		userID:   "user-1",
		target:   store.SSHTarget{ID: "target-1"},
		sourceIP: "127.0.0.1",
		ctx:      context.Background(),
		input:    input,
		clients:  map[*terminalWSWriter]bool{},
		screen:   newTerminalScreenBuffer(24),
	}
	session.writeOutput("output", []byte("\x1b]633;C\a"))

	run := (&App{}).runCommandInTerminalSession(context.Background(), session, "echo busy", terminalSessionCommandOptions{
		UserID:           "user-1",
		SkipPolicyReview: true,
	})
	if !run.Routed || run.Err == nil || !errors.Is(run.Err, errTerminalSessionBusy) {
		t.Fatalf("blocking session command should surface busy error, run=%+v", run)
	}
	if input.Len() != 0 {
		t.Fatalf("busy shell should not receive command input, got %q", input.String())
	}
}

type carriageReturnCommandWriter struct {
	session *terminalSession
	input   strings.Builder
}

func (w *carriageReturnCommandWriter) Write(data []byte) (int, error) {
	w.input.Write(data)
	if strings.Contains(string(data), "\r") {
		go func() {
			w.session.writeOutput("output", []byte("command output\r\n"))
			w.session.writeOutput("output", []byte("\x1b]633;D;0\a"))
		}()
	}
	return len(data), nil
}

func TestTrySendCommandUsesCarriageReturnAndReturnsOutput(t *testing.T) {
	session := &terminalSession{
		id:      "session-1",
		ctx:     context.Background(),
		clients: map[*terminalWSWriter]bool{},
		screen:  newTerminalScreenBuffer(24),
	}
	input := &carriageReturnCommandWriter{session: session}
	session.input = input

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	attempt := session.trySendCommand(ctx, "echo once")
	if !attempt.Acquired || !attempt.Sent || attempt.Err != nil {
		t.Fatalf("expected command to be sent and completed, attempt=%+v", attempt)
	}
	if got := input.input.String(); got != " echo once\r" {
		t.Fatalf("command input = %q, want %q", got, " echo once\r")
	}
	if attempt.Result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", attempt.Result.ExitCode)
	}
	if !strings.Contains(attempt.Result.Output, "command output") {
		t.Fatalf("output missing command result: %q", attempt.Result.Output)
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
	if got := input.String(); got != " echo once\r" {
		t.Fatalf("command input = %q, want %q", got, " echo once\r")
	}
}

func TestRunCommandInTerminalSessionTimeoutReleasesCommandLock(t *testing.T) {
	input := &strings.Builder{}
	session := &terminalSession{
		id:      "session-1",
		ctx:     context.Background(),
		input:   input,
		clients: map[*terminalWSWriter]bool{},
		screen:  newTerminalScreenBuffer(24),
	}

	run := (&App{}).runCommandInTerminalSession(context.Background(), session, "echo no-finish-event", terminalSessionCommandOptions{
		UserID:           "user-1",
		SkipPolicyReview: true,
		WaitTimeout:      25 * time.Millisecond,
	})
	if !run.Routed || !run.Allowed || run.Err == nil {
		t.Fatalf("expected routed command to time out while waiting for terminal integration, run=%+v", run)
	}
	if got := input.String(); got != " echo no-finish-event\r" {
		t.Fatalf("command input = %q, want %q", got, " echo no-finish-event\r")
	}

	if !session.commandMu.TryLock() {
		t.Fatal("command lock should be released after timeout")
	}
	session.commandMu.Unlock()
}

func TestHistorySuppressedTerminalCommandAddsLeadingSpace(t *testing.T) {
	if got := historySuppressedTerminalCommand("echo hidden"); got != " echo hidden" {
		t.Fatalf("history suppressed command = %q", got)
	}
	if got := historySuppressedTerminalCommand(" echo already"); got != " echo already" {
		t.Fatalf("history suppressed command should preserve existing leading space, got %q", got)
	}
}
