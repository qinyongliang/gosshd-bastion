package server

import (
	"io/fs"
	"net/http/httptest"
	"testing"
	"time"
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

func TestAPIFileEntryUsesSymlinkDirectoryTarget(t *testing.T) {
	entry := apiFileEntry("/srv", fakeFileInfo{name: "current", mode: fs.ModeSymlink | 0o777}, fakeFileInfo{name: "release", mode: fs.ModeDir | 0o755})
	if entry.Type != "dir" {
		t.Fatalf("symlink to directory should render as dir: %+v", entry)
	}
	if entry.Path != "/srv/current" {
		t.Fatalf("entry path mismatch: %s", entry.Path)
	}
}

func TestAPIFileEntryKeepsNonDirectorySymlink(t *testing.T) {
	entry := apiFileEntry("/srv", fakeFileInfo{name: "latest.log", mode: fs.ModeSymlink | 0o777}, fakeFileInfo{name: "app.log", mode: 0o644})
	if entry.Type != "symlink" {
		t.Fatalf("symlink to non-directory should stay symlink: %+v", entry)
	}
}

type fakeFileInfo struct {
	name string
	mode fs.FileMode
	size int64
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Unix(0, 0).UTC() }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }
