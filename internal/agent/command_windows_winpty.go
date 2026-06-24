//go:build windows && (amd64 || 386)

package agent

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"

	winpty "github.com/iamacarpet/go-winpty"
)

func (c *Client) handleWinPTYShell(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("winpty unavailable: %v", recovered)
		}
	}()

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	cols, rows := terminalSize(req)
	pty, err := winpty.OpenWithOptions(winpty.Options{
		DLLPrefix:   filepath.Dir(exePath),
		Command:     windowsCommandLine(c.cfg.Shell),
		Dir:         c.cfg.Root,
		Env:         os.Environ(),
		Flags:       winpty.WINPTY_FLAG_ALLOW_CURPROC_DESKTOP_CREATION,
		InitialCols: uint32(cols),
		InitialRows: uint32(rows),
	})
	if err != nil {
		return err
	}
	defer pty.Close()

	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return err
	}

	go copyFramesToWinPTY(pty, reader)
	go copyWindowsConsoleReaderToFrame(stream, protocol.FrameStdout, pty.StdOut, c.cfg.Shell)

	code := waitHandleExitCode(pty.GetProcHandle())
	_ = protocol.WriteFrame(stream, protocol.ExitFrame(code))
	return nil
}

func copyFramesToWinPTY(pty *winpty.WinPTY, reader *bufio.Reader) {
	defer pty.StdIn.Close()
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			return
		}
		switch frame.Type {
		case protocol.FrameStdin:
			writeWinPTYInput(pty.StdIn, frame.Data)
		case protocol.FrameResize:
			if len(frame.Data) == 8 {
				width := uint32(frame.Data[0])<<24 | uint32(frame.Data[1])<<16 | uint32(frame.Data[2])<<8 | uint32(frame.Data[3])
				height := uint32(frame.Data[4])<<24 | uint32(frame.Data[5])<<16 | uint32(frame.Data[6])<<8 | uint32(frame.Data[7])
				pty.SetSize(width, height)
			}
		}
	}
}

func writeWinPTYInput(w io.Writer, data []byte) {
	for len(data) > 0 {
		special := -1
		for i, b := range data {
			if b == 0x04 {
				special = i
				break
			}
		}
		if special < 0 {
			_, _ = w.Write(data)
			return
		}
		if special > 0 {
			_, _ = w.Write(data[:special])
		}
		_, _ = w.Write([]byte("exit\r\n"))
		data = data[special+1:]
	}
}

func terminalSize(req protocol.StreamRequest) (int, int) {
	cols, rows := req.Width, req.Height
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return cols, rows
}

func windowsCommandLine(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "cmd.exe"
	}
	args := windowsInteractiveShellArgs(command)
	if len(args) == 0 {
		return quoteWindowsArg(command)
	}
	parts := []string{quoteWindowsArg(command)}
	for _, arg := range args {
		parts = append(parts, quoteWindowsArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteWindowsArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}
