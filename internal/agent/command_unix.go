//go:build !windows

package agent

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/qinyongliang/gosshd/internal/protocol"

	"github.com/creack/pty"
)

func (c *Client) handleCommand(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	cmd := exec.Command(c.cfg.Shell)
	if req.Type == protocol.StreamExec {
		cmd = exec.Command(c.cfg.Shell, "-lc", req.Command)
	}
	cmd.Dir = c.cfg.Root
	cmd.Env = os.Environ()
	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(req.Width), Rows: uint16(req.Height)})
	if err != nil {
		_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStderr, Data: []byte(err.Error())})
		_ = protocol.WriteFrame(stream, protocol.ExitFrame(255))
		return
	}
	defer ptmx.Close()
	go copyFramesToWriter(ptmx, reader)
	buf := make([]byte, 32*1024)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if writeErr := protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStdout, Data: append([]byte(nil), buf[:n]...)}); writeErr != nil {
				return
			}
		}
		if err != nil {
			break
		}
	}
	code := waitExitCode(cmd)
	_ = protocol.WriteFrame(stream, protocol.ExitFrame(code))
}

func copyFramesToWriter(w io.Writer, reader *bufio.Reader) {
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			return
		}
		switch frame.Type {
		case protocol.FrameStdin:
			_, _ = w.Write(frame.Data)
		case protocol.FrameResize:
			if len(frame.Data) == 8 {
				_ = pty.Setsize(w.(*os.File), &pty.Winsize{
					Cols: uint16(binary.BigEndian.Uint32(frame.Data[0:4])),
					Rows: uint16(binary.BigEndian.Uint32(frame.Data[4:8])),
				})
			}
		}
	}
}

func waitExitCode(cmd *exec.Cmd) int {
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
		return 255
	}
	return 0
}
