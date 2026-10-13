//go:build darwin

package desktop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMacBundleCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := startNativeProcess(ctx, Config{HelperPath: filepath.Join(t.TempDir(), "Missing.app", "Contents", "MacOS", "helper")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled launch: %v", err)
	}
}

func TestMacBundleProcessLifecycle(t *testing.T) {
	helperPath := os.Getenv("GOSSHD_DESKTOP_HELPER_TEST_PATH")
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" || helperPath == "" {
		t.Skip("native macOS bundle lifecycle test is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := Config{HelperPath: helperPath, Enabled: true}
	first, err := startNativeProcess(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := startNativeProcess(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	inputPath := first.in.(interface{ Name() string }).Name()
	otherPath := second.in.(interface{ Name() string }).Name()
	if inputPath == otherPath {
		t.Fatal("sessions shared IPC")
	}
	for _, path := range []string{inputPath, otherPath} {
		info, e := os.Stat(path)
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private FIFO permissions: %v", e)
		}
		info, e = os.Stat(filepath.Dir(path))
		if e != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("private directory permissions: %v", e)
		}
	}
	for _, helper := range []*process{first, second} {
		for _, id := range []string{"one", "two"} {
			res, e := helper.call(Request{ID: id, Method: "status"})
			if e != nil || !res.OK {
				t.Fatalf("persistent LaunchServices protocol: %v %s", e, res.Error)
			}
		}
	}
	// Inspect only processes holding our unique FIFO, never another desktop app.
	ids := nativeFIFOReaders(ctx, inputPath)
	var helperPID int
	for _, pid := range ids {
		if pid > 1 && pid != os.Getpid() && pid != first.cmd.Process.Pid {
			helperPID = pid
		}
	}
	if helperPID == 0 {
		t.Fatal("native helper did not hold session FIFO")
	}
	first.close()
	if _, err = os.Stat(filepath.Dir(inputPath)); !os.IsNotExist(err) {
		t.Fatal("private IPC directory survived close")
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(helperPID, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(helperPID, 0) == nil {
		t.Fatal("native helper survived session close")
	}
	res, err := second.call(Request{ID: "still-live", Method: "status"})
	if err != nil || !res.OK {
		t.Fatalf("closing another session affected this helper: %v", err)
	}
	cancel()
	closed := make(chan struct{})
	go func() { second.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(8 * time.Second):
		t.Fatal("context cancellation did not terminate helper")
	}
	if _, err = os.Stat(filepath.Dir(otherPath)); !os.IsNotExist(err) {
		t.Fatal("cancelled IPC directory survived")
	}
}

func TestMacBundlePeerDisconnect(t *testing.T) {
	helperPath := os.Getenv("GOSSHD_DESKTOP_HELPER_TEST_PATH")
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" || helperPath == "" {
		t.Skip("native macOS peer disconnect test is opt-in")
	}
	a, b := net.Pipe()
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, b, Config{Enabled: true, HelperPath: helperPath}) }()
	scan := bufio.NewScanner(a)
	if !scan.Scan() {
		t.Fatal("initialization not received")
	}
	if _, err := a.Write([]byte("{\"id\":\"probe\",\"method\":\"status\",\"params\":{}}\n")); err != nil {
		t.Fatal(err)
	}
	if !scan.Scan() || !strings.Contains(scan.Text(), "probe") {
		t.Fatal("native status response not received")
	}
	a.Close()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("peer disconnect did not stop native bridge")
	}
}

func TestMacBundleBusyHelperCancellation(t *testing.T) {
	if os.Getenv("GOSSHD_DESKTOP_NATIVE_TEST") != "1" {
		t.Skip("LaunchServices test is opt-in")
	}
	root := t.TempDir()
	bundle := filepath.Join(root, "FakeHelper.app")
	binary := filepath.Join(bundle, "Contents", "MacOS", "helper")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	ready := filepath.Join(root, "hang-ready")
	script := fmt.Sprintf("#!/bin/sh\nGOSSHD_DESKTOP_TEST_HELPER=1 GOSSHD_DESKTOP_TEST_HANG_FILE=%s exec %s -test.run='^TestHelperProcess$'\n", quote(ready), quote(executable))
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.gosshd.desktop-test-helper</string><key>CFBundleExecutable</key><string>helper</string><key>CFBundlePackageType</key><string>APPL</string><key>LSUIElement</key><true/></dict></plist>`
	if err = os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	helper, err := startNativeProcess(ctx, Config{HelperPath: binary})
	if err != nil {
		t.Fatal(err)
	}
	defer helper.close()
	if res, e := helper.call(Request{ID: "ready", Method: "status"}); e != nil || !res.OK {
		t.Fatalf("fake bundle did not start: %v", e)
	}
	done := make(chan error, 1)
	go func() {
		_, err := helper.call(Request{ID: "hang", Method: "status", Params: Params{Text: "hang"}})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake provider did not enter blocked call")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pidData, _ := os.ReadFile(ready)
	helperPID, _ := strconv.Atoi(string(pidData))
	cancel()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("blocked call survived cancellation")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("native blocked provider was not terminated")
	}
	helper.close()
	deadline = time.Now().Add(3 * time.Second)
	for syscall.Kill(helperPID, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if helperPID <= 1 || syscall.Kill(helperPID, 0) == nil {
		t.Fatal("blocked native provider survived cleanup")
	}
	if _, err = os.Stat(filepath.Dir(helper.in.(interface{ Name() string }).Name())); !os.IsNotExist(err) {
		t.Fatal("busy session left private IPC behind")
	}
}
