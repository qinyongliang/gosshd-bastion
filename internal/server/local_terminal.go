package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func (a *App) handleLocalTerminalWS(w http.ResponseWriter, r *http.Request, _ store.User) {
	if !a.cfg.ClientMode || !isLoopbackRequest(r) {
		writeError(w, http.StatusNotFound, "local terminal unavailable")
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	cmd := localShellCommand(ctx)
	configureLocalShellCommand(cmd)
	cmd.Dir = localShellDirectory()
	cmd.Env = localShellEnvironment(os.Environ())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = (&terminalWSWriter{ws: ws}).write(terminalWSMessage{Type: "error", Data: err.Error()})
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = (&terminalWSWriter{ws: ws}).write(terminalWSMessage{Type: "error", Data: err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = (&terminalWSWriter{ws: ws}).write(terminalWSMessage{Type: "error", Data: err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		_ = (&terminalWSWriter{ws: ws}).write(terminalWSMessage{Type: "error", Data: err.Error()})
		return
	}

	writer := &terminalWSWriter{ws: ws}
	done := make(chan int, 1)
	go streamLocalTerminalOutput(writer, stdout)
	go streamLocalTerminalOutput(writer, stderr)
	go func() {
		done <- localShellExitCode(cmd.Wait())
	}()

	_ = writer.write(terminalWSMessage{Type: "session", SessionID: "local"})
	_ = writer.write(terminalWSMessage{Type: "output", Data: "GOSSHD local terminal\r\nWorking directory: " + cmd.Dir + "\r\n\r\n"})

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer cancel()
		for {
			var msg terminalWSMessage
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Type {
			case "input":
				_, _ = io.WriteString(stdin, msg.Data)
			case "heartbeat", "resize":
			case "close":
				return
			}
		}
	}()

	select {
	case code := <-done:
		_ = writer.write(terminalWSMessage{Type: "exit", Code: code})
	case <-readerDone:
		_ = stdin.Close()
		terminateLocalShell(cmd)
		select {
		case code := <-done:
			_ = writer.write(terminalWSMessage{Type: "exit", Code: code})
		case <-time.After(2 * time.Second):
		}
	case <-ctx.Done():
		terminateLocalShell(cmd)
	}
}

func streamLocalTerminalOutput(writer *terminalWSWriter, pipe io.Reader) {
	reader := bufio.NewReader(pipe)
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			_ = writer.write(terminalWSMessage{Type: "output", Data: string(buffer[:n])})
		}
		if err != nil {
			return
		}
	}
}

func localShellCommand(ctx context.Context) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("powershell.exe"); err == nil {
			return exec.CommandContext(ctx, path, "-NoLogo", "-NoExit")
		}
		return exec.CommandContext(ctx, "cmd.exe")
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.CommandContext(ctx, shell, "-i")
}

func localShellDirectory() string {
	if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
		return dir
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}

func localShellEnvironment(env []string) []string {
	for _, item := range env {
		if strings.HasPrefix(item, "TERM=") {
			return env
		}
	}
	return append(env, "TERM=xterm-256color")
}

func terminateLocalShell(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = err
	}
}

func localShellExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 255
}
