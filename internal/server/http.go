package server

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd/internal/protocol"
	"github.com/qinyongliang/gosshd/internal/relay"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (a *App) routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/install.sh", a.installSH)
	mux.HandleFunc("/install.ps1", a.installPS1)
	mux.HandleFunc("/download/agent/", a.downloadAgent)
	mux.HandleFunc(protocol.WebSocketPath, a.agentWS)
}

func (a *App) installSH(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	base := publicBaseURL(r, a.cfg.publicHost())
	tokenArg := ""
	if a.cfg.AgentToken != "" {
		tokenArg = fmt.Sprintf(" --token %q", a.cfg.AgentToken)
	}
	fmt.Fprintf(w, `#!/usr/bin/env sh
set -eu
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  i386|i686|386) arch="386" ;;
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  armv6l|armv6*) arch="armv6" ;;
  armv7l|armv7*) arch="armv7" ;;
  riscv64) arch="riscv64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
tmp="${TMPDIR:-/tmp}/gosshd-agent"
url="%s/download/agent/${os}/${arch}"
echo "downloading $url"
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"
exec "$tmp" --server "%s"%s
`, base, base, tokenArg)
}

func (a *App) installPS1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	base := publicBaseURL(r, a.cfg.publicHost())
	tokenArg := ""
	if a.cfg.AgentToken != "" {
		tokenArg = fmt.Sprintf(" --token %q", a.cfg.AgentToken)
	}
	fmt.Fprintf(w, `$ErrorActionPreference = "Stop"
$machine = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($machine) {
  "x64" { $arch = "amd64" }
  "x86" { $arch = "386" }
  "arm64" { $arch = "arm64" }
  default { throw "unsupported arch: $machine" }
}
$tmp = Join-Path $env:TEMP "gosshd-agent.exe"
$url = "%s/download/agent/windows/$arch"
Write-Host "downloading $url"
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
& $tmp --server "%s"%s
`, base, base, tokenArg)
}

func (a *App) downloadAgent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/download/agent/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "expected /download/agent/{goos}/{goarch}", http.StatusBadRequest)
		return
	}
	goos, goarch := parts[0], parts[1]
	name := "gosshd-agent"
	if goos == "windows" {
		name += ".exe"
	}
	path := filepath.Join(a.cfg.AgentPath, goos, goarch, name)
	http.ServeFile(w, r, path)
}

func (a *App) agentWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("agent websocket upgrade failed: %v", err)
		return
	}
	conn := relay.NewWSConn(ws)
	reader := bufio.NewReader(conn)
	hello, err := protocol.ReadJSONLine[protocol.AgentHello](reader)
	if err != nil {
		_ = conn.Close()
		log.Printf("agent hello failed: %v", err)
		return
	}
	if !protocol.IsValidID(hello.ID) {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "invalid agent id"})
		_ = conn.Close()
		return
	}
	if a.cfg.AgentToken != "" && hello.Token != a.cfg.AgentToken {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "invalid agent token"})
		_ = conn.Close()
		return
	}
	if err := protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: true}); err != nil {
		_ = conn.Close()
		return
	}
	session, err := yamux.Server(conn, nil)
	if err != nil {
		_ = conn.Close()
		log.Printf("yamux server failed: %v", err)
		return
	}
	a.registry.Register(hello.ID, session)
	log.Printf("agent online: %s", hello.ID)
	go func() {
		<-session.CloseChan()
		a.registry.Unregister(hello.ID, session)
		log.Printf("agent offline: %s", hello.ID)
	}()
}

func publicBaseURL(r *http.Request, fallbackHost string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := fallbackHost
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost"
	}
	if runtime.GOOS == "windows" {
		host = strings.TrimSpace(host)
	}
	return scheme + "://" + host
}
