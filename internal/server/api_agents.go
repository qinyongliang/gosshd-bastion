package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd/internal/store"
)

type apiAgentEnrollmentResponse struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	InstallSH  string `json:"install_sh"`
	InstallPS1 string `json:"install_ps1"`
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
	ownerType, ownerID := resolveOwner(req.OwnerType, req.OwnerID, user.ID)
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
	writeJSON(w, http.StatusCreated, apiAgentEnrollmentResponse{
		ID:         enrollment.ID,
		Token:      token,
		InstallSH:  fmt.Sprintf("curl -fsSL %s/install/%s.sh | sh", base, token),
		InstallPS1: fmt.Sprintf("irm %s/install/%s.ps1 | iex", base, token),
	})
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
tmp="${TMPDIR:-/tmp}/gosshd-agent"
url="%s/download/agent/$(uname -s | tr '[:upper:]' '[:lower:]')/amd64"
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"
exec "$tmp" --server "%s" --enrollment-token %q
`, base, base, token)
}

func (a *App) handleInstallPS1(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	base := publicBaseURL(r, a.cfg.publicHost())
	fmt.Fprintf(w, `$ErrorActionPreference = "Stop"
$tmp = Join-Path $env:TEMP "gosshd-agent.exe"
$url = "%s/download/agent/windows/amd64"
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
& $tmp --server "%s" --enrollment-token %q
`, base, base, token)
}
