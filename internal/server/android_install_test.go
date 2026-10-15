package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The copied test executable is the mock downloaded agent. Unlike a shell
// fixture, its /proc/PID/exe identity exercises the root controller faithfully.
func init() {
	if os.Getenv("GOSSHD_TEST_ANDROID_AGENT") != "1" || filepath.Base(os.Args[0]) != "gosshd-agent" {
		return
	}
	record, _ := json.Marshal(struct {
		Args []string
		Home string
	}{os.Args[1:], os.Getenv("HOME")})
	if err := os.WriteFile(os.Getenv("GOSSHD_TEST_AGENT_RECORD"), record, 0o600); err != nil {
		os.Exit(2)
	}
	if os.Getenv("GOSSHD_TEST_AGENT_WAIT") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(0)
}

type androidInstallFixture struct {
	script, root, prefix, home, agentDir, bootDir, record, events string
	env                                                           []string
}

func newAndroidInstallFixture(t *testing.T, termux bool, abi string) *androidInstallFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("execute generated Android shell scripts in Linux (including WSL)")
	}
	root := t.TempDir()
	f := &androidInstallFixture{root: root, home: filepath.Join(root, "home with 'quotes"), prefix: filepath.Join(root, "termux-prefix"),
		bootDir: filepath.Join(root, "root-service.d"), record: filepath.Join(root, "arguments.json"), events: filepath.Join(root, "events")}
	f.agentDir = filepath.Join(root, "native-agent")
	uid := "0"
	if termux {
		f.agentDir = filepath.Join(f.home, ".gosshd")
		uid = "2000"
	}
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{bin, f.home, filepath.Join(f.prefix, "bin"), f.agentDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"sh", "sleep", "cp", "mkdir", "chmod", "mktemp", "tr", "sha256sum", "sed", "mv", "rm", "cat", "readlink", "nohup", "awk"} {
		tool, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(tool, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"sh", "bash"} {
		if err := os.Symlink("/bin/"+name, filepath.Join(f.prefix, "bin", name)); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		t.Fatal(err)
	}
	input.Close()
	sum := fmt.Sprintf("%x", hash.Sum(nil))
	commands := map[string]string{
		"id":      "printf '%s\\n' \"$MOCK_UID\"",
		"uname":   "case \"$1\" in -s) echo Linux;; -m) echo aarch64;; esac",
		"dpkg":    "printf '%s\\n' \"$MOCK_ABI\"",
		"getprop": "case \"$1\" in ro.product.cpu.abi) echo \"$MOCK_ABI\";; ro.build.version.sdk) echo \"$MOCK_SDK\";; esac",
		"curl": `url=''; output=''; while [ "$#" -gt 0 ]; do case "$1" in -o|-O) output="$2"; shift 2;; -*) shift;; *) url="$1"; shift;; esac; done
printf '%s\n' "$url" >> "$MOCK_EVENTS"
case "$url" in *.sha256) printf '%s\n' "$MOCK_SHA" > "$output";; *) cp "$MOCK_BINARY" "$output";; esac`,
		"sv":             `printf 'sv %s\n' "$*" >> "$MOCK_EVENTS"`,
		"service-daemon": `printf 'service-daemon %s\n' "$*" >> "$MOCK_EVENTS"`,
	}
	for name, body := range commands {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app := NewApp(Config{PublicHost: "relay.example.com", PublicSSHPort: 22022})
	rec := httptest.NewRecorder()
	app.handleInstallSH(rec, httptest.NewRequest(http.MethodGet, "http://relay.example.com/install/test.sh", nil), "token'$literal")
	script := strings.ReplaceAll(rec.Body.String(), "android_dir=/data/local/gosshd", "android_dir="+shellQuote(f.agentDir))
	script = strings.ReplaceAll(script, "/data/adb/service.d", f.bootDir)
	f.script = filepath.Join(root, "installer.sh")
	if err := os.WriteFile(f.script, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	// Override these variables without accidentally inheriting a developer's
	// actual Termux, shell or mock-fixture environment.
	overridden := map[string]bool{"HOME": true, "PREFIX": true, "PATH": true, "SHELL": true, "ANDROID_ROOT": true, "TERMUX_VERSION": true}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !overridden[key] && !strings.HasPrefix(key, "MOCK_") && !strings.HasPrefix(key, "GOSSHD_TEST_") {
			f.env = append(f.env, value)
		}
	}
	f.env = append(f.env, "HOME="+f.home, "PREFIX="+f.prefix, "SHELL="+filepath.Join(f.prefix, "bin", "bash"), "PATH="+bin,
		"ANDROID_ROOT=/system", "MOCK_UID="+uid, "MOCK_ABI="+abi, "MOCK_SDK=24", "MOCK_SHA="+sum, "MOCK_BINARY="+binary, "MOCK_EVENTS="+f.events,
		"GOSSHD_TEST_ANDROID_AGENT=1", "GOSSHD_TEST_AGENT_RECORD="+f.record)
	if termux {
		f.env = append(f.env, "TERMUX_VERSION=test")
	}
	return f
}

func (f *androidInstallFixture) run(mode string, extra ...string) ([]byte, error) {
	cmd := exec.Command("sh", f.script, mode)
	cmd.Env = append(append([]string{}, f.env...), extra...)
	return cmd.CombinedOutput()
}

func (f *androidInstallFixture) assertAgent(t *testing.T) {
	t.Helper()
	body, err := os.ReadFile(f.record)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Args []string
		Home string
	}
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for i := 0; i+1 < len(record.Args); i += 2 {
		values[record.Args[i]] = record.Args[i+1]
	}
	home, shell := f.agentDir, "/bin/sh"
	if strings.HasSuffix(f.agentDir, ".gosshd") {
		home = f.home
		shell = filepath.Join(f.prefix, "bin", "bash")
	} else {
		shell = filepath.Join(f.root, "bin", "sh")
	}
	for flag, want := range map[string]string{"--server": "http://relay.example.com", "--enrollment-token": "token'$literal", "--ssh-port": "22022", "--id-file": filepath.Join(f.agentDir, "agent.json"), "--shell": shell, "--root": home} {
		if values[flag] != want {
			t.Errorf("%s = %q, want %q (args=%v)", flag, values[flag], want, record.Args)
		}
	}
	if record.Home != home {
		t.Errorf("HOME=%q, want %q", record.Home, home)
	}
}

