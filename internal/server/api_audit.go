package server

import (
	"net/http"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiAuditLog struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	TargetID       string `json:"target_id"`
	OrganizationID string `json:"organization_id,omitempty"`
	Command        string `json:"command"`
	RequestType    string `json:"request_type"`
	PolicyDecision string `json:"policy_decision"`
	PolicyReason   string `json:"policy_reason"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	StartedAt      string `json:"started_at"`
	EndedAt        string `json:"ended_at,omitempty"`
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
		ID:             log.ID,
		UserID:         log.UserID,
		TargetID:       log.TargetID,
		OrganizationID: log.OrganizationID,
		Command:        log.Command,
		RequestType:    log.RequestType,
		PolicyDecision: log.PolicyDecision,
		PolicyReason:   log.PolicyReason,
		ExitCode:       log.ExitCode,
		StartedAt:      log.StartedAt.Format(time.RFC3339),
	}
	if log.EndedAt != nil {
		out.EndedAt = log.EndedAt.Format(time.RFC3339)
	}
	return out
}
