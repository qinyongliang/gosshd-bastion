package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type AuditStore struct {
	db   *sql.DB
	repo *AuditRepository
}

type AuditRepository struct {
	db *sql.DB
}

var auditMigrations = []string{
	`CREATE TABLE IF NOT EXISTS command_audit_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		user_email TEXT NOT NULL DEFAULT '',
		user_display_name TEXT NOT NULL DEFAULT '',
		target_id TEXT NOT NULL,
		target_name TEXT NOT NULL DEFAULT '',
		target_alias TEXT NOT NULL DEFAULT '',
		target_host TEXT NOT NULL DEFAULT '',
		target_port INTEGER NOT NULL DEFAULT 0,
		target_username TEXT NOT NULL DEFAULT '',
		organization_id TEXT NOT NULL DEFAULT '',
		session_id TEXT NOT NULL,
		command TEXT NOT NULL,
		request_type TEXT NOT NULL,
		policy_decision TEXT NOT NULL,
		policy_reason TEXT NOT NULL,
		public_key_fingerprint TEXT NOT NULL DEFAULT '',
		public_key_name TEXT NOT NULL DEFAULT '',
		exit_code INTEGER,
		started_at TEXT NOT NULL,
		ended_at TEXT,
		remote_address TEXT NOT NULL DEFAULT '',
		recording_path TEXT NOT NULL DEFAULT '',
		recording_size INTEGER NOT NULL DEFAULT 0,
		recording_sha256 TEXT NOT NULL DEFAULT '',
		recording_duration_ms INTEGER NOT NULL DEFAULT 0,
		recording_width INTEGER NOT NULL DEFAULT 0,
		recording_height INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS idx_command_audit_user_started ON command_audit_logs (user_id, started_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_command_audit_target_started ON command_audit_logs (target_id, started_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_command_audit_started ON command_audit_logs (started_at DESC)`,
}