func TestAndroidInstallerRunBothABIsAndEnvironments(t *testing.T) {
	for _, tc := range []struct {
		name, abi, asset string
		termux           bool
	}{
		{"termux-v7-on-v8-kernel", "arm", "armv7", true}, {"termux-v8", "aarch64", "arm64", true},
		{"root-v7", "armeabi-v7a", "armv7", false}, {"root-v8", "arm64-v8a", "arm64", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAndroidInstallFixture(t, tc.termux, tc.abi)
			if out, err := f.run("run"); err != nil {
				t.Fatalf("installer: %v\n%s", err, out)
			}
			f.assertAgent(t)
			events, _ := os.ReadFile(f.events)
			if !strings.Contains(string(events), "/download/agent/android/"+tc.asset+"\n") || strings.Contains(string(events), "/download/agent/linux/") {
				t.Fatalf("wrong download platform: %s", events)
			}
		})
	}
}

func TestAndroidInstallerTermuxServiceAndBoot(t *testing.T) {
	f := newAndroidInstallFixture(t, true, "arm")
	if out, err := f.run("install"); err != nil {
		t.Fatalf("installer: %v\n%s", err, out)
	}
	for _, path := range []string{filepath.Join(f.prefix, "var/service/gosshd-agent/run"), filepath.Join(f.home, ".termux/boot/gosshd-agent")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("missing executable service/boot file: %s (%v)", path, err)
		}
		cmd := exec.Command("sh", "-n", path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("invalid script: %s: %v %s", path, err, out)
		}
	}
	boot := exec.Command(filepath.Join(f.home, ".termux/boot/gosshd-agent"))
	boot.Env = f.env
	if out, err := boot.CombinedOutput(); err != nil {
		t.Fatalf("Termux boot script: %v %s", err, out)
	}
	cmd := exec.Command(filepath.Join(f.prefix, "var/service/gosshd-agent/run"))
	cmd.Env = f.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("service run: %v %s", err, out)
	}
	f.assertAgent(t)
	if out, err := f.run("install"); err != nil {
		t.Fatalf("repeat install: %v %s", err, out)
	}
	events, _ := os.ReadFile(f.events)
	if !strings.Contains(string(events), "sv -w 10 down ") || !strings.Contains(string(events), "sv -w 15 up ") {
		t.Fatalf("missing service lifecycle: %s", events)
	}
}

