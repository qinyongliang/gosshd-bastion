//go:build windows

package agent

import (
	"bufio"
	"io"
	"os"
	"os/exec"

	"github.com/qinyongliang/gosshd/internal/protocol"
)

func (c *Client) handleCommand(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	args := []string{}
	if req.Type == protocol.StreamExec {
		if isPowerShell(c.cfg.Shell) {
			args = []string{"-NoLogo", "-NoProfile", "-Command", req.Command}
		} else {
			args = []string{"/C", req.Command}
		}
	}
	cmd := exec.Command(c.cfg.Shell, args...)
	cmd.Dir = c.cfg.Root
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}
	go copyFramesToWriter(stdin, reader)
	go copyReaderToFrame(stream, protocol.FrameStdout, stdout)
	go copyReaderToFrame(stream, protocol.FrameStderr, stderr)
	code := waitExitCode(cmd)
	_ = stdin.Close()
	_ = protocol.WriteFrame(stream, protocol.ExitFrame(code))
}

func copyFramesToWriter(w io.WriteCloser, reader *bufio.Reader) {
	defer w.Close()
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			return
		}
		if frame.Type == protocol.FrameStdin {
			_, _ = w.Write(frame.Data)
		}
	}
}

func copyReaderToFrame(w io.Writer, typ byte, r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if writeErr := protocol.WriteFrame(w, protocol.Frame{Type: typ, Data: append([]byte(nil), buf[:n]...)}); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func waitExitCode(cmd *exec.Cmd) int {
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return 255
	}
	return 0
}

func isPowerShell(shell string) bool {
	return shell == "powershell.exe" || shell == "pwsh.exe" || shell == "powershell" || shell == "pwsh"
}
