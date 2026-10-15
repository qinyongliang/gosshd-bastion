package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/relay"
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

func TestDefaultRootUsesUserHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Server: "http://agent.example.test:8880",
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Clean(client.cfg.Root), filepath.Clean(home); got != want {
		t.Fatalf("root mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestExplicitRootIsPreserved(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	client, err := New(Config{
		Server: "http://agent.example.test:8880",
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
		Server: "http://agent.example.test:8880",
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

func TestCommandEnvironmentAddsDefaultTERM(t *testing.T) {
	env := commandEnvironment([]string{"PATH=/usr/bin"}, "/root")
	if got, want := env[1], "TERM=xterm-256color"; got != want {
		t.Fatalf("TERM mismatch: got %q want %q", got, want)
	}
	if got, want := env[2], "HOME=/root"; got != want {
		t.Fatalf("HOME mismatch: got %q want %q", got, want)
	}
	if got, want := env[3], "XDG_CONFIG_HOME=/root/.config"; got != want {
		t.Fatalf("XDG_CONFIG_HOME mismatch: got %q want %q", got, want)
	}
}

func TestCommandEnvironmentPreservesExistingTERM(t *testing.T) {
	env := commandEnvironment([]string{"TERM=vt100", "PATH=/usr/bin"}, "/root")
	if len(env) != 4 || env[0] != "TERM=vt100" {
		t.Fatalf("existing TERM should be preserved: %#v", env)
	}
	if got, want := env[2], "HOME=/root"; got != want {
		t.Fatalf("HOME mismatch: got %q want %q", got, want)
	}
	if got, want := env[3], "XDG_CONFIG_HOME=/root/.config"; got != want {
		t.Fatalf("XDG_CONFIG_HOME mismatch: got %q want %q", got, want)
	}
}

func TestClientInstanceIsNotCopiedWithAgentIDFile(t *testing.T) {
	cfg := Config{Server: "http://agent.example.test:8880", IDFile: filepath.Join(t.TempDir(), "agent.json"), Root: t.TempDir()}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.id != second.id {
		t.Fatal("persisted runtime ID changed")
	}
	if !protocol.IsValidID(first.instanceID) || !protocol.IsValidID(second.instanceID) || first.instanceID == second.instanceID {
		t.Fatal("distinct clients need distinct valid in-memory instance IDs")
	}
	data, err := os.ReadFile(cfg.IDFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("instance_id")) || bytes.Contains(data, []byte(first.instanceID)) || bytes.Contains(data, []byte(second.instanceID)) {
		t.Fatal("instance ID must not be persisted or copied with the installation")
	}
}

func TestClientReconnectSendsStableInstanceID(t *testing.T) {
	hellos := make(chan protocol.AgentHello, 2)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn := relay.NewWSConn(ws)
		defer conn.Close()
		hello, err := protocol.ReadJSONLine[protocol.AgentHello](bufio.NewReader(conn))
		if err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		hellos <- hello
		if err := protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: true}); err != nil {
			t.Errorf("write hello response: %v", err)
		}
	}))
	defer srv.Close()
	client, err := New(Config{Server: srv.URL, IDFile: filepath.Join(t.TempDir(), "agent.json"), Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		if err := client.runOnce(ctx); err == nil {
			t.Fatal("closed connection should trigger reconnect")
		}
		select {
		case hello := <-hellos:
			if hello.InstanceID != client.instanceID || !protocol.IsValidID(hello.InstanceID) || hello.ID != client.ID() || hello.BootID != client.bootID {
				t.Fatalf("reconnect did not preserve instance identity: %+v", hello)
			}
		case <-ctx.Done():
			t.Fatal("Agent did not send hello")
		}
	}
}
