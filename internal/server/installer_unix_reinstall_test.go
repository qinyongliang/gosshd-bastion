package server

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type unixInstallFixture struct {
	script, root, target, events, home string
	env                                []string
}

func newUnixInstallFixture(t *testing.T, platform string) *unixInstallFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("isolated POSIX installer execution on Linux/WSL")
	}
	root := t.TempDir()
	f := &unixInstallFixture{root: root, target: filepath.Join(root, "usr/local/bin/gosshd-agent"), events: filepath.Join(root, "events"), home: filepath.Join(root, `home <&"' $cash%`)}
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{bin, f.home, filepath.Dir(f.target), filepath.Join(root, "tmp"), filepath.Join(root, "etc/systemd/system"), filepath.Join(root, "Library/LaunchDaemons")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]string{
		"uname":  `case "$1" in -s) echo "$TEST_PLATFORM";; -m) echo x86_64;; esac`,
		"id":     `case "$1" in -u) echo 0;; -un) echo installer;; esac`,
		"getent": `printf 'installer:x:1000:1000::%s:/bin/sh\n' "$TEST_HOME"`,
		"dscl":   `printf 'NFSHomeDirectory: %s\n' "$TEST_HOME"`,
		"curl": `url=''; output=''; while [ "$#" -gt 0 ]; do case "$1" in -o) output="$2"; shift 2;; -*) shift;; *) url="$1"; shift;; esac; done
case "$url" in *.sha256) if [ "${TEST_BAD_SHA:-0}" = 1 ]; then printf '%064d\n' 0; else sha256sum "$TEST_BINARY" | awk '{print $1}'; fi;; *) cp "$TEST_BINARY" "$output";; esac`,
		"systemctl": `printf 'systemctl %s\n' "$*" >> "$TEST_EVENTS"
case "$1" in show) echo "${TEST_LOAD_STATE:-loaded}";; stop) [ "${TEST_STOP_FAIL:-0}" != 1 ];; esac`,
		"launchctl": `printf 'launchctl %s\n' "$*" >> "$TEST_EVENTS"
case "$1" in print) if [ "${TEST_LOAD_STATE:-loaded}" = not-found ] && [ ! -f "$TEST_EVENTS.bootstrapped" ]; then exit 1; fi; echo 'state = running';; bootstrap) touch "$TEST_EVENTS.bootstrapped";; bootout) [ "${TEST_STOP_FAIL:-0}" != 1 ];; esac`,
		"lsof":   `exit 1`,
		"plutil": `printf 'plutil %s\n' "$*" >> "$TEST_EVENTS"; exit "${TEST_PLIST_FAIL:-0}"`,
		"chown":  `printf 'chown %s\n' "$*" >> "$TEST_EVENTS"`,
		"mv":     `printf 'replace %s\n' "$*" >> "$TEST_EVENTS"; exec /bin/mv "$@"`,
	}
	for name, body := range commands {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	app := NewApp(Config{PublicHost: "relay.example.com", PublicSSHPort: 22022})
	rec := httptest.NewRecorder()
	app.handleInstallSH(rec, httptest.NewRequest("GET", "http://relay.example.com/install/test.sh", nil), `token<&"'$literal`)
	script := rec.Body.String()
	for _, path := range []string{"/usr/local/bin", "/var/lib/gosshd", "/etc/systemd/system", "/Library/LaunchDaemons"} {
		script = strings.ReplaceAll(script, path, filepath.Join(root, path))
	}
	f.script = filepath.Join(root, "installer.sh")
	if err := os.WriteFile(f.script, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	// Do not accidentally inherit real Android or installer fixture settings.
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if key != "PATH" && key != "HOME" && key != "TMPDIR" && key != "SUDO_USER" && key != "ANDROID_ROOT" && key != "TERMUX_VERSION" && !strings.HasPrefix(key, "TEST_") && !strings.HasPrefix(key, "GOSSHD_TEST_") {
			f.env = append(f.env, v)
		}
	}
	f.env = append(f.env, "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+f.home, "SUDO_USER=installer", "TMPDIR="+filepath.Join(root, "tmp"), "TEST_PLATFORM="+platform, "TEST_HOME="+f.home, "TEST_EVENTS="+f.events, "TEST_BINARY="+binary, "GOSSHD_TEST_ANDROID_AGENT=1", "GOSSHD_TEST_AGENT_RECORD="+filepath.Join(root, "agent-record"))
	return f
}
func (f *unixInstallFixture) run(mode string, extra ...string) ([]byte, error) {
	cmd := exec.Command("sh", f.script, mode)
	cmd.Env = append(append([]string{}, f.env...), extra...)
	return cmd.CombinedOutput()
}
func (f *unixInstallFixture) eventLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(f.events)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestUnixInstallerServiceReinstall(t *testing.T) {
	for _, platform := range []string{"Linux", "Darwin"} {
		for _, state := range []string{"loaded", "not-found"} {
			t.Run(platform+"/"+state, func(t *testing.T) {
				f := newUnixInstallFixture(t, platform)
				id := filepath.Join(f.root, "var/lib/gosshd/agent.json")
				if err := os.MkdirAll(filepath.Dir(id), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(id, []byte("stable-identity"), 0600); err != nil {
					t.Fatal(err)
				}
				if out, err := f.run("install", "TEST_LOAD_STATE="+state); err != nil {
					t.Fatalf("installer: %v\n%s", err, out)
				}
				events := f.eventLog(t)
				stop, start := "systemctl stop gosshd-agent", "systemctl restart gosshd-agent"
				if platform == "Darwin" {
					stop = "launchctl bootout system/com.gosshd.agent"
					start = "launchctl bootstrap system "
				}
				replacement := strings.Index(events, "replace ")
				if replacement < 0 || strings.Index(events, start) < replacement {
					t.Fatalf("missing replacement/start order: %s", events)
				}
				if state == "loaded" {
					if i := strings.Index(events, stop); i < 0 || i > replacement {
						t.Fatalf("must stop before replacing: %s", events)
					}
				} else if strings.Contains(events, stop) {
					t.Fatalf("stopped a missing service: %s", events)
				}
				if body, _ := os.ReadFile(id); string(body) != "stable-identity" {
					t.Fatal("identity replaced")
				}
				if platform == "Darwin" {
					data, err := os.ReadFile(filepath.Join(f.root, "Library/LaunchDaemons/com.gosshd.agent.plist"))
					if err != nil {
						t.Fatal(err)
					}
					decoder := xml.NewDecoder(bytes.NewReader(data))
					for {
						_, err = decoder.Token()
						if err != nil {
							break
						}
					}
					if err != io.EOF {
						t.Fatalf("invalid plist XML: %v\n%s", err, data)
					}
					for _, want := range []string{"&lt;", "&amp;", "--id-file", "--root", "RunAtLoad", "KeepAlive"} {
						if !bytes.Contains(data, []byte(want)) {
							t.Errorf("plist missing %s: %s", want, data)
						}
					}
					if strings.Contains(events, "systemctl ") {
						t.Fatalf("Darwin invoked systemd: %s", events)
					}
				}
			})
		}
	}
}
func TestUnixInstallerFailurePreservesExistingFiles(t *testing.T) {
	for _, platform := range []string{"Linux", "Darwin"} {
		for _, failure := range []string{"TEST_STOP_FAIL=1", "TEST_BAD_SHA=1"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				f := newUnixInstallFixture(t, platform)
				if err := os.WriteFile(f.target, []byte("old-binary"), 0700); err != nil {
					t.Fatal(err)
				}
				if out, err := f.run("install", failure); err == nil {
					t.Fatalf("failure swallowed: %s", out)
				}
				if b, _ := os.ReadFile(f.target); string(b) != "old-binary" {
					t.Fatal("failed installation replaced old binary")
				}
				events := f.eventLog(t)
				if strings.Contains(events, "replace ") || strings.Contains(events, "bootstrap ") || strings.Contains(events, "restart ") {
					t.Fatalf("failure continued: %s", events)
				}
				if failure == "TEST_BAD_SHA=1" && (strings.Contains(events, "stop ") || strings.Contains(events, "bootout ")) {
					t.Fatalf("checksum failure stopped old service: %s", events)
				}
			})
		}
	}
}
func TestUnixInstallerRunStopsOnlyMatchingExecutable(t *testing.T) {
	f := newUnixInstallFixture(t, "Linux")
	target := filepath.Join(f.root, "tmp/gosshd-agent")
	binary, _ := os.Executable()
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, data, 0700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(target)
	child.Env = append(f.env, "GOSSHD_TEST_AGENT_WAIT=1")
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- child.Wait() }()
	t.Cleanup(func() { _ = child.Process.Kill() })
	if out, err := f.run("run"); err != nil {
		t.Fatalf("run reinstall: %v %s", err, out)
	}
	// A correctly stopped child has an exit status; do not rely on /proc timing.
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("old fixture should have been terminated")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old fixture was not stopped")
	}
	if events := f.eventLog(t); strings.Contains(events, "systemctl ") || strings.Contains(events, "launchctl ") {
		t.Fatalf("foreground run stopped a service: %s", events)
	}
}
func TestUnixInstallerStopTimeoutIsFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	rec := httptest.NewRecorder()
	NewApp(Config{}).handleInstallSH(rec, httptest.NewRequest("GET", "http://relay.example.com/install/test.sh", nil), "test")
	script := rec.Body.String()
	start := strings.Index(script, "# Path-checked agent process helpers.")
	end := strings.Index(script, "# End path-checked agent process helpers.")
	if start < 0 || end < start {
		t.Fatal("process helpers missing")
	}
	body := "os=linux\n" + script[start:end] + "\n" + `agent_matching_pids() { echo 123; }
agent_pid_matches() { return 0; }
kill() { :; }
sleep() { :; }
if stop_agent_processes; then echo 'timeout swallowed'; exit 1; fi
`
	cmd := exec.Command("sh", "-eu")
	cmd.Stdin = strings.NewReader(body)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("timeout handling: %v %s", err, out)
	}
}
