//go:build windows

package agent

import (
	"bufio"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestWindowsShellUsesConPTYForInteractiveInput(t *testing.T) {
	client, err := New(Config{
		Server: "http://qyl.my.to:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
		Shell:  "cmd.exe",
		Root:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	agentConn, peerConn := net.Pipe()
	defer agentConn.Close()
	defer peerConn.Close()
	_ = peerConn.SetDeadline(time.Now().Add(10 * time.Second))

	go client.handleCommand(agentConn, bufio.NewReader(agentConn), protocol.StreamRequest{
		Type:   protocol.StreamShell,
		Width:  100,
		Height: 30,
	})

	reader := bufio.NewReader(peerConn)
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("shell start failed: %s", resp.Error)
	}

	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("echo conpty-ok\r")}); err != nil {
		t.Fatal(err)
	}
	output := readFramesUntil(t, reader, "conpty-ok")
	if !strings.Contains(output, "conpty-ok") {
		t.Fatalf("interactive output missing command result: %q", output)
	}
	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("exit\r")}); err != nil {
		t.Fatal(err)
	}
	if code := readExitCode(t, reader); code != 0 {
		t.Fatalf("exit code mismatch: got %d want 0", code)
	}
}

func readFramesUntil(t *testing.T, reader *bufio.Reader, marker string) string {
	t.Helper()
	var out strings.Builder
	for out.Len() < 64*1024 {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		switch frame.Type {
		case protocol.FrameStdout, protocol.FrameStderr:
			out.Write(frame.Data)
			if strings.Contains(out.String(), marker) {
				return out.String()
			}
		case protocol.FrameExit:
			t.Fatalf("shell exited before %q appeared; output=%q code=%d", marker, out.String(), protocol.ExitCode(frame))
		}
	}
	t.Fatalf("marker %q not found in output prefix %q", marker, out.String())
	return out.String()
}

func readExitCode(t *testing.T, reader *bufio.Reader) int {
	t.Helper()
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type == protocol.FrameExit {
			return protocol.ExitCode(frame)
		}
	}
}