func TestAndroidInstallerRootServiceBootAndReinstall(t *testing.T) {
	for _, boot := range []bool{false, true} {
		t.Run(fmt.Sprintf("boot=%t", boot), func(t *testing.T) {
			f := newAndroidInstallFixture(t, false, "arm64-v8a")
			if boot {
				if err := os.Mkdir(f.bootDir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// A stale PID for an unrelated process must never authorize killing it.
			if err := os.WriteFile(filepath.Join(f.agentDir, "agent.pid"), []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
				t.Fatal(err)
			}
			controller := filepath.Join(f.agentDir, "control.sh")
			t.Cleanup(func() { cmd := exec.Command(controller, "stop"); cmd.Env = f.env; _ = cmd.Run() })
			out, err := f.run("install", "GOSSHD_TEST_AGENT_WAIT=1")
			if err != nil {
				t.Fatalf("root install: %v\n%s", err, out)
			}
			f.assertAgent(t)
			firstPID, _ := os.ReadFile(filepath.Join(f.agentDir, "agent.pid"))
			if boot {
				path := filepath.Join(f.bootDir, "gosshd-agent.sh")
				if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o100 == 0 {
					t.Fatalf("boot hook: %v", err)
				}
				hook := exec.Command(path)
				hook.Env = f.env
				if out, err := hook.CombinedOutput(); err != nil {
					t.Fatalf("root boot hook: %v %s", err, out)
				}
				if pid, _ := os.ReadFile(filepath.Join(f.agentDir, "agent.pid")); string(pid) != string(firstPID) {
					t.Fatal("boot hook duplicated a running agent")
				}
			} else if !strings.Contains(string(out), "reboot startup is not configured") {
				t.Fatalf("missing honest boot guidance: %s", out)
			}
			identity := filepath.Join(f.agentDir, "agent.json")
			if err := os.WriteFile(identity, []byte("stable-identity"), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := f.run("install", "GOSSHD_TEST_AGENT_WAIT=1"); err != nil {
				t.Fatalf("repeat root install: %v\n%s", err, out)
			}
			secondPID, _ := os.ReadFile(filepath.Join(f.agentDir, "agent.pid"))
			if string(firstPID) == string(secondPID) {
				t.Fatal("reinstall did not restart agent")
			}
			if body, _ := os.ReadFile(identity); string(body) != "stable-identity" {
				t.Fatalf("identity overwritten: %s", body)
			}
			cmd := exec.Command(controller, "status")
			cmd.Env = f.env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("status: %v %s", err, out)
			}
			stop := exec.Command(controller, "stop")
			stop.Env = f.env
			if out, err := stop.CombinedOutput(); err != nil {
				t.Fatalf("stop: %v %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(f.agentDir, "agent.pid")); !os.IsNotExist(err) {
				t.Fatalf("stop left a PID file: %v", err)
			}
			stopped := exec.Command(controller, "status")
			stopped.Env = f.env
			if out, err := stopped.CombinedOutput(); err == nil || !strings.Contains(string(out), "stopped") {
				t.Fatalf("stopped status: %v %s", err, out)
			}
		})
	}
}

func TestAndroidInstallerRejectsBadChecksumWithoutReplacingAgent(t *testing.T) {
	f := newAndroidInstallFixture(t, true, "arm")
	target := filepath.Join(f.agentDir, "gosshd-agent")
	if err := os.WriteFile(target, []byte("previous-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("run", "MOCK_SHA="+strings.Repeat("0", 64)); err == nil || !strings.Contains(string(out), "FAILED") {
		t.Fatalf("expected checksum rejection, got %v: %s", err, out)
	}
	if body, _ := os.ReadFile(target); string(body) != "previous-binary" {
		t.Fatalf("binary was replaced: %s", body)
	}
	entries, _ := filepath.Glob(filepath.Join(f.agentDir, ".gosshd-agent-*"))
	if len(entries) != 0 {
		t.Fatalf("download temps leaked: %v", entries)
	}
}

func TestAndroidInstallerRejectsUnsupportedAndUnprivilegedEnvironments(t *testing.T) {
	for _, tc := range []struct {
		name, abi, mode, extra, want string
		termux                       bool
	}{
		{"unsupported ABI", "x86_64", "run", "", "unsupported Android ABI", true},
		{"old Android", "arm", "run", "MOCK_SDK=23", "API 24", true},
		{"native nonroot", "arm64-v8a", "run", "MOCK_UID=2000", "requires a root shell", false},
		{"bad mode", "arm", "oops", "", "usage:", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAndroidInstallFixture(t, tc.termux, tc.abi)
			if !tc.termux {
				f.env = append(f.env, "PREFIX=")
			}
			var extras []string
			if tc.extra != "" {
				extras = append(extras, tc.extra)
			}
			out, err := f.run(tc.mode, extras...)
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("want error %q, got %v %s", tc.want, err, out)
			}
		})
	}
}

func TestAndroidEnrollmentCommandsDoNotRequireSudo(t *testing.T) {
	enrollment := agentEnrollmentResponse("id", "token", "https://relay.example.com")
	for _, command := range []string{enrollment.InstallAndroid, enrollment.ServiceAndroid} {
		if command == "" || !strings.Contains(command, "wget") || strings.Contains(command, "sudo") || !strings.Contains(command, "/install/token.sh") || !strings.Contains(command, "mktemp") {
			t.Fatalf("invalid Android command: %s", command)
		}
	}
	if !strings.HasSuffix(enrollment.ServiceAndroid, " install)") {
		t.Fatalf("missing background install mode: %s", enrollment.ServiceAndroid)
	}
}

func TestAndroidInstallerMissingDependenciesAndWgetFallback(t *testing.T) {
	for _, tc := range []struct{ name, missing, mode, want string }{
		{"services", "sv", "install", "pkg install termux-services"},
		{"downloader", "curl", "run", "Install curl or wget"},
		{"wget", "curl", "run", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAndroidInstallFixture(t, true, "aarch64")
			path := filepath.Join(f.root, "bin", tc.missing)
			if tc.name == "wget" {
				if err := os.Rename(path, filepath.Join(f.root, "bin", "wget")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			out, err := f.run(tc.mode)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("wget install: %v %s", err, out)
				}
				f.assertAgent(t)
			} else if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("want dependency error %q, got %v %s", tc.want, err, out)
			}
		})
	}
}

