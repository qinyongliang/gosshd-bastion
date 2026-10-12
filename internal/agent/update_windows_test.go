//go:build windows

package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type updateProbe struct {
	Path string
	Args []string
	PID  int
}

// Runs only in disposable child processes, never in the real agent/service.
func TestWindowsUpdateProcess(t *testing.T) {
	mode := os.Getenv("GOSSHD_UPDATE_TEST_MODE")
	if mode == "" {
		return
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "probe" {
		data, err := json.Marshal(updateProbe{current, os.Args[1:], os.Getpid()})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("GOSSHD_UPDATE_TEST_RESULT"), data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	// The update helper and restarted process inherit the probe mode.
	if err := os.Setenv("GOSSHD_UPDATE_TEST_MODE", "probe"); err != nil {
		t.Fatal(err)
	}
	if mode == "legacy" {
		if restarting, err := finalizeLegacyUpdate(); err != nil || !restarting {
			t.Fatalf("legacy finalization: restarting=%v err=%v", restarting, err)
		}
	} else if mode == "update" {
		if err := replaceAndRestart(current, os.Getenv("GOSSHD_UPDATE_TEST_REPLACEMENT")); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatalf("unexpected child mode: %s", mode)
	}
	// Keep the executable locked briefly after launching the helper to verify it
	// actually waits for parent exit rather than replacing a running image.
	time.Sleep(500 * time.Millisecond)
}

func TestWindowsUpdateReplacesAndRestartsCanonicalPath(t *testing.T) {
	for _, mode := range []string{"update", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "spaces 中文 'quoted' [literal].new-42")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			canonical := filepath.Join(dir, "gosshd-agent.exe")
			current := canonical
			pending := canonical + ".new-789"
			binary, err := os.ReadFile(os.Args[0])
			if err != nil {
				t.Fatal(err)
			}
			if mode == "legacy" {
				current = canonical + ".new-123.new-456"
				pending = current
				if err := os.WriteFile(canonical, []byte("obsolete canonical agent"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(current, binary, 0700); err != nil {
				t.Fatal(err)
			}
			if mode == "update" {
				if err := os.WriteFile(pending, binary, 0700); err != nil {
					t.Fatal(err)
				}
			}
			old := canonical + ".new-111.new-222"
			unrelated := canonical + ".new-not-numeric"
			config := filepath.Join(dir, "agent-id.json")
			for _, path := range []string{old, unrelated, config} {
				if err := os.WriteFile(path, []byte("keep configuration"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := filepath.Join(dir, "result.json")
			args := []string{"-test.run=^TestWindowsUpdateProcess$", "--", "with spaces", `quote"inside`, "", `C:\trailing space\`, "中文'$()"}
			parent := exec.Command(current, args...)
			parent.Env = append(os.Environ(), "GOSSHD_UPDATE_TEST_MODE="+mode, "GOSSHD_UPDATE_TEST_RESULT="+result, "GOSSHD_UPDATE_TEST_REPLACEMENT="+pending)
			parent.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if output, err := parent.CombinedOutput(); err != nil {
				t.Fatalf("parent failed: %v\n%s", err, output)
			}
			var probe updateProbe
			deadline := time.Now().Add(30 * time.Second)
			for {
				data, err := os.ReadFile(result)
				if err == nil && json.Unmarshal(data, &probe) == nil {
					break
				}
				if time.Now().After(deadline) {
					log, _ := os.ReadFile(canonical + ".update-error.log")
					t.Fatalf("replacement did not restart: %v\n%s", err, log)
				}
				time.Sleep(100 * time.Millisecond)
			}
			process, err := os.FindProcess(probe.PID)
			if err == nil {
				_, _ = process.Wait()
			}
			if probe.Path != canonical {
				t.Fatalf("restart path = %q, want %q", probe.Path, canonical)
			}
			if !reflect.DeepEqual(probe.Args, args) {
				t.Fatalf("restart args = %#v, want %#v", probe.Args, args)
			}
			installed, err := os.ReadFile(canonical)
			if err != nil || !reflect.DeepEqual(installed, binary) {
				t.Fatalf("canonical binary mismatch: %v", err)
			}
			for _, path := range []string{old, pending, canonical + ".update-error.log"} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("update artifact retained: %s (%v)", path, err)
				}
			}
			for _, path := range []string{unrelated, config} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("unrelated file removed: %s (%v)", path, err)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 4 {
				t.Fatalf("unexpected leftover files: %v", entries)
			}
		})
	}
}

func TestWindowsUpdateScriptServiceRestart(t *testing.T) {
	script := windowsUpdateScript(`C:\ProgramData\gosshd\gosshd-agent.exe.new-123`, `C:\ProgramData\gosshd\gosshd-agent.exe.new-456`, 1234, nil, true)
	for _, want := range []string{"$isService = $true", "Start-Service -Name 'gosshd-agent'", "WaitForStatus('Stopped'"} {
		if !strings.Contains(script, want) {
			t.Fatalf("service script missing %q", want)
		}
	}
	if !strings.Contains(script, "$target = "+quotePowerShell(`C:\ProgramData\gosshd\gosshd-agent.exe`)) {
		t.Fatal("service restart not using canonical path")
	}
}

func TestWindowsUpdateRejectsUnrelatedReplacement(t *testing.T) {
	canonical := filepath.Join(t.TempDir(), "gosshd-agent.exe")
	for _, path := range []string{canonical, canonical + ".new-text", filepath.Join(t.TempDir(), "gosshd-agent.exe.new-123")} {
		if err := replaceAndRestart(canonical, path); err == nil {
			t.Fatalf("accepted unrelated replacement: %s", path)
		}
	}
}

func TestWindowsUpdateEncodedScriptRoundTrip(t *testing.T) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell("[Console]::Write("+quotePowerShell("中文 'quoted' $()")+")"))
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("encoded script: %v\n%s", err, output)
	}
	// PowerShell's redirected default encoding is OEM, so only demand the
	// literal metacharacters survive rather than assuming UTF-8 on all hosts.
	if !strings.Contains(string(output), "'quoted' $()") {
		t.Fatalf("encoded script was evaluated incorrectly: %q", output)
	}
}

func TestWindowsUpdateFailureRetainsCanonicalAndRestarts(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "gosshd-agent.exe")
	pending := canonical + ".new-123"
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, binary, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("replacement must not be installed while locked"), 0600); err != nil {
		t.Fatal(err)
	}
	pointer, err := windows.UTF16PtrFromString(canonical)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(pointer, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	// Use a process known to have already exited instead of a guessed PID.
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(filepath.Join(systemDir, "cmd.exe"), "/c", "exit", "0")
	parent.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := parent.Run(); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(dir, "result.json")
	args := []string{"-test.run=^TestWindowsUpdateProcess$"}
	script := windowsUpdateScript(canonical, pending, parent.Process.Pid, args, false)
	// Exercise the actual failing MoveFileEx and recovery with one attempt; the
	// 60-attempt production retry policy need not make this regression slow.
	script = strings.Replace(script, "$attempt -lt 60", "$attempt -lt 1", 1)
	command := exec.Command(filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	command.Env = append(os.Environ(), "GOSSHD_UPDATE_TEST_MODE=probe", "GOSSHD_UPDATE_TEST_RESULT="+result)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("failed replacement was reported as successful: %s", output)
	}
	deadline := time.Now().Add(10 * time.Second)
	var probe updateProbe
	for {
		data, err := os.ReadFile(result)
		if err == nil && json.Unmarshal(data, &probe) == nil {
			break
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(canonical + ".update-error.log")
			t.Fatalf("previous canonical agent was not restarted: %v\n%s", err, log)
		}
		time.Sleep(50 * time.Millisecond)
	}
	process, err := os.FindProcess(probe.PID)
	if err == nil {
		_, _ = process.Wait()
	}
	installed, err := os.ReadFile(canonical)
	if err != nil || !reflect.DeepEqual(installed, binary) {
		t.Fatalf("failed update changed previous binary: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("failed replacement leaked temporary binary: %v", err)
	}
	if data, err := os.ReadFile(canonical + ".update-error.log"); err != nil || !strings.Contains(string(data), "Cannot replace agent executable") {
		t.Fatalf("replacement failure diagnostic missing: %v %s", err, data)
	}
}
