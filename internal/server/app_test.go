package server

import "testing"

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
