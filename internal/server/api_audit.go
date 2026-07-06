package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	HasRecording         bool   `json:"has_recording"`
	RecordingDurationMS  int64  `json:"recording_duration_ms,omitempty"`
	RecordingWidth       int    `json:"recording_width,omitempty"`
	RecordingHeight      int    `json:"recording_height,omitempty"`
}

type apiAuditLogsResponse struct {
	Logs     []apiAuditLog `json:"logs"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

func (a *App) handleListAuditLogs(w http.ResponseWriter, r *http.Request, user store.User) {
	page, pageSize := auditPageParams(r.URL.Query())
	filter := store.AuditLogFilter{
		Query:          r.URL.Query().Get("query"),
		OrganizationID: strings.TrimSpace(r.URL.Query().Get("organization_id")),
		StartedFrom:    parseAuditTime(r.URL.Query().Get("started_from")),
		StartedTo:      parseAuditTime(r.URL.Query().Get("started_to")),
		Limit:          pageSize,
		Offset:         (page - 1) * pageSize,
	}
	if err := a.scopeAuditFilter(r, user, &filter); err != nil {
		writeOwnerError(w, err)
		return
	}
	if targetID := strings.TrimSpace(r.URL.Query().Get("target_id")); targetID != "" {
		filter.TargetID = targetID
	}
	if decision := auditDecisionParam(r.URL.Query().Get("decision")); decision != "" {
		filter.PolicyDecision = decision
	}
	if requestType := auditRequestTypeParam(r.URL.Query().Get("request_type")); requestType != "" {
		filter.RequestType = requestType
	}
	pageResult, err := a.audit.Repository().ListCommandAuditLogs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiAuditLogsResponse{Total: pageResult.Total, Page: page, PageSize: pageSize}
	for _, log := range pageResult.Logs {
		out.Logs = append(out.Logs, apiAuditLogFromStore(log))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleAuditRecording(w http.ResponseWriter, r *http.Request, user store.User) {
	log, err := a.audit.Repository().GetCommandAuditLog(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !a.canReadAuditLog(r, user, log) {
		writeError(w, http.StatusForbidden, "audit recording access denied")
		return
	}
	path := a.absoluteRecordingPath(log.RecordingPath)
	if path == "" {
		writeError(w, http.StatusNotFound, "recording not found")
		return
	}
	lines, err := loadTerminalRecording(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "recording not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Log   apiAuditLog       `json:"log"`
		Lines []json.RawMessage `json:"lines"`
	}{Log: apiAuditLogFromStore(log), Lines: lines})
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
		HasRecording:         log.RecordingPath != "",
		RecordingDurationMS:  log.RecordingDurationMS,
		RecordingWidth:       log.RecordingWidth,
		RecordingHeight:      log.RecordingHeight,
	}
	if log.EndedAt != nil {
		out.EndedAt = log.EndedAt.Format(time.RFC3339)
	}
	return out
}

func (a *App) scopeAuditFilter(r *http.Request, user store.User, filter *store.AuditLogFilter) error {
	if user.IsSystemAdmin {
		return nil
	}
	if filter.OrganizationID == "" {
		filter.UserID = user.ID
		return nil
	}
	member, err := a.store.Repository().GetOrganizationMember(r.Context(), filter.OrganizationID, user.ID)
	if err != nil {
		return errOwnerAccess
	}
	if member.Role == store.RoleOwner || member.Role == store.RoleAdmin {
		return nil
	}
	filter.UserID = user.ID
	return nil
}

func (a *App) canReadAuditLog(r *http.Request, user store.User, log store.CommandAuditLog) bool {
	if user.IsSystemAdmin || log.UserID == user.ID {
		return true
	}
	if strings.TrimSpace(log.OrganizationID) == "" {
		return false
	}
	member, err := a.store.Repository().GetOrganizationMember(r.Context(), log.OrganizationID, user.ID)
	if err != nil {
		return false
	}
	return member.Role == store.RoleOwner || member.Role == store.RoleAdmin
}

func auditPageParams(values url.Values) (int, int) {
	page, _ := strconv.Atoi(values.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(values.Get("page_size"))
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func parseAuditTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func auditDecisionParam(raw string) string {
	switch strings.TrimSpace(raw) {
	case store.DecisionAllow, store.DecisionDeny:
		return strings.TrimSpace(raw)
	default:
		return ""
	}
}

func auditRequestTypeParam(raw string) string {
	switch strings.TrimSpace(raw) {
	case store.RequestExec, store.RequestShell, store.RequestWebTerminal, store.RequestSFTP, store.RequestForward:
		return strings.TrimSpace(raw)
	default:
		return ""
	}
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
