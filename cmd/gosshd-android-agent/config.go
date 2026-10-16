package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
)

// Only app-private data is configurable. Executable, shell and identity paths
// are fixed by the APK launcher, never supplied by an Intent or remote RPC.
func loadAPKConfig(name string) (agent.Config, error) {
	f, err := os.Open(name)
	if err != nil {
		return agent.Config{}, errors.New("cannot open private APK configuration")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return agent.Config{}, errors.New("invalid private APK configuration file")
	}
	var stored struct {
		Server          string `json:"server"`
		EnrollmentToken string `json:"enrollment_token"`
		DesktopEnabled  bool   `json:"desktop_enabled"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, 16385))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return agent.Config{}, errors.New("invalid APK configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return agent.Config{}, errors.New("unexpected APK configuration data")
	}
	if len(stored.Server) > 2048 {
		return agent.Config{}, errors.New("server URL too long")
	}
	server, err := url.Parse(strings.TrimSpace(stored.Server))
	if err != nil || server.Hostname() == "" || server.User != nil || server.RawQuery != "" || server.Fragment != "" || server.Port() == "0" || (server.Path != "" && server.Path != "/") {
		return agent.Config{}, errors.New("invalid server URL")
	}
	if server.Port() != "" {
		port, err := strconv.Atoi(server.Port())
		if err != nil || port < 1 || port > 65535 {
			return agent.Config{}, errors.New("invalid server port")
		}
	}
	loopback := strings.EqualFold(server.Hostname(), "localhost")
	if ip := net.ParseIP(server.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if server.Scheme != "https" && !(server.Scheme == "http" && loopback) {
		return agent.Config{}, errors.New("HTTPS is required except for loopback development")
	}
	token := strings.TrimSpace(stored.EnrollmentToken)
	if token == "" || len(token) > 4096 || strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return agent.Config{}, errors.New("enrollment token is required")
	}
	dir := filepath.Dir(name)
	native := desktop.Config{Enabled: stored.DesktopEnabled, AndroidBackend: "accessibility"}
	if stored.DesktopEnabled {
		native.AndroidBridgeTokenFile = filepath.Join(dir, "desktop-pairing.token")
	}
	return agent.Config{Server: strings.TrimRight(server.String(), "/"), EnrollmentToken: token, IDFile: filepath.Join(dir, "agent.json"), Root: filepath.Join(dir, "home"), Shell: "/system/bin/sh", Desktop: native, DesktopDisabled: !stored.DesktopEnabled, DisableAutoUpdate: true}, nil
}
