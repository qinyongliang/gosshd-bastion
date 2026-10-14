//go:build windows

package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func windowsTestPipe(t *testing.T) (string, net.Listener) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	name, err := randomWindowsName()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := winio.ListenPipe(`\\.\pipe\`+name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return name, listener
}

func TestWindowsDesktopPipePeerIdentity(t *testing.T) {
	name, listener := windowsTestPipe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	received := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			received <- conn
		}
	}()
	client, err := winio.DialPipeContext(ctx, `\\.\pipe\`+name)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var server net.Conn
	select {
	case server = <-received:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer server.Close()
	var session uint32
	if err = windows.ProcessIdToSessionId(uint32(os.Getpid()), &session); err != nil {
		t.Fatal(err)
	}
	if err = validateWindowsPipePeer(server, uint32(os.Getpid()), session); err != nil {
		t.Fatal(err)
	}
	if validateWindowsPipePeer(server, uint32(os.Getpid()+1), session) == nil {
		t.Fatal("accepted another client process")
	}
	if validateWindowsPipePeer(server, uint32(os.Getpid()), session+1) == nil {
		t.Fatal("accepted another Windows session")
	}
}

func TestWindowsDesktopNativePrivatePipeProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd, cleanup, err := platformCommand(ctx, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	name, listener := windowsTestPipe(t)
	cmd.Args = append(cmd.Args, "--ipc-pipe", name, "--ipc-parent-pid", strconv.Itoa(os.Getpid()))
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	defer func() {
		cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			t.Error("native pipe helper did not exit")
		}
	}()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-exited:
		t.Fatal("helper exited before IPC")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer conn.Close()
	var session uint32
	if err = windows.ProcessIdToSessionId(uint32(os.Getpid()), &session); err != nil {
		t.Fatal(err)
	}
	if err = validateWindowsPipePeer(conn, uint32(cmd.Process.Pid), session); err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	id := "pipe-汉字-🙂-" + strings.Repeat("x", 70000)
	data, _ := json.Marshal(Request{ID: id, Method: "status"})
	if _, err = conn.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if err = json.Unmarshal(line, &response); err != nil || !response.OK || response.ID != id {
		t.Fatalf("private pipe protocol: %v", err)
	}
	// Only status was requested: no screenshot, AX contents or keyboard/mouse input.
}

func TestWindowsDesktopJobTerminatesOwnedHelper(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_JOB_TEST_CHILD") == "1" {
		time.Sleep(time.Minute)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsDesktopJobTerminatesOwnedHelper$")
	cmd.Env = append(os.Environ(), "GOSSHD_DESKTOP_JOB_TEST_CHILD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		t.Fatal(err)
	}
	if err = windows.AssignProcessToJobObject(job, handle); err != nil {
		windows.CloseHandle(job)
		t.Fatal(err)
	}
	child := &windowsGuiChild{job: job, process: handle, pid: uint32(cmd.Process.Pid), exited: make(chan struct{})}
	go func() { cmd.Wait(); close(child.exited) }()
	started := time.Now()
	child.close()
	child.close()
	if time.Since(started) > 3*time.Second {
		t.Fatal("owned helper cleanup exceeded deadline")
	}
	select {
	case <-child.exited:
	default:
		t.Fatal("owned helper remains alive")
	}
}

func TestWindowsDesktopPipeStartupCancellation(t *testing.T) {
	_, listener := windowsTestPipe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	child := &windowsGuiChild{pid: uint32(os.Getpid()), exited: make(chan struct{})}
	started := time.Now()
	_, err := acceptWindowsHelper(ctx, listener, child, 1)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("pipe startup ignored cancellation: %v", err)
	}
}

func TestWindowsDesktopPipeStartupChildExit(t *testing.T) {
	_, listener := windowsTestPipe(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	child := &windowsGuiChild{pid: uint32(os.Getpid()), exited: make(chan struct{})}
	close(child.exited)
	_, err := acceptWindowsHelper(ctx, listener, child, 1)
	if err == nil {
		t.Fatal("accepted an exited helper")
	}
}
