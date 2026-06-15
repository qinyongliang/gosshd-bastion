package server_test

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd/internal/agent"
	"github.com/qinyongliang/gosshd/internal/server"

	gossh "golang.org/x/crypto/ssh"
)

func TestSSHExecThroughAgent(t *testing.T) {
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := server.NewApp(server.Config{
		PublicHost: httpLn.Addr().String(),
		AgentPath:  "dist/agent",
	})
	go func() {
		if err := app.RunListeners(ctx, httpLn, sshLn); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()

	shell, command := testShell()
	client, err := agent.New(agent.Config{
		Server: "http://" + httpLn.Addr().String(),
		IDFile: filepath.Join(t.TempDir(), "agent.json"),
		Shell:  shell,
		Root:   ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := client.Run(ctx); err != nil {
			t.Logf("agent stopped: %v", err)
		}
	}()
	waitForAgent(t, app.Registry(), client.ID())

	cfg := &gossh.ClientConfig{
		User:            client.ID(),
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshClient, err := gossh.Dial("tcp", sshLn.Addr().String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sshClient.Close()
	session, err := sshClient.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var out bytes.Buffer
	session.Stdout = &out
	if err := session.Run(command); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "gosshd-ok\n" && got != "gosshd-ok\r\n" {
		t.Fatalf("unexpected output %q", got)
	}
}

func waitForAgent(t *testing.T, registry *server.AgentRegistry, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := registry.Get(id); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("agent %s did not register", id)
}

func testShell() (string, string) {
	if runtime.GOOS == "windows" {
		return "powershell.exe", "Write-Output gosshd-ok"
	}
	return "/bin/sh", "printf 'gosshd-ok\\n'"
}
