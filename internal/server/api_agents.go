package server

import (
	"fmt"
	"net/http"
	"strconv"
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

const (
	defaultAgentTargetHost = "127.0.0.1"
	defaultAgentTargetPort = 22
)

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
		writeOwnerError(w, err)
		return
	}
	defaultHost, defaultPort := agentEnrollmentDefaults(req.DefaultHost, req.DefaultPort)
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
		DefaultHost: defaultHost,
		DefaultPort: defaultPort,
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

func agentEnrollmentDefaults(host string, port int) (string, int) {
	host = strings.TrimSpace(host)
	if host == "" {
		host = defaultAgentTargetHost
	}
	if port == 0 {
		port = defaultAgentTargetPort
	}
	return host, port
}

func agentEnrollmentResponse(id, token, base string) apiAgentEnrollmentResponse {
	return apiAgentEnrollmentResponse{
		ID:         id,
		Token:      token,
		InstallSH:  fmt.Sprintf("tmp=\"${TMPDIR:-/tmp}/gosshd-agent-install.sh\"; curl -fsSL %s/install/%s.sh -o \"$tmp\" && sh \"$tmp\"", base, token),
		InstallPS1: fmt.Sprintf("$s='%s/install/%s.ps1'; $tmp=Join-Path $env:TEMP 'gosshd-agent-install.ps1'; irm $s -OutFile $tmp; powershell -ExecutionPolicy Bypass -File $tmp", base, token),
		ServiceSH:  fmt.Sprintf("tmp=\"${TMPDIR:-/tmp}/gosshd-agent-install.sh\"; curl -fsSL %s/install/%s.sh -o \"$tmp\" && sudo sh \"$tmp\" install", base, token),
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
	sshPort := strconv.Itoa(publicSSHPort(a.cfg.PublicSSHPort, a.cfg.SSHListen))
	fmt.Fprintf(w, `#!/usr/bin/env sh
set -eu
mode="${1:-run}"
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
sha_url="${url}.sha256"
curl -fsSL "$url" -o "$tmp"
expected_sha="$(curl -fsSL "$sha_url" | tr -d '[:space:]')"
if command -v sha256sum >/dev/null 2>&1; then
  printf '%%s  %%s\n' "$expected_sha" "$tmp" | sha256sum -c -
elif command -v shasum >/dev/null 2>&1; then
  actual_sha="$(shasum -a 256 "$tmp" | awk '{print $1}')"
  [ "$actual_sha" = "$expected_sha" ] || { echo "sha256 mismatch" >&2; exit 1; }
else
  echo "sha256 checker not found" >&2
  exit 1
fi
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
ExecStart=/usr/local/bin/gosshd-agent --server "%s" --enrollment-token %q --ssh-port %q
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
exec "$tmp" --server "%s" --enrollment-token %q --ssh-port %q
`, base, base, token, sshPort, base, token, sshPort)
}

func (a *App) handleInstallPS1(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	base := publicBaseURL(r, a.cfg.publicHost())
	sshPort := strconv.Itoa(publicSSHPort(a.cfg.PublicSSHPort, a.cfg.SSHListen))
	fmt.Fprintf(w, `param(
  [switch]$Install
)
$ErrorActionPreference = "Stop"
$isInstall = $Install
$tmp = Join-Path $env:TEMP "gosshd-agent.exe"
$url = "%s/download/agent/windows/amd64"
$shaUrl = "$url.sha256"
$server = "%s"
$enrollmentToken = "%s"
$sshPort = "%s"
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
$expectedSha = (Invoke-WebRequest -UseBasicParsing -Uri $shaUrl).Content.Trim().ToLowerInvariant()
$actualSha = (Get-FileHash -Algorithm SHA256 -Path $tmp).Hash.ToLowerInvariant()
if ($actualSha -ne $expectedSha) {
  throw "sha256 mismatch: $actualSha != $expectedSha"
}
$targetDir = Join-Path $env:ProgramData "gosshd"
$target = Join-Path $targetDir "gosshd-agent.exe"
if ($isInstall) {
  New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
  Copy-Item -Force $tmp $target
  $binPath = '"' + $target + '" --server "' + $server + '" --enrollment-token "' + $enrollmentToken + '" --ssh-port "' + $sshPort + '"'
  $serviceName = "gosshd-agent"
  $existing = Get-CimInstance -ClassName Win32_Service -Filter "Name='$serviceName'" -ErrorAction SilentlyContinue
  if ($existing) {
    sc.exe stop $serviceName | Out-Null
    sc.exe delete $serviceName | Out-Null
    if ($LASTEXITCODE -ne 0) {
      throw "failed to delete existing $serviceName service"
    }
    Start-Sleep -Seconds 2
  }
  sc.exe create $serviceName binPath= $binPath start= auto DisplayName= "gosshd bastion agent" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to create $serviceName service"
  }
  sc.exe failure $serviceName reset= 60 actions= restart/5000/restart/5000/restart/5000 | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to configure $serviceName recovery actions"
  }
  sc.exe start $serviceName | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to start $serviceName service"
  }
  Get-CimInstance -ClassName Win32_Service -Filter "Name='$serviceName'" | Select-Object Name, State, StartMode, PathName
  exit 0
}
& $tmp --server $server --enrollment-token $enrollmentToken --ssh-port $sshPort
`, base, base, token, sshPort)
}
