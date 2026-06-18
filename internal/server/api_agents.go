package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiAgentEnrollmentResponse struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	InstallSH  string `json:"install_sh"`
	InstallPS1 string `json:"install_ps1"`
	ServiceSH  string `json:"service_sh"`
	ServicePS1 string `json:"service_ps1"`
}

func (a *App) handleCreateAgentEnrollment(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType   string `json:"owner_type"`
		OwnerID     string `json:"owner_id"`
		Label       string `json:"label"`
		DefaultHost string `json:"default_host"`
		DefaultPort int    `json:"default_port"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ownerType, ownerID, err := a.resolveOwner(r.Context(), req.OwnerType, req.OwnerID, user.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, hash, err := randomCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enrollment, err := a.store.Repository().CreateAgentEnrollment(r.Context(), store.CreateAgentEnrollmentParams{
		OwnerType:   ownerType,
		OwnerID:     ownerID,
		TokenHash:   hash,
		Label:       req.Label,
		DefaultHost: req.DefaultHost,
		DefaultPort: req.DefaultPort,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().UTC().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	base := publicBaseURL(r, a.cfg.publicHost())
	writeJSON(w, http.StatusCreated, agentEnrollmentResponse(enrollment.ID, token, base))
}

func agentEnrollmentResponse(id, token, base string) apiAgentEnrollmentResponse {
	return apiAgentEnrollmentResponse{
		ID:         id,
		Token:      token,
		InstallSH:  fmt.Sprintf("curl -fsSL %s/install/%s.sh | sh", base, token),
		InstallPS1: fmt.Sprintf("irm %s/install/%s.ps1 | iex", base, token),
		ServiceSH:  fmt.Sprintf("curl -fsSL %s/install/%s.sh | sudo sh -s -- install", base, token),
		ServicePS1: fmt.Sprintf("$s='%s/install/%s.ps1'; irm $s -OutFile $env:TEMP\\gosshd-agent-install.ps1; powershell -ExecutionPolicy Bypass -File $env:TEMP\\gosshd-agent-install.ps1 -Install", base, token),
	}
}

func (a *App) handleInstall(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	switch {
	case strings.HasSuffix(file, ".sh"):
		a.handleInstallSH(w, r, strings.TrimSuffix(file, ".sh"))
	case strings.HasSuffix(file, ".ps1"):
		a.handleInstallPS1(w, r, strings.TrimSuffix(file, ".ps1"))
	default:
		http.NotFound(w, r)
	}
}

func (a *App) handleInstallSH(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	base := publicBaseURL(r, a.cfg.publicHost())
	fmt.Fprintf(w, `#!/usr/bin/env sh
set -eu
mode="${1:-run}"
if [ "$mode" = "installl" ]; then
  mode="install"
fi
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
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"
if [ "$mode" = "install" ]; then
  if [ "$(id -u)" -ne 0 ]; then
    echo "service install requires root; run with sudo" >&2
    exit 1
  fi
  install -m 0755 "$tmp" /usr/local/bin/gosshd-agent
  cat >/etc/systemd/system/gosshd-agent.service <<'SERVICE'
[Unit]
Description=gosshd bastion agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/gosshd-agent --server "%s" --enrollment-token %q
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SERVICE
  systemctl daemon-reload
  systemctl enable --now gosshd-agent
  systemctl status gosshd-agent --no-pager
  exit 0
fi
exec "$tmp" --server "%s" --enrollment-token %q
`, base, base, token, base, token)
}

func (a *App) handleInstallPS1(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	base := publicBaseURL(r, a.cfg.publicHost())
	fmt.Fprintf(w, `param(
  [switch]$Install,
  [Alias("installl")]
  [switch]$Installl
)
$ErrorActionPreference = "Stop"
$isInstall = $Install -or $Installl
$tmp = Join-Path $env:TEMP "gosshd-agent.exe"
$url = "%s/download/agent/windows/amd64"
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
$targetDir = Join-Path $env:ProgramData "gosshd"
$target = Join-Path $targetDir "gosshd-agent.exe"
if ($isInstall) {
  New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
  Copy-Item -Force $tmp $target
  $quote = [char]34
  $args = "--server $quote%s$quote --enrollment-token $quote%s$quote"
  $binPath = "$quote$target$quote $args"
  $existing = Get-Service -Name "gosshd-agent" -ErrorAction SilentlyContinue
  if ($existing) {
    sc.exe stop gosshd-agent | Out-Null
    sc.exe delete gosshd-agent | Out-Null
    Start-Sleep -Seconds 2
  }
  sc.exe create gosshd-agent binPath= $binPath start= auto DisplayName= "gosshd bastion agent" | Out-Null
  sc.exe failure gosshd-agent reset= 60 actions= restart/5000/restart/5000/restart/5000 | Out-Null
  sc.exe start gosshd-agent | Out-Null
  Get-Service gosshd-agent
  exit 0
}
& $tmp --server "%s" --enrollment-token %q
`, base, base, token, base, token)
}
