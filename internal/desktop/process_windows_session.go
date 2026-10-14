//go:build windows

package desktop

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func randomWindowsName() (string, error) {
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	return "gosshd-desktop-" + hex.EncodeToString(secret[:]), nil
}

func windowsHelperDirectory(guiSID string) (string, error) {
	if guiSID == "" {
		return os.MkdirTemp("", "gosshd-desktop-")
	}
	// Create with the final protected DACL atomically, before writing any source.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;GA;;;SY)(A;OICI;GRGX;;;" + guiSID + ")")
	if err != nil {
		return "", err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	for attempt := 0; attempt < 5; attempt++ {
		name, err := randomWindowsName()
		if err != nil {
			return "", err
		}
		dir, err := filepath.Abs(filepath.Join(os.TempDir(), name))
		if err != nil {
			return "", err
		}
		value, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			return "", err
		}
		err = windows.CreateDirectory(value, &sa)
		if err == windows.ERROR_ALREADY_EXISTS {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create private Windows helper directory: %w", err)
		}
		return dir, nil
	}
	return "", errors.New("cannot allocate private Windows helper directory")
}

func loggedInWindowsToken() (windows.Token, uint32, error) {
	own, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return 0, 0, err
	}
	if !own.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
		return 0, 0, errors.New("a Session-0 desktop Agent must run as LocalSystem to launch its helper in the logged-in user session")
	}
	var token windows.Token
	var selected uint32
	err = winio.RunWithPrivilege("SeTcbPrivilege", func() error {
		var rows *windows.WTS_SESSION_INFO
		var count uint32
		if err := windows.WTSEnumerateSessions(0, 0, 1, &rows, &count); err != nil {
			return err
		}
		defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(rows)))
		if count > 4096 {
			return errors.New("unexpected Windows session count")
		}
		sessions := unsafe.Slice(rows, int(count))
		tokens := make(map[uint32]windows.Token)
		var candidates []uint32
		defer func() {
			for id, t := range tokens {
				if id != selected || token == 0 {
					t.Close()
				}
			}
		}()
		for _, row := range sessions {
			if row.SessionID == 0 || row.State != windows.WTSActive {
				continue
			}
			if _, ok := tokens[row.SessionID]; ok {
				continue
			}
			var current windows.Token
			err := windows.WTSQueryUserToken(row.SessionID, &current)
			if err == windows.ERROR_NO_TOKEN || err == windows.ERROR_INVALID_PARAMETER {
				continue
			}
			if err != nil {
				return fmt.Errorf("query logged-in Windows session token: %w", err)
			}
			tokens[row.SessionID] = current
			candidates = append(candidates, row.SessionID)
		}
		var err error
		selected, err = selectWindowsInteractiveSession(windows.WTSGetActiveConsoleSessionId(), candidates)
		if err != nil {
			return err
		}
		token = tokens[selected]
		return nil
	})
	if err != nil {
		if token != 0 {
			token.Close()
		}
		return 0, 0, err
	}
	return token, selected, nil
}

type windowsGuiChild struct {
	job     windows.Handle
	process windows.Handle
	pid     uint32
	exited  chan struct{}
	once    sync.Once
}

func (child *windowsGuiChild) close() {
	child.once.Do(func() {
		_ = windows.TerminateJobObject(child.job, 1)
		_ = windows.CloseHandle(child.job)
		select {
		case <-child.exited:
			windows.CloseHandle(child.process)
		case <-time.After(5 * time.Second):
			go func() { <-child.exited; windows.CloseHandle(child.process) }()
		}
	})
}

