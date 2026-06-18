package server

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentReleaseURLUsesRawAgentAsset(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3"})
	got := app.agentReleaseURL("linux", "amd64", "gosshd-agent")
	want := "https://github.com/qinyongliang/gosshd-bastion/releases/download/v1.2.3/gosshd-agent-v1.2.3-linux-amd64"
	if got != want {
		t.Fatalf("agent release url mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAgentReleaseURLUsesWindowsExtension(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3"})
	got := app.agentReleaseURL("windows", "arm64", "gosshd-agent.exe")
	want := "https://github.com/qinyongliang/gosshd-bastion/releases/download/v1.2.3/gosshd-agent-v1.2.3-windows-arm64.exe"
	if got != want {
		t.Fatalf("agent release url mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAgentCachePathDefaultsToVersionedTempDir(t *testing.T) {
	app := NewApp(Config{Version: "v1.2.3"})
	got := app.agentCachePath("linux", "amd64", "gosshd-agent")
	want := filepath.Join("gosshd-agent-cache", "v1.2.3", "linux", "amd64", "gosshd-agent")
	if filepath.ToSlash(got) == filepath.ToSlash(want) {
		t.Fatalf("expected absolute temp cache path, got relative %s", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), filepath.ToSlash(want)) {
		t.Fatalf("cache path mismatch:\n got: %s\nwant suffix: %s", got, want)
	}
}

func TestAgentCachePathUsesVersionUnderConfiguredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	app := NewApp(Config{Version: "v1.2.3", AgentCachePath: root})
	got := app.agentCachePath("linux", "amd64", "gosshd-agent")
	want := filepath.Join(root, "v1.2.3", "linux", "amd64", "gosshd-agent")
	if got != want {
		t.Fatalf("cache path mismatch:\n got: %s\nwant: %s", got, want)
	}
}
