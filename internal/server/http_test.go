package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunSHUsesRunDownloadFlow(t *testing.T) {
	app := NewApp(Config{PublicHost: "relay.example.com", AgentToken: "secret"})
	req := httptest.NewRequest(http.MethodGet, "http://relay.example.com/run.sh", nil)
	rec := httptest.NewRecorder()

	app.runSH(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(body, "http://relay.example.com/download/agent/${os}/${arch}") {
		t.Fatalf("run.sh did not include agent download URL:\n%s", body)
	}
	if !strings.Contains(body, `exec "$tmp" --server "http://relay.example.com" --token "secret"`) {
		t.Fatalf("run.sh did not execute agent with expected server/token:\n%s", body)
	}
	if !strings.Contains(body, `export GOSSHD_SSH_HOST="relay.example.com"`) || !strings.Contains(body, `export GOSSHD_SSH_PORT="22"`) {
		t.Fatalf("run.sh did not include expected SSH hint:\n%s", body)
	}
}

func TestRunSHDefaultsToRequestHost(t *testing.T) {
	app := NewApp(Config{})
	req := httptest.NewRequest(http.MethodGet, "http://lan.example.test:8080/run.sh", nil)
	rec := httptest.NewRecorder()

	app.runSH(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(body, "http://lan.example.test:8080/download/agent/${os}/${arch}") {
		t.Fatalf("run.sh did not use request host for download URL:\n%s", body)
	}
	if !strings.Contains(body, `exec "$tmp" --server "http://lan.example.test:8080"`) {
		t.Fatalf("run.sh did not use request host for server URL:\n%s", body)
	}
	if !strings.Contains(body, `export GOSSHD_SSH_HOST="lan.example.test"`) || !strings.Contains(body, `export GOSSHD_SSH_PORT="22"`) {
		t.Fatalf("run.sh did not use request host for SSH hint:\n%s", body)
	}
}

func TestRunSHUsesPublicSSHPortForDockerMappedServer(t *testing.T) {
	app := NewApp(Config{
		PublicHost:    "agent.example.test:8880",
		SSHListen:     ":22",
		PublicSSHPort: "2222",
	})
	req := httptest.NewRequest(http.MethodGet, "http://agent.example.test:8880/run.sh", nil)
	rec := httptest.NewRecorder()

	app.runSH(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(body, `exec "$tmp" --server "http://agent.example.test:8880"`) ||
		!strings.Contains(body, `export GOSSHD_SSH_HOST="agent.example.test"`) ||
		!strings.Contains(body, `export GOSSHD_SSH_PORT="2222"`) {
		t.Fatalf("run.sh did not include Docker-mapped SSH hint:\n%s", body)
	}
}

func TestRunSHUsesSSHListenPortWhenPublicPortIsUnset(t *testing.T) {
	app := NewApp(Config{SSHListen: ":2222"})
	req := httptest.NewRequest(http.MethodGet, "http://relay.example.com:8880/run.sh", nil)
	rec := httptest.NewRecorder()

	app.runSH(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(body, `--server "http://relay.example.com:8880"`) ||
		!strings.Contains(body, `export GOSSHD_SSH_HOST="relay.example.com"`) ||
		!strings.Contains(body, `export GOSSHD_SSH_PORT="2222"`) {
		t.Fatalf("run.sh did not derive SSH listen port:\n%s", body)
	}
}

func TestRunPS1UsesRunDownloadFlow(t *testing.T) {
	app := NewApp(Config{PublicHost: "relay.example.com"})
	req := httptest.NewRequest(http.MethodGet, "http://relay.example.com/run.ps1", nil)
	rec := httptest.NewRecorder()

	app.runPS1(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(body, `$url = "http://relay.example.com/download/agent/windows/$arch"`) {
		t.Fatalf("run.ps1 did not include agent download URL:\n%s", body)
	}
	if !strings.Contains(body, `& $tmp --server "http://relay.example.com"`) {
		t.Fatalf("run.ps1 did not execute agent with expected server:\n%s", body)
	}
	if !strings.Contains(body, `$env:GOSSHD_SSH_HOST = "relay.example.com"`) || !strings.Contains(body, `$env:GOSSHD_SSH_PORT = "22"`) {
		t.Fatalf("run.ps1 did not include expected SSH hint:\n%s", body)
	}
}

func TestRunPS1DefaultsToRequestHost(t *testing.T) {
	app := NewApp(Config{})
	req := httptest.NewRequest(http.MethodGet, "http://lan.example.test:8080/run.ps1", nil)
	rec := httptest.NewRecorder()

	app.runPS1(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if !strings.Contains(body, `$url = "http://lan.example.test:8080/download/agent/windows/$arch"`) {
		t.Fatalf("run.ps1 did not use request host for download URL:\n%s", body)
	}
	if !strings.Contains(body, `& $tmp --server "http://lan.example.test:8080"`) {
		t.Fatalf("run.ps1 did not use request host for server URL:\n%s", body)
	}
	if !strings.Contains(body, `$env:GOSSHD_SSH_HOST = "lan.example.test"`) || !strings.Contains(body, `$env:GOSSHD_SSH_PORT = "22"`) {
		t.Fatalf("run.ps1 did not use request host for SSH hint:\n%s", body)
	}
}
