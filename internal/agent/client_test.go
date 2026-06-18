package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewRequiresExplicitServer(t *testing.T) {
	idFile := filepath.Join(t.TempDir(), "agent.json")
	_, err := New(Config{IDFile: idFile})
	if !errors.Is(err, errServerRequired) && (err == nil || err.Error() != "server is required") {
		t.Fatalf("expected server required error, got %v", err)
	}
	if _, statErr := os.Stat(idFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("server validation should happen before creating id file, stat error: %v", statErr)
	}
}

func TestSSHAddressUsesPublicSSHHint(t *testing.T) {
	client, err := New(Config{
		Server:  "http://qyl.my.to:8880",
		IDFile:  filepath.Join(t.TempDir(), "agent.json"),
		SSHHost: "qyl.my.to",
		SSHPort: "2222",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@qyl.my.to -p 2222"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSSHAddressOmitsDefaultPort(t *testing.T) {
	client, err := New(Config{
		Server:  "http://qyl.my.to:8880",
		IDFile:  filepath.Join(t.TempDir(), "agent.json"),
		SSHHost: "qyl.my.to",
		SSHPort: "22",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@qyl.my.to"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSSHAddressDoesNotTreatServerHTTPPortAsSSHPort(t *testing.T) {
	client, err := New(Config{
		Server: "http://qyl.my.to:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@qyl.my.to"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSSHAddressUsesEnvironmentHint(t *testing.T) {
	t.Setenv("GOSSHD_SSH_HOST", "qyl.my.to")
	t.Setenv("GOSSHD_SSH_PORT", "2222")
	client, err := New(Config{
		Server: "http://qyl.my.to:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@qyl.my.to -p 2222"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestDefaultRootUsesCurrentWorkingDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Server: "http://qyl.my.to:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Clean(client.cfg.Root), filepath.Clean(cwd); got != want {
		t.Fatalf("root mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestExplicitRootIsPreserved(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	client, err := New(Config{
		Server: "http://qyl.my.to:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
		Root:   root,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Clean(client.cfg.Root), filepath.Clean(root); got != want {
		t.Fatalf("root mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestExplicitShellIsPreserved(t *testing.T) {
	client, err := New(Config{
		Server: "http://qyl.my.to:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
		Shell:  "/custom/bash",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := client.cfg.Shell, "/custom/bash"; got != want {
		t.Fatalf("shell mismatch:\n got: %s\nwant: %s", got, want)
	}
}
