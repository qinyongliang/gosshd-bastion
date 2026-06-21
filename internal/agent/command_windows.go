//go:build windows

package agent

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"unsafe"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"

	"golang.org/x/sys/windows"
)

func (c *Client) handleCommand(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	if req.Type == protocol.StreamShell {
		c.handleShell(stream, reader, req)
		return
	}

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

func (c *Client) handleShell(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	inRead, inWrite, outRead, outWrite, err := createConPTYPipes()
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}

	consoleSize := windows.Coord{X: int16(req.Width), Y: int16(req.Height)}
	if consoleSize.X <= 0 {
		consoleSize.X = 80
	}
	if consoleSize.Y <= 0 {
		consoleSize.Y = 24
	}

	var console windows.Handle
	if err := windows.CreatePseudoConsole(consoleSize, inRead, outWrite, 0, &console); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	defer windows.ClosePseudoConsole(console)

	input := os.NewFile(uintptr(inWrite), "gosshd-conpty-input")
	output := os.NewFile(uintptr(outRead), "gosshd-conpty-output")
	defer input.Close()
	defer output.Close()

	process, attributeList, err := startConPTYProcess(c.cfg.Shell, c.cfg.Root, console)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	defer attributeList.Delete()
	defer windows.CloseHandle(process)

	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}

	go copyFramesToConPTY(input, reader, console)
	go copyReaderToFrame(stream, protocol.FrameStdout, output)

	code := waitProcessExitCode(process)
	_ = protocol.WriteFrame(stream, protocol.ExitFrame(code))
}

func createConPTYPipes() (windows.Handle, windows.Handle, windows.Handle, windows.Handle, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return 0, 0, 0, 0, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return 0, 0, 0, 0, err
	}
	if err := windows.SetHandleInformation(inWrite, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return 0, 0, 0, 0, err
	}
	if err := windows.SetHandleInformation(outRead, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return 0, 0, 0, 0, err
	}
	return inRead, inWrite, outRead, outWrite, nil
}

func startConPTYProcess(shell, root string, console windows.Handle) (windows.Handle, *windows.ProcThreadAttributeListContainer, error) {
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		return 0, nil, err
	}
	attributeList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, nil, err
	}
	if err := attributeList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(console), unsafe.Sizeof(console)); err != nil {
		attributeList.Delete()
		return 0, nil, err
	}

	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:    uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags: windows.STARTF_USESTDHANDLES,
		},
		ProcThreadAttributeList: attributeList.List(),
	}
	var processInfo windows.ProcessInformation
	appName, err := windows.UTF16PtrFromString(shellPath)
	if err != nil {
		attributeList.Delete()
		return 0, nil, err
	}
	var currentDir *uint16
	if root != "" {
		currentDir, err = windows.UTF16PtrFromString(root)
		if err != nil {
			attributeList.Delete()
			return 0, nil, err
		}
	}

	if err := windows.CreateProcess(
		appName,
		nil,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NEW_PROCESS_GROUP,
		nil,
		currentDir,
		&startup.StartupInfo,
		&processInfo,
	); err != nil {
		attributeList.Delete()
		return 0, nil, err
	}
	_ = windows.CloseHandle(processInfo.Thread)
	return processInfo.Process, attributeList, nil
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

func copyFramesToConPTY(w io.WriteCloser, reader *bufio.Reader, console windows.Handle) {
	defer w.Close()
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
				width := int16(uint16(frame.Data[2])<<8 | uint16(frame.Data[3]))
				height := int16(uint16(frame.Data[6])<<8 | uint16(frame.Data[7]))
				if width > 0 && height > 0 {
					_ = windows.ResizePseudoConsole(console, windows.Coord{X: width, Y: height})
				}
			}
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

func waitProcessExitCode(process windows.Handle) int {
	_, _ = windows.WaitForSingleObject(process, windows.INFINITE)
	var code uint32
	if err := windows.GetExitCodeProcess(process, &code); err != nil {
		return 255
	}
	return int(code)
}

func isPowerShell(shell string) bool {
	return shell == "powershell.exe" || shell == "pwsh.exe" || shell == "powershell" || shell == "pwsh"
}
