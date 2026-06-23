package server

import (
	"context"
	"strings"
	"testing"
	"time"
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
