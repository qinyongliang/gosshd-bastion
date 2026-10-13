//go:build !windows

package agent

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestAgentBashRCDoesNotExportPromptHook(t *testing.T) {
	if script := agentBashRC(); !strings.Contains(script, "export -n PROMPT_COMMAND") {
		t.Fatalf("agent bashrc should not export the prompt hook to tmux child shells: %q", script)
	}
}

func TestExecDisconnectKillsCommandAndItsChildren(t *testing.T) {
	root := t.TempDir()
	clientConn, agentConn := net.Pipe()
	defer clientConn.Close()
	defer agentConn.Close()
	client := &Client{cfg: Config{Shell: "/bin/sh", Root: root}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.handleCommand(agentConn, bufio.NewReader(agentConn), protocol.StreamRequest{
			Type: protocol.StreamExec, Width: 80, Height: 24,
			Command: "trap '' HUP INT TERM; sleep 30 & echo ready; wait; touch completed",
		})
	}()
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(clientConn)
	response, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil || !response.OK {
		t.Fatalf("start command: %+v, %v", response, err)
	}
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(frame.Data), "ready") {
			break
		}
	}
	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnected command or child still holds the PTY open")
	}
	if _, err := os.Stat(filepath.Join(root, "completed")); !os.IsNotExist(err) {
		t.Fatalf("stopped command continued executing: %v", err)
	}
}

func TestAgentBashDirectoryIntegrationReportsInitialHomeAndCD(t *testing.T) {
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work;中文")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("SHELL", shell)
	clientConn, agentConn := net.Pipe()
	defer clientConn.Close()
	defer agentConn.Close()
	client := &Client{cfg: Config{Shell: shell, Root: root}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.handleCommand(agentConn, bufio.NewReader(agentConn), protocol.StreamRequest{Type: protocol.StreamShell, Width: 80, Height: 24})
	}()
	_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(clientConn)
	response, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil || !response.OK {
		t.Fatalf("start shell: %+v, %v", response, err)
	}
	waitDirectory := func(want string) {
		t.Helper()
		var output strings.Builder
		for !strings.Contains(output.String(), "\x1b]633;P;Cwd="+want+"\a") {
			frame, err := protocol.ReadFrame(reader)
			if err != nil {
				t.Fatalf("waiting for cwd %q: %v\n%s", want, err, output.String())
			}
			output.Write(frame.Data)
		}
	}
	waitDirectory(root)
	if err := protocol.WriteFrame(clientConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("cd 'work;中文'\r")}); err != nil {
		t.Fatal(err)
	}
	waitDirectory(work)
	if err := protocol.WriteFrame(clientConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("exit\r")}); err != nil {
		t.Fatal(err)
	}
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type == protocol.FrameExit {
			break
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shell did not exit")
	}
}
