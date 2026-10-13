package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type DesktopAccess struct {
	UserID       string     `json:"user_id"`
	TargetID     string     `json:"target_id"`
	AllowRead    bool       `json:"allow_read"`
	AllowControl bool       `json:"allow_control"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	GrantedBy    string     `json:"granted_by"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (r *Repository) GetDesktopAccess(ctx context.Context, user, target string) (DesktopAccess, error) {
	a := DesktopAccess{UserID: user, TargetID: target}
	var read, control int
	var expiry sql.NullString
	var updated string
	err := r.db.QueryRowContext(ctx, `SELECT allow_read,allow_control,expires_at,granted_by,updated_at FROM desktop_access WHERE user_id=? AND target_id=?`, user, target).Scan(&read, &control, &expiry, &a.GrantedBy, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	a.AllowRead = read == 1
	a.AllowControl = control == 1
	a.UpdatedAt = parseTime(updated)
	if expiry.Valid {
		v := parseTime(expiry.String)
		a.ExpiresAt = &v
	}
	return a, nil
}
func (r *Repository) SetDesktopAccess(ctx context.Context, a DesktopAccess) error {
	if a.AllowControl && !a.AllowRead {
		return errors.New("desktop control requires desktop read")
	}
	if a.UserID == "" || a.TargetID == "" || a.GrantedBy == "" {
		return errors.New("desktop grant identity is required")
	}
	if a.ExpiresAt != nil && (a.ExpiresAt.Before(time.Now()) || a.ExpiresAt.After(time.Now().Add(365*24*time.Hour))) {
		return errors.New("desktop grant expiry must be in the next 365 days")
	}
	var expiry any
	if a.ExpiresAt != nil {
		expiry = a.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO desktop_access(user_id,target_id,allow_read,allow_control,expires_at,granted_by,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(user_id,target_id) DO UPDATE SET allow_read=excluded.allow_read,allow_control=excluded.allow_control,expires_at=excluded.expires_at,granted_by=excluded.granted_by,updated_at=excluded.updated_at`, a.UserID, a.TargetID, boolInt(a.AllowRead), boolInt(a.AllowControl), expiry, a.GrantedBy, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

type DesktopAudit struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TargetID  string    `json:"target_id"`
	SessionID string    `json:"session_id"`
	Method    string    `json:"method"`
	Outcome   string    `json:"outcome"`
	StartedAt time.Time `json:"started_at"`
}

func (r *AuditRepository) SaveDesktopAudit(ctx context.Context, e DesktopAudit) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO desktop_audit(id,user_id,target_id,session_id,method,outcome,started_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET outcome=excluded.outcome,updated_at=excluded.updated_at`, e.ID, e.UserID, e.TargetID, e.SessionID, e.Method, e.Outcome, e.StartedAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (r *AuditRepository) ListDesktopAudit(ctx context.Context, user, target string, limit int) ([]DesktopAudit, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,target_id,session_id,method,outcome,started_at FROM desktop_audit WHERE user_id=? AND (?='' OR target_id=?) ORDER BY started_at DESC LIMIT ?`, user, target, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DesktopAudit{}
	for rows.Next() {
		var e DesktopAudit
		var at string
		if err = rows.Scan(&e.ID, &e.UserID, &e.TargetID, &e.SessionID, &e.Method, &e.Outcome, &at); err != nil {
			return nil, err
		}
		e.StartedAt = parseTime(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