func TestAndroidInstallerMissingRootControlTools(t *testing.T) {
	for _, utility := range []string{"nohup", "readlink"} {
		t.Run(utility, func(t *testing.T) {
			f := newAndroidInstallFixture(t, false, "arm64-v8a")
			if err := os.Remove(filepath.Join(f.root, "bin", utility)); err != nil {
				t.Fatal(err)
			}
			out, err := f.run("install")
			if err == nil || !strings.Contains(string(out), "requires "+utility) {
				t.Fatalf("missing tool %s: %v %s", utility, err, out)
			}
			if _, err := os.Stat(f.record); !os.IsNotExist(err) {
				t.Fatal("started agent despite missing controller dependency")
			}
		})
	}
}

func TestAndroidInstallerFailedServiceStopPreservesInstallation(t *testing.T) {
	f := newAndroidInstallFixture(t, true, "arm")
	target := filepath.Join(f.agentDir, "gosshd-agent")
	runner := filepath.Join(f.agentDir, "run.sh")
	for _, path := range []string{target, runner} {
		if err := os.WriteFile(path, []byte("previous-content"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(f.prefix, "var/service/gosshd-agent"), 0700); err != nil {
		t.Fatal(err)
	}
	sv := filepath.Join(f.root, "bin/sv")
	if err := os.WriteFile(sv, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run("install"); err == nil {
		t.Fatalf("failed stop was ignored: %s", out)
	}
	for _, path := range []string{target, runner} {
		if body, _ := os.ReadFile(path); string(body) != "previous-content" {
			t.Fatalf("stop failure replaced %s", filepath.Base(path))
		}
	}
}
