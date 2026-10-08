//go:build !windows

package agent

import (
	"bufio"
	"net"
	"os"
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