func launchWindowsGuiHelper(token windows.Token, executable, pipe string) (*windowsGuiChild, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	// The helper dies with its owner; explicitly launched user apps survive it.
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	var environment *uint16
	if err = windows.CreateEnvironmentBlock(&environment, token, false); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	defer windows.DestroyEnvironmentBlock(environment)
	profile, err := token.GetUserProfileDirectory()
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	application, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	currentDirectory, err := windows.UTF16PtrFromString(profile)
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	args := []string{executable, "--ipc-pipe", pipe, "--ipc-parent-pid", strconv.Itoa(os.Getpid())}
	for i := range args {
		args[i] = syscall.EscapeArg(args[i])
	}
	command, err := windows.UTF16FromString(strings.Join(args, " "))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{})), Desktop: desktop, Flags: windows.STARTF_USESHOWWINDOW, ShowWindow: windows.SW_HIDE}
	var info windows.ProcessInformation
	err = winio.RunWithPrivileges([]string{"SeAssignPrimaryTokenPrivilege", "SeIncreaseQuotaPrivilege"}, func() error {
		return windows.CreateProcessAsUser(token, application, &command[0], nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_NO_WINDOW|windows.CREATE_UNICODE_ENVIRONMENT, environment, currentDirectory, &startup, &info)
	})
	if err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("launch logged-in Windows desktop helper: %w", err)
	}
	if err = windows.AssignProcessToJobObject(job, info.Process); err != nil {
		windows.TerminateProcess(info.Process, 1)
		windows.WaitForSingleObject(info.Process, 5000)
		windows.CloseHandle(info.Thread)
		windows.CloseHandle(info.Process)
		windows.CloseHandle(job)
		return nil, err
	}
	child := &windowsGuiChild{job: job, process: info.Process, pid: info.ProcessId, exited: make(chan struct{})}
	go func() {
		windows.WaitForSingleObject(info.Process, windows.INFINITE)
		close(child.exited)
	}()
	if _, err = windows.ResumeThread(info.Thread); err != nil {
		windows.CloseHandle(info.Thread)
		child.close()
		return nil, err
	}
	windows.CloseHandle(info.Thread)
	return child, nil
}

func validateWindowsPipePeer(conn net.Conn, pid, session uint32) error {
	file, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("Windows desktop pipe has no native handle")
	}
	var actual uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(file.Fd()), &actual); err != nil {
		return err
	}
	if actual != pid {
		return errors.New("Windows desktop IPC client process mismatch")
	}
	var actualSession uint32
	if err := windows.ProcessIdToSessionId(actual, &actualSession); err != nil {
		return err
	}
	if actualSession != session {
		return errors.New("Windows desktop IPC client session mismatch")
	}
	return nil
}

func acceptWindowsHelper(ctx context.Context, listener net.Listener, child *windowsGuiChild, session uint32) (net.Conn, error) {
	type accepted struct {
		conn net.Conn
		err  error
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ready := make(chan accepted)
	go func() {
		for {
			conn, err := listener.Accept()
			if err == nil {
				if peerErr := validateWindowsPipePeer(conn, child.pid, session); peerErr != nil {
					conn.Close()
					continue
				}
			}
			select {
			case ready <- accepted{conn, err}:
			case <-startup.Done():
				if conn != nil {
					conn.Close()
				}
			}
			return
		}
	}()
	defer listener.Close()
	select {
	case result := <-ready:
		select {
		case <-child.exited:
			if result.conn != nil {
				result.conn.Close()
			}
			return nil, errors.New("Windows desktop helper exited during IPC startup")
		default:
		}
		return result.conn, result.err
	case <-child.exited:
		return nil, errors.New("Windows desktop helper exited before connecting its private IPC; custom helpers must support the pipe interface")
	case <-startup.Done():
		return nil, startup.Err()
	}
}

func startInteractiveWindowsHelper(ctx context.Context, cfg Config) (*process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	token, session, err := loggedInWindowsToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	command, cleanup, err := windowsHelperCommand(ctx, cfg, user.User.Sid.String())
	if err != nil {
		return nil, err
	}
	name, err := randomWindowsName()
	if err != nil {
		cleanup()
		return nil, err
	}
	// Explicit read/write data rights omit creating other server instances.
	descriptor := "D:P(A;;GA;;;SY)(A;;0x0012019b;;;" + user.User.Sid.String() + ")"
	listener, err := winio.ListenPipe(`\\.\pipe\`+name, &winio.PipeConfig{SecurityDescriptor: descriptor, InputBufferSize: 65536, OutputBufferSize: 65536})
	if err != nil {
		cleanup()
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		listener.Close()
		cleanup()
		return nil, err
	}
	child, err := launchWindowsGuiHelper(token, command.Path, name)
	if err != nil {
		listener.Close()
		cleanup()
		return nil, err
	}
	conn, err := acceptWindowsHelper(ctx, listener, child, session)
	if err != nil {
		child.close()
		cleanup()
		return nil, err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 65536), MaxMessage)
	native := &process{in: conn, out: scanner}
	finished := make(chan struct{})
	native.closeNative = func() { conn.Close(); child.close(); cleanup(); close(finished) }
	go func() {
		select {
		case <-ctx.Done():
			native.close()
		case <-child.exited:
			native.close()
		case <-finished:
		}
	}()
	return native, nil
}
