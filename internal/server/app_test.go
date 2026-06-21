package server

import (
	"net/http/httptest"
	"testing"
)

func TestStartupHTTPBaseUsesPublicHost(t *testing.T) {
	app := NewApp(Config{PublicHost: "relay.example.com", HTTPListen: ":8000"})
	if got, want := app.startupHTTPBase(), "http://relay.example.com"; got != want {
		t.Fatalf("startupHTTPBase mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestStartupHTTPBaseUsesHTTPURLPublicHost(t *testing.T) {
	app := NewApp(Config{PublicHost: "https://relay.example.com/", HTTPListen: ":8000"})
	if got, want := app.startupHTTPBase(), "https://relay.example.com"; got != want {
		t.Fatalf("startupHTTPBase mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestStartupHTTPBaseFallsBackToListenPort(t *testing.T) {
	app := NewApp(Config{HTTPListen: ":8000"})
	if got, want := app.startupHTTPBase(), "http://<server-host>:8000"; got != want {
		t.Fatalf("startupHTTPBase mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestRuntimeInfoUsesPublicHostAndSSHListen(t *testing.T) {
	app := NewApp(Config{PublicHost: "https://bastion.example.com:18080/docs", SSHListen: ":22022"})
	req := httptest.NewRequest("GET", "http://internal.local:18080/api/me", nil)
	got := app.runtimeInfo(req)
	if got.SSHHost != "bastion.example.com" || got.SSHPort != 22022 {
		t.Fatalf("runtimeInfo mismatch: %+v", got)
	}
}

func TestRuntimeInfoUsesPublicSSHPortOverride(t *testing.T) {
	app := NewApp(Config{PublicHost: "https://bastion.example.com:18080", SSHListen: ":22", PublicSSHPort: 22022})
	req := httptest.NewRequest("GET", "http://internal.local:18080/api/me", nil)
	got := app.runtimeInfo(req)
	if got.SSHHost != "bastion.example.com" || got.SSHPort != 22022 {
		t.Fatalf("runtimeInfo mismatch: %+v", got)
	}
}
