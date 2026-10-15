//go:build windows && (amd64 || 386)

package agent

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

// Compile this test binary next to winpty.dll and winpty-agent.exe to exercise
// the real fallback even on hosts that also support ConPTY.
func TestWinPTYPromptIntegration(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "winpty.dll")); err != nil {
		t.Skip("run test binary alongside winpty.dll and winpty-agent.exe")
	}
	for _, test := range []struct {
		shell string
		width int
	}{{"cmd.exe", 80}, {"cmd.exe", 40}, {"powershell.exe", 80}} {
		t.Run(fmt.Sprintf("%s/%d", test.shell, test.width), func(t *testing.T) {
			root := t.TempDir()
			c, err := New(Config{Server: "http://agent.example.test:8880", IDFile: filepath.Join(t.TempDir(), "agent.json"), Shell: test.shell, Root: root})
			if err != nil {
				t.Fatal(err)
			}
			agentConn, peerConn := net.Pipe()
			defer agentConn.Close()
			defer peerConn.Close()
			_ = peerConn.SetDeadline(time.Now().Add(25 * time.Second))
			done := make(chan error, 1)
			go func() {
				done <- c.handleWinPTYShell(agentConn, bufio.NewReader(agentConn), protocol.StreamRequest{Type: protocol.StreamShell, Width: test.width, Height: 24})
				close(done)
			}()
			defer func() {
				_ = peerConn.SetDeadline(time.Now().Add(3 * time.Second))
				go io.Copy(io.Discard, peerConn)
				_ = protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("exit\r")})
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("WinPTY shell did not stop")
				}
			}()
			reader := bufio.NewReader(peerConn)
			response, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
			if err != nil {
				t.Fatal(err)
			}
			if !response.OK {
				t.Fatalf("start: %+v", response)
			}
			write := func(command string) {
				t.Helper()
				if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte(command + "\r")}); err != nil {
					t.Fatal(err)
				}
			}
			assertPrompt := func(output, cwd string) {
				t.Helper()
				for _, marker := range []string{"\x1b]633;D;", "\x1b]633;A\a", "\x1b]633;P;Cwd=" + cwd + "\a"} {
					if !strings.Contains(output, marker) {
						t.Errorf("missing %q in WinPTY output %q", marker, output)
					}
				}
				plain, _ := normalizedLegacyPrompt(output)
				if strings.Contains(plain, "?]633;") || strings.Contains(plain, "__GOSSHD_") {
					t.Errorf("private prompt leaked: %q", output)
				}
			}
			output := readWinPTYUntil(t, reader, func(s string) bool { text, _ := normalizedLegacyPrompt(s); return strings.Contains(text, root+">") })
			assertPrompt(output, root)
			write("echo first-result")
			output = readFramesUntil(t, reader, "\x1b]633;A\a")
			assertPrompt(output, root)
			text, _ := normalizedLegacyPrompt(output)
			// PowerShell may repaint the echoed input more than once.
			// Require both the command echo and its eventual result.
			if strings.Count(text, "first-result") < 2 {
				t.Errorf("command result missing: %q", output)
			}

			// Exercise prompts generated within the same cmd $T clock tick,
			// including a genuine screen clear rather than a resize replay.
			for n := 0; n < 8; n++ {
				command := fmt.Sprintf("echo fast-result-%d", n)
				if isCmdShell(test.shell) && n%3 == 2 {
					command = "cls"
				}
				write(command)
				output = readFramesUntil(t, reader, "\x1b]633;A\a")
				assertPrompt(output, root)
			}
			next := filepath.Join(root, "中文; working directory")
			if err := os.Mkdir(next, 0700); err != nil {
				t.Fatal(err)
			}
			if isCmdShell(test.shell) {
				write(`cd /d "` + next + `"`)
			} else {
				write(`Set-Location -LiteralPath '` + next + `'`)
			}
			output = readFramesUntil(t, reader, "\x1b]633;A\a")
			assertPrompt(output, next)

			// A redraw of the previous prompt must not finish a still-running command.
			if isCmdShell(test.shell) {
				write("ping -n 3 127.0.0.1 >nul & echo slow-result")
			} else {
				write("Start-Sleep -Seconds 2; Write-Output slow-result")
			}
			time.Sleep(300 * time.Millisecond)
			if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameResize, Data: []byte{0, 0, 0, 60, 0, 0, 0, 24}}); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			output = readFramesUntil(t, reader, "\x1b]633;A\a")
			if elapsed := time.Since(start); elapsed < time.Second {
				t.Errorf("slow command completed on redraw after %v: %q", elapsed, output)
			}
			assertPrompt(output, next)
			text, _ = normalizedLegacyPrompt(output)
			if strings.Count(text, "slow-result") < 2 {
				t.Errorf("slow result missing: %q", output)
			}

			if isPowerShell(test.shell) {
				write("Write-Error 'expected-test-error'")
				output = readFramesUntil(t, reader, "\x1b]633;A\a")
				if !strings.Contains(output, "\x1b]633;D;1\a") {
					t.Errorf("failure completion missing: %q", output)
				}
			}
			if isCmdShell(test.shell) {
				write("ping -t 127.0.0.1 >nul")
			} else {
				write("Start-Sleep -Seconds 60")
			}
			time.Sleep(150 * time.Millisecond)
			if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte{0x03}}); err != nil {
				t.Fatal(err)
			}
			output = readFramesUntil(t, reader, "\x1b]633;A\a")
			assertPrompt(output, next)
			write("exit")
			_ = readExitCode(t, reader)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func readWinPTYUntil(t *testing.T, reader *bufio.Reader, match func(string) bool) string {
	t.Helper()
	var output strings.Builder
	for output.Len() < 128*1024 {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatalf("read WinPTY output: %v; output=%q", err, output.String())
		}
		if frame.Type == protocol.FrameExit {
			t.Fatalf("WinPTY exited before prompt: %q", output.String())
		}
		output.Write(frame.Data)
		if match(output.String()) {
			return output.String()
		}
	}
	t.Fatalf("WinPTY output exceeded limit: %q", output.String())
	return ""
}
