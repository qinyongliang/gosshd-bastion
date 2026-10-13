package server

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func generatedHomeInstaller(t *testing.T, powershell bool) string {
	t.Helper()
	srv, _, app := newAPITestServer(t)
	defer srv.Close()
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", srv.URL+"/install/home-test.sh", nil)
	if powershell {
		app.handleInstallPS1(response, request, "home-test")
	} else {
		app.handleInstallSH(response, request, "home-test")
	}
	return response.Body.String()
}

func TestInstallerHomePowerShellParses(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell parser")
	}
	script := filepath.Join(t.TempDir(), "install.ps1")
	if err := os.WriteFile(script, []byte(generatedHomeInstaller(t, true)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSSHD_TEST_INSTALL_SCRIPT", script)
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", `$tokens=$null; $errors=$null; $null=[System.Management.Automation.Language.Parser]::ParseFile($env:GOSSHD_TEST_INSTALL_SCRIPT,[ref]$tokens,[ref]$errors); if ($errors.Count) { $errors | Out-String | Write-Error; exit 1 }`)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell syntax: %v\n%s", err, output)
	}
}

func TestInstallerHomeShellResolutionAndEscaping(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell execution")
	}
	script := generatedHomeInstaller(t, false)
	syntax := exec.Command("sh", "-n")
	syntax.Stdin = strings.NewReader(script)
	if output, err := syntax.CombinedOutput(); err != nil {
		t.Fatalf("installer syntax: %v\n%s", err, output)
	}
	start := strings.Index(script, "  # Resolve service shell home")
	end := strings.Index(script, "  # End service home resolution.")
	if start < 0 || end < start {
		t.Fatal("home resolution block not found")
	}
	fragment := script[start:end]
	for _, name := range []string{"home with spaces", `home'$cash%percent\quote"&|`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, name)
			bin := filepath.Join(root, "bin")
			for _, dir := range []string{home, bin} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			getent := "#!/bin/sh\n[ \"$1\" = passwd ] && [ \"$2\" = installer ] || exit 1\nprintf 'installer:x:1000:1000::%s:/bin/bash\\n' \"$GOSSHD_TEST_INSTALL_HOME\"\n"
			if err := os.WriteFile(filepath.Join(bin, "getent"), []byte(getent), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			t.Setenv("HOME", root)
			t.Setenv("SUDO_USER", "installer")
			t.Setenv("GOSSHD_TEST_INSTALL_HOME", home)
			body := fragment + "\nprintf '%s\\n' \"$agent_home\"\nsed \"s|@GOSSHD_AGENT_HOME@|$home_replacement|g\" <<'SERVICE'\n--root \"@GOSSHD_AGENT_HOME@\"\nSERVICE\n"
			command := exec.Command("sh", "-eu")
			command.Stdin = strings.NewReader(body)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("resolve home: %v\n%s", err, output)
			}
			escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$").Replace(home)
			want := home + "\n--root \"" + escaped + "\"\n"
			if string(output) != want {
				t.Fatalf("home unit fragment = %q, want %q", output, want)
			}
		})
	}
}
