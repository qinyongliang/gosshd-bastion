package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiAuditLog struct {
	ID                   string `json:"id"`
	UserID               string `json:"user_id"`
	UserEmail            string `json:"user_email,omitempty"`
	UserDisplayName      string `json:"user_display_name,omitempty"`
	TargetID             string `json:"target_id"`
	TargetName           string `json:"target_name,omitempty"`
	TargetAlias          string `json:"target_alias,omitempty"`
	TargetEndpoint       string `json:"target_endpoint,omitempty"`
	OrganizationID       string `json:"organization_id,omitempty"`
	PublicKeyFingerprint string `json:"public_key_fingerprint,omitempty"`
	PublicKeyName        string `json:"public_key_name,omitempty"`
	Command              string `json:"command"`
	RequestType          string `json:"request_type"`
	PolicyDecision       string `json:"policy_decision"`
	PolicyReason         string `json:"policy_reason"`
	ExitCode             *int   `json:"exit_code,omitempty"`
	StartedAt            string `json:"started_at"`
	EndedAt              string `json:"ended_at,omitempty"`
}

type apiAuditLogsResponse struct {
	Logs []apiAuditLog `json:"logs"`
}

func (a *App) handleListAuditLogs(w http.ResponseWriter, r *http.Request, user store.User) {
	logs, err := a.store.Repository().ListCommandAuditLogs(r.Context(), store.AuditLogFilter{UserID: user.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiAuditLogsResponse{}
	for _, log := range logs {
		out.Logs = append(out.Logs, apiAuditLogFromStore(log))
	}
	writeJSON(w, http.StatusOK, out)
}

func apiAuditLogFromStore(log store.CommandAuditLog) apiAuditLog {
	out := apiAuditLog{
		ID:                   log.ID,
		UserID:               log.UserID,
		UserEmail:            log.UserEmail,
		UserDisplayName:      log.UserDisplayName,
		TargetID:             log.TargetID,
		TargetName:           log.TargetName,
		TargetAlias:          log.TargetAlias,
		TargetEndpoint:       auditTargetEndpoint(log),
		OrganizationID:       log.OrganizationID,
		PublicKeyFingerprint: log.PublicKeyFingerprint,
		PublicKeyName:        log.PublicKeyName,
		Command:              log.Command,
		RequestType:          log.RequestType,
		PolicyDecision:       log.PolicyDecision,
		PolicyReason:         log.PolicyReason,
		ExitCode:             log.ExitCode,
		StartedAt:            log.StartedAt.Format(time.RFC3339),
	}
	if log.EndedAt != nil {
		out.EndedAt = log.EndedAt.Format(time.RFC3339)
	}
	return out
}

func auditTargetEndpoint(log store.CommandAuditLog) string {
	if log.TargetHost == "" {
		return ""
	}
	endpoint := log.TargetHost
	if log.TargetUsername != "" {
		endpoint = log.TargetUsername + "@" + endpoint
	}
	if log.TargetPort <= 0 {
		return endpoint
	}
	return endpoint + ":" + strconv.Itoa(log.TargetPort)
}
