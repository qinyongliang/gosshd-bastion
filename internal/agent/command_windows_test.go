//go:build windows

package agent

import (
	"bufio"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"golang.org/x/sys/windows"
	"golang.org/x/text/transform"
)

func TestWindowsShellUsesConPTYForInteractiveInput(t *testing.T) {
	client, err := New(Config{
		Server: "http://agent.example.test:8880",
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
	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("ping -t 127.0.0.1\r")}); err != nil {
		t.Fatal(err)
	}
	_ = readFramesUntil(t, reader, "TTL=")
	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte{0x03}}); err != nil {
		t.Fatal(err)
	}
	output = readFramesUntil(t, reader, "Control-C")
	if !strings.Contains(output, "Control-C") {
		t.Fatalf("ctrl-c did not interrupt ping: %q", output)
	}
	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("echo still-alive\r")}); err != nil {
		t.Fatal(err)
	}
	output = readFramesUntil(t, reader, "still-alive")
	if !strings.Contains(output, "still-alive") {
		t.Fatalf("shell did not continue after ctrl-c: %q", output)
	}
	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte{0x04}}); err != nil {
		t.Fatal(err)
	}
	if code := readExitCode(t, reader); code != 0 && uint32(code) != 0xC000013A {
		t.Fatalf("exit code mismatch: got %d want 0 or CTRL_C_EVENT", code)
	}
}

func TestWindowsInteractiveShellArgsKeepInteractiveEcho(t *testing.T) {
	if got, want := windowsInteractiveShellArgs("cmd.exe"), []string{"/D", "/K", windowsCmdIntegrationCommand()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cmd args mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if got, want := windowsInteractiveShellArgs(`C:\Windows\system32\cmd.exe`), []string{"/D", "/K", windowsCmdIntegrationCommand()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cmd path args mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	got := windowsCommandLine("cmd.exe")
	if strings.Contains(got, " /Q ") {
		t.Fatalf("interactive cmd should not disable echo: %q", got)
	}
	if strings.Contains(got, "delims=") {
		t.Fatalf("interactive cmd should not expose for/F delimiters: %q", got)
	}
	if !strings.Contains(got, "633;D") || !strings.Contains(got, "633;A") || !strings.Contains(got, "633;P;Cwd=$P") {
		t.Fatalf("interactive cmd should install terminal integration prompt: %q", got)
	}
}

func TestWindowsPowerShellInteractiveArgsInstallTerminalIntegration(t *testing.T) {
	args := windowsInteractiveShellArgs("powershell.exe")
	if len(args) != 5 || args[0] != "-NoLogo" || args[1] != "-NoProfile" || args[2] != "-NoExit" || args[3] != "-Command" {
		t.Fatalf("powershell args mismatch: %#v", args)
	}
	if !strings.Contains(args[4], "function global:prompt") || !strings.Contains(args[4], "633;D") || !strings.Contains(args[4], "633;A") || !strings.Contains(args[4], "633;P;Cwd=") {
		t.Fatalf("powershell integration missing: %q", args[4])
	}
}

func TestWindowsShellBaseNameDetectsFullPaths(t *testing.T) {
	if !isCmdShell(`C:\Windows\system32\cmd.exe`) {
		t.Fatal("cmd full path should be detected")
	}
	if !isPowerShell(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`) {
		t.Fatal("powershell full path should be detected")
	}
}

func TestNormalizePipeInputExpandsBareCarriageReturn(t *testing.T) {
	got := string(normalizePipeInput([]byte("echo ok\rnext\r\n")))
	if want := "echo ok\r\nnext\r\n"; got != want {
		t.Fatalf("normalized input mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestWindowsConsoleEncodingDecodesCP936(t *testing.T) {
	decoder := windowsConsoleEncodingForCodePage(936).NewDecoder()
	got, _, err := transform.String(decoder, "\xc7\xfd\xb6\xaf\xc6\xf7")
	if err != nil {
		t.Fatal(err)
	}
	if got != "驱动器" {
		t.Fatalf("CP936 decode mismatch: %q", got)
	}
}

func readFramesUntil(t *testing.T, reader *bufio.Reader, marker string) string {
	t.Helper()
	var out strings.Builder
	for out.Len() < 64*1024 {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatalf("read frames until %q: %v; output=%q", marker, err, out.String())
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

func TestWindowsConPTYPromptPreservesIntegration(t *testing.T) {
	if err := windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find(); err != nil {
		t.Skip("ConPTY is not available on this Windows version")
	}
	root := t.TempDir()
	client, err := New(Config{Server: "http://agent.example.test:8880", IDFile: filepath.Join(t.TempDir(), "agent.json"), Shell: "cmd.exe", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	agentConn, peerConn := net.Pipe()
	defer agentConn.Close()
	defer peerConn.Close()
	_ = peerConn.SetDeadline(time.Now().Add(10 * time.Second))
	done := make(chan error, 1)
	go func() {
		done <- client.handleConPTYShell(agentConn, bufio.NewReader(agentConn), protocol.StreamRequest{Type: protocol.StreamShell, Width: 200, Height: 30})
		close(done)
	}()
	defer func() {
		_ = peerConn.SetDeadline(time.Now().Add(3 * time.Second))
		go io.Copy(io.Discard, peerConn)
		_ = protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("exit\r")})
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("ConPTY shell did not stop")
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
	output := readFramesUntil(t, reader, root+">")
	for _, marker := range []string{"\x1b]633;D;0", "\x1b]633;A", "\x1b]633;P;Cwd=" + root} {
		if !strings.Contains(output, marker) {
			t.Errorf("missing %q in ConPTY prompt %q", marker, output)
		}
	}
	if strings.Contains(output, "__GOSSHD_") {
		t.Errorf("legacy marker used for ConPTY: %q", output)
	}
	if err := protocol.WriteFrame(peerConn, protocol.Frame{Type: protocol.FrameStdin, Data: []byte("exit\r")}); err != nil {
		t.Fatal(err)
	}
	_ = readExitCode(t, reader)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
