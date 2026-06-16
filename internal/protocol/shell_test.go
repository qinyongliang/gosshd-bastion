package protocol

import (
	"os"
	"testing"
)

func TestDefaultShellLinuxUsesShellEnvironment(t *testing.T) {
	probe := testShellProbe(map[string]string{"SHELL": "/custom/bash"}, nil)

	if got, want := defaultShell("linux", probe), "/custom/bash"; got != want {
		t.Fatalf("defaultShell mismatch: got %q want %q", got, want)
	}
}

func TestDefaultShellLinuxUsesParentShellBeforeEnvironment(t *testing.T) {
	probe := testShellProbe(map[string]string{"SHELL": "/bin/zsh"}, map[string]bool{"/bin/bash": true})
	probe.getppid = func() int { return 42 }
	probe.readlink = func(path string) (string, error) {
		switch path {
		case "/proc/42/exe":
			return "/bin/bash", nil
		default:
			return "", os.ErrNotExist
		}
	}

	if got, want := defaultShell("linux", probe), "/bin/bash"; got != want {
		t.Fatalf("defaultShell mismatch: got %q want %q", got, want)
	}
}

func TestDefaultShellLinuxSkipsPipeShForParentShell(t *testing.T) {
	probe := testShellProbe(nil, map[string]bool{"/bin/sh": true, "/bin/bash": true})
	probe.getppid = func() int { return 42 }
	probe.readlink = func(path string) (string, error) {
		switch path {
		case "/proc/42/exe":
			return "/bin/sh", nil
		case "/proc/7/exe":
			return "/bin/bash", nil
		default:
			return "", os.ErrNotExist
		}
	}
	probe.readFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/42/stat":
			return []byte("42 (sh) S 7 1 1 0 -1 0 0 0 0 0 0 0 0 20 0 1 0 0 0 0"), nil
		case "/proc/7/stat":
			return []byte("7 (bash) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 20 0 1 0 0 0 0"), nil
		default:
			return nil, os.ErrNotExist
		}
	}

	if got, want := defaultShell("linux", probe), "/bin/bash"; got != want {
		t.Fatalf("defaultShell mismatch: got %q want %q", got, want)
	}
}

func TestDefaultShellLinuxUsesPasswdLoginShellWhenEnvUnset(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/bash\n"
	probe := testShellProbe(nil, map[string]bool{"/bin/bash": true})
	probe.currentUser = func() (shellUser, error) {
		return shellUser{UID: "1000", Username: "alice"}, nil
	}
	probe.readFile = func(path string) ([]byte, error) {
		if path != "/etc/passwd" {
			return nil, os.ErrNotExist
		}
		return []byte(passwd), nil
	}

	if got, want := defaultShell("linux", probe), "/bin/bash"; got != want {
		t.Fatalf("defaultShell mismatch: got %q want %q", got, want)
	}
}

func TestDefaultShellLinuxFallsBackToBashBeforeSh(t *testing.T) {
	probe := testShellProbe(nil, map[string]bool{
		"/bin/bash": true,
		"/bin/sh":   true,
	})

	if got, want := defaultShell("linux", probe), "/bin/bash"; got != want {
		t.Fatalf("defaultShell mismatch: got %q want %q", got, want)
	}
}

func TestDefaultShellWindowsUsesComspec(t *testing.T) {
	probe := testShellProbe(map[string]string{"COMSPEC": `C:\Windows\System32\cmd.exe`}, nil)

	if got, want := defaultShell("windows", probe), `C:\Windows\System32\cmd.exe`; got != want {
		t.Fatalf("defaultShell mismatch: got %q want %q", got, want)
	}
}

func testShellProbe(env map[string]string, available map[string]bool) shellProbe {
	return shellProbe{
		getenv: func(key string) string {
			return env[key]
		},
		lookPath: func(file string) (string, error) {
			if available[file] {
				return file, nil
			}
			return "", os.ErrNotExist
		},
		readlink: func(string) (string, error) {
			return "", os.ErrNotExist
		},
		getppid: func() int {
			return 0
		},
		readFile: func(string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		currentUser: func() (shellUser, error) {
			return shellUser{}, os.ErrNotExist
		},
	}
}