func OpenAudit(ctx context.Context, path string) (*AuditStore, error) {
	if strings.TrimSpace(path) == "" {
		path = filepath.Join(".", "gosshd-audit.db")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open audit sqlite: %w", err)
	}
	st := &AuditStore{db: db}
	st.repo = &AuditRepository{db: db}
	if err := configureSQLite(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := st.ApplyMigrations(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *AuditStore) ApplyMigrations(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range auditMigrations {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return fmt.Errorf("apply audit migration: %w", err)
		}
	}
	return tx.Commit()
}

func (s *AuditStore) DB() *sql.DB {
	return s.db
}

func (s *AuditStore) Repository() *AuditRepository {
	return s.repo
}

func (s *AuditStore) Close() error {
	return s.db.Close()
}

func (r *AuditRepository) CreateCommandAuditLog(ctx context.Context, params CreateCommandAuditLogParams) (CommandAuditLog, error) {
	started := params.StartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	log := CommandAuditLog{
		ID:                   uuid.NewString(),
		UserID:               strings.TrimSpace(params.UserID),
		UserEmail:            strings.TrimSpace(params.UserEmail),
		UserDisplayName:      strings.TrimSpace(params.UserDisplayName),
		TargetID:             strings.TrimSpace(params.TargetID),
		TargetName:           strings.TrimSpace(params.TargetName),
		TargetAlias:          strings.TrimSpace(params.TargetAlias),
		TargetHost:           strings.TrimSpace(params.TargetHost),
		TargetPort:           params.TargetPort,
		TargetUsername:       strings.TrimSpace(params.TargetUsername),
		OrganizationID:       strings.TrimSpace(params.OrganizationID),
		PublicKeyFingerprint: strings.TrimSpace(params.PublicKeyFingerprint),
		PublicKeyName:        strings.TrimSpace(params.PublicKeyName),
		SessionID:            strings.TrimSpace(params.SessionID),
		Command:              strings.TrimSpace(params.Command),
		RequestType:          strings.TrimSpace(params.RequestType),
		PolicyDecision:       strings.TrimSpace(params.PolicyDecision),
		PolicyReason:         strings.TrimSpace(params.PolicyReason),
		ExitCode:             params.ExitCode,
		StartedAt:            started.UTC(),
		EndedAt:              params.EndedAt,
		RemoteAddress:        strings.TrimSpace(params.RemoteAddress),
		RecordingPath:        strings.TrimSpace(params.RecordingPath),
		RecordingSize:        params.RecordingSize,
		RecordingSHA256:      strings.TrimSpace(params.RecordingSHA256),
		RecordingDurationMS:  params.RecordingDurationMS,
		RecordingWidth:       params.RecordingWidth,
		RecordingHeight:      params.RecordingHeight,
	}
	if log.SessionID == "" {
		log.SessionID = uuid.NewString()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO command_audit_logs (
			id, user_id, user_email, user_display_name, target_id, target_name, target_alias,
			target_host, target_port, target_username, organization_id, session_id, command,
			request_type, policy_decision, policy_reason, public_key_fingerprint, public_key_name,
			exit_code, started_at, ended_at, remote_address, recording_path, recording_size,
			recording_sha256, recording_duration_ms, recording_width, recording_height
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.UserID, log.UserEmail, log.UserDisplayName, log.TargetID, log.TargetName,
		log.TargetAlias, log.TargetHost, log.TargetPort, log.TargetUsername, log.OrganizationID,
		log.SessionID, log.Command, log.RequestType, log.PolicyDecision, log.PolicyReason,
		log.PublicKeyFingerprint, log.PublicKeyName, nullableInt(log.ExitCode), formatTime(log.StartedAt),
		nullableTime(log.EndedAt), log.RemoteAddress, log.RecordingPath, log.RecordingSize,
		log.RecordingSHA256, log.RecordingDurationMS, log.RecordingWidth, log.RecordingHeight)
	if err != nil {
		return CommandAuditLog{}, err
	}
	return log, nil
}

func (r *AuditRepository) CompleteCommandAuditLog(ctx context.Context, params CompleteCommandAuditLogParams) error {
	if strings.TrimSpace(params.ID) == "" {
		return ErrNotFound
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE command_audit_logs
		SET exit_code = ?, ended_at = ?, recording_path = ?, recording_size = ?,
			recording_sha256 = ?, recording_duration_ms = ?, recording_width = ?, recording_height = ?
		WHERE id = ?
	`, nullableInt(params.ExitCode), formatTime(params.EndedAt.UTC()), strings.TrimSpace(params.RecordingPath),
		params.RecordingSize, strings.TrimSpace(params.RecordingSHA256), params.RecordingDurationMS,
		params.RecordingWidth, params.RecordingHeight, strings.TrimSpace(params.ID))
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *AuditRepository) ListCommandAuditLogs(ctx context.Context, filter AuditLogFilter) (AuditLogPage, error) {
	where, args := auditWhere(filter)
	totalQuery := `SELECT COUNT(*) FROM command_audit_logs` + where
	var total int
	if err := r.db.QueryRowContext(ctx, totalQuery, args...).Scan(&total); err != nil {
		return AuditLogPage{}, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query := `
		SELECT id, user_id, user_email, user_display_name, target_id, target_name, target_alias,
			target_host, target_port, target_username, organization_id, session_id, command,
			request_type, policy_decision, policy_reason, public_key_fingerprint, public_key_name,
			exit_code, started_at, ended_at, remote_address, recording_path, recording_size,
			recording_sha256, recording_duration_ms, recording_width, recording_height
		FROM command_audit_logs` + where + `
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return AuditLogPage{}, err
	}
	defer rows.Close()
	logs, err := scanAuditLogRows(rows)
	if err != nil {
		return AuditLogPage{}, err
	}
	return AuditLogPage{Logs: logs, Total: total}, rows.Err()
}

func (r *AuditRepository) GetCommandAuditLog(ctx context.Context, id string) (CommandAuditLog, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, user_email, user_display_name, target_id, target_name, target_alias,
			target_host, target_port, target_username, organization_id, session_id, command,
			request_type, policy_decision, policy_reason, public_key_fingerprint, public_key_name,
			exit_code, started_at, ended_at, remote_address, recording_path, recording_size,
			recording_sha256, recording_duration_ms, recording_width, recording_height
		FROM command_audit_logs
		WHERE id = ?
	`, id)
	return scanAuditLog(row)
}

func auditWhere(filter AuditLogFilter) (string, []any) {
	var clauses []string
	var args []any
	if filter.UserID != "" {
		clauses = append(clauses, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.OrganizationID != "" {
		clauses = append(clauses, "organization_id = ?")
		args = append(args, filter.OrganizationID)
	}
	if filter.TargetID != "" {
		clauses = append(clauses, "target_id = ?")
		args = append(args, filter.TargetID)
	}
	if filter.PolicyDecision != "" {
		clauses = append(clauses, "policy_decision = ?")
		args = append(args, filter.PolicyDecision)
	}
	if filter.RequestType != "" {
		clauses = append(clauses, "request_type = ?")
		args = append(args, filter.RequestType)
	}
	if !filter.StartedFrom.IsZero() {
		clauses = append(clauses, "started_at >= ?")
		args = append(args, formatTime(filter.StartedFrom))
	}
	if !filter.StartedTo.IsZero() {
		clauses = append(clauses, "started_at <= ?")
		args = append(args, formatTime(filter.StartedTo))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		clauses = append(clauses, `(LOWER(command) LIKE ? OR LOWER(policy_reason) LIKE ? OR LOWER(user_email) LIKE ? OR LOWER(user_display_name) LIKE ? OR LOWER(target_name) LIKE ? OR LOWER(target_alias) LIKE ? OR LOWER(public_key_name) LIKE ? OR LOWER(public_key_fingerprint) LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func scanAuditLogRows(rows *sql.Rows) ([]CommandAuditLog, error) {
	var logs []CommandAuditLog
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

func scanAuditLog(row targetScanner) (CommandAuditLog, error) {
	var log CommandAuditLog
	var exit sql.NullInt64
	var ended sql.NullString
	var started string
	err := row.Scan(&log.ID, &log.UserID, &log.UserEmail, &log.UserDisplayName,
		&log.TargetID, &log.TargetName, &log.TargetAlias, &log.TargetHost, &log.TargetPort,
		&log.TargetUsername, &log.OrganizationID, &log.SessionID, &log.Command, &log.RequestType,
		&log.PolicyDecision, &log.PolicyReason, &log.PublicKeyFingerprint, &log.PublicKeyName,
		&exit, &started, &ended, &log.RemoteAddress, &log.RecordingPath, &log.RecordingSize,
		&log.RecordingSHA256, &log.RecordingDurationMS, &log.RecordingWidth, &log.RecordingHeight)
	if err != nil {
		return CommandAuditLog{}, wrapScanErr(err)
	}
	if exit.Valid {
		v := int(exit.Int64)
		log.ExitCode = &v
	}
	log.StartedAt = parseTime(started)
	if ended.Valid {
		v := parseTime(ended.String)
		log.EndedAt = &v
	}
	return log, nil
}
