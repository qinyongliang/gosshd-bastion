//go:build windows

package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"

	"golang.org/x/sys/windows"
)

const (
	conPTYCtrlCArg = "--gosshd-conpty-ctrl-c"
	conPTYCtrlCEnv = "GOSSHD_CONPTY_CTRL_C_PID"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != conPTYCtrlCArg {
		return
	}
	processID, err := strconv.ParseUint(os.Getenv(conPTYCtrlCEnv), 10, 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := sendConsoleCtrlC(uint32(processID)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

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

	process, processID, attributeList, err := startConPTYProcess(c.cfg.Shell, c.cfg.Root, console)
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: false, Error: err.Error()})
		return
	}
	defer attributeList.Delete()
	defer windows.CloseHandle(process)

	if err := protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}

	go copyFramesToConPTY(input, reader, console, processID)
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

func startConPTYProcess(shell, root string, console windows.Handle) (windows.Handle, uint32, *windows.ProcThreadAttributeListContainer, error) {
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		return 0, 0, nil, err
	}
	attributeList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, 0, nil, err
	}
	if err := attributeList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(console), unsafe.Sizeof(console)); err != nil {
		attributeList.Delete()
		return 0, 0, nil, err
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
		return 0, 0, nil, err
	}
	var currentDir *uint16
	if root != "" {
		currentDir, err = windows.UTF16PtrFromString(root)
		if err != nil {
			attributeList.Delete()
			return 0, 0, nil, err
		}
	}

	if err := windows.CreateProcess(
		appName,
		nil,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		nil,
		currentDir,
		&startup.StartupInfo,
		&processInfo,
	); err != nil {
		attributeList.Delete()
		return 0, 0, nil, err
	}
	_ = windows.CloseHandle(processInfo.Thread)
	return processInfo.Process, processInfo.ProcessId, attributeList, nil
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

func copyFramesToConPTY(w io.WriteCloser, reader *bufio.Reader, console windows.Handle, processID uint32) {
	defer w.Close()
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			return
		}
		switch frame.Type {
		case protocol.FrameStdin:
			writeConPTYInput(w, frame.Data, processID)
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

func writeConPTYInput(w io.Writer, data []byte, processID uint32) {
	for len(data) > 0 {
		special := -1
		for i, b := range data {
			if b == 0x03 || b == 0x04 {
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
		switch data[special] {
		case 0x03:
			_ = interruptConsoleProcess(processID)
		case 0x04:
			_, _ = w.Write([]byte("exit\r\n"))
		}
		data = data[special+1:]
	}
}

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole         = kernel32.NewProc("AttachConsole")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

func interruptConsoleProcess(processID uint32) error {
	if processID == 0 {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, conPTYCtrlCArg)
	cmd.Env = append(os.Environ(), conPTYCtrlCEnv+"="+strconv.FormatUint(uint64(processID), 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.Run()
}

func sendConsoleCtrlC(processID uint32) error {
	detached, err := attachConsole(processID)
	if err != nil {
		return err
	}
	defer restoreConsole(detached)

	_ = setConsoleCtrlIgnored(true)
	defer setConsoleCtrlIgnored(false)

	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return nil
}

func attachConsole(processID uint32) (bool, error) {
	if err := callAttachConsole(processID); err == nil {
		return false, nil
	} else if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return false, err
	}

	if err := freeConsole(); err != nil {
		return false, err
	}
	if err := callAttachConsole(processID); err != nil {
		return false, err
	}
	return true, nil
}

func restoreConsole(reattachParent bool) {
	_ = freeConsole()
	if reattachParent {
		const attachParentProcess = ^uint32(0)
		_ = callAttachConsole(attachParentProcess)
	}
}

func callAttachConsole(processID uint32) error {
	r, _, err := procAttachConsole.Call(uintptr(processID))
	if r == 0 {
		return err
	}
	return nil
}

func freeConsole() error {
	r, _, err := procFreeConsole.Call()
	if r == 0 {
		return err
	}
	return nil
}

func setConsoleCtrlIgnored(ignore bool) error {
	add := uintptr(0)
	if ignore {
		add = 1
	}
	r, _, err := procSetConsoleCtrlHandler.Call(0, add)
	if r == 0 {
		return err
	}
	return nil
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
