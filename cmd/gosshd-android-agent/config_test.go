package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAPKConfig(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "agent-config.json")
	if err := os.WriteFile(name, []byte(`{"server":"https://bastion.example.com/","enrollment_token":"example-registration-token","desktop_enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAPKConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://bastion.example.com" || cfg.EnrollmentToken != "example-registration-token" || !cfg.DisableAutoUpdate || !cfg.Desktop.Enabled || cfg.Desktop.AndroidBackend != "accessibility" {
		t.Fatalf("unexpected non-secret config fields: server=%s desktop=%+v", cfg.Server, cfg.Desktop)
	}
	if cfg.IDFile != filepath.Join(dir, "agent.json") || cfg.Root != filepath.Join(dir, "home") || cfg.Shell != "/system/bin/sh" || cfg.Desktop.AndroidBridgeTokenFile != filepath.Join(dir, "desktop-pairing.token") {
		t.Fatal("APK config did not use fixed private paths")
	}
}

func TestAPKConfigRejectsInvalidInput(t *testing.T) {
	cases := []string{
		`{"server":"https://bastion.example.com:65536","enrollment_token":"example-token"}`,
		`{"server":"https://bastion.example.com:0","enrollment_token":"example-token"}`,
		`{"server":"https://bastion.example.com"}`,
		`{"server":"http://bastion.example.com","enrollment_token":"example-token"}`,
		`{"server":"https://user:secret@bastion.example.com","enrollment_token":"example-token"}`,
		`{"server":"https://bastion.example.com?secret=example","enrollment_token":"example-token"}`,
		`{"server":"https://bastion.example.com","enrollment_token":"example-token","shell":"/system/bin/sh"}`,
		`{"server":"https://bastion.example.com","enrollment_token":"example-token"} {}`,
		strings.Repeat("x", 16385),
	}
	for i, body := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "agent-config.json")
			if err := os.WriteFile(name, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadAPKConfig(name); err == nil {
				t.Fatal("accepted invalid APK config")
			}
		})
	}
}

func TestAPKConfigAllowsLoopbackDevelopment(t *testing.T) {
	name := filepath.Join(t.TempDir(), "agent-config.json")
	if err := os.WriteFile(name, []byte(`{"server":"http://127.0.0.1:18080","enrollment_token":"example-token"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadAPKConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Desktop.Enabled || !cfg.DesktopDisabled {
		t.Fatal("desktop must remain opt-in")
	}
}
