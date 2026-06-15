package agent

import (
	"path/filepath"
	"testing"
)

func TestSSHAddressUsesPublicSSHHint(t *testing.T) {
	client, err := New(Config{
		Server:  "http://agent.example.test:8880",
		IDFile:  filepath.Join(t.TempDir(), "agent.json"),
		SSHHost: "agent.example.test",
		SSHPort: "2222",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@agent.example.test -p 2222"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSSHAddressOmitsDefaultPort(t *testing.T) {
	client, err := New(Config{
		Server:  "http://agent.example.test:8880",
		IDFile:  filepath.Join(t.TempDir(), "agent.json"),
		SSHHost: "agent.example.test",
		SSHPort: "22",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@agent.example.test"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSSHAddressDoesNotTreatServerHTTPPortAsSSHPort(t *testing.T) {
	client, err := New(Config{
		Server: "http://agent.example.test:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@agent.example.test"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestSSHAddressUsesEnvironmentHint(t *testing.T) {
	t.Setenv("GOSSHD_SSH_HOST", "agent.example.test")
	t.Setenv("GOSSHD_SSH_PORT", "2222")
	client, err := New(Config{
		Server: "http://agent.example.test:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "ssh " + client.ID() + "@agent.example.test -p 2222"
	if got := client.SSHAddress(); got != want {
		t.Fatalf("SSHAddress mismatch:\n got: %s\nwant: %s", got, want)
	}
}
