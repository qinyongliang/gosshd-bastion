package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Empty target IDs select the bastion server itself.
type TunnelConfig struct {
	Name            string `json:"name"`
	EntryTargetID   string `json:"entry_target_id"`
	ListenHost      string `json:"listen_host"`
	ListenPort      int    `json:"listen_port"`
	ExitTargetID    string `json:"exit_target_id"`
	DestinationHost string `json:"destination_host"`
	DestinationPort int    `json:"destination_port"`
	DurationSeconds int64  `json:"duration_seconds"`
}
type Tunnel struct {
	TunnelConfig
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	CreatedBy      string     `json:"created_by"`
	EnabledBy      string     `json:"enabled_by,omitempty"`
	Enabled        bool       `json:"enabled"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const tunnelColumns = `id, organization_id, created_by, COALESCE(enabled_by,''), config, enabled, expires_at, revision, created_at, updated_at`

func scanTunnel(row interface{ Scan(...any) error }) (Tunnel, error) {
	var t Tunnel
	var config, created, updated string
	var expires sql.NullInt64
	err := row.Scan(&t.ID, &t.OrganizationID, &t.CreatedBy, &t.EnabledBy, &config, &t.Enabled, &expires, &t.Revision, &created, &updated)
	if err == sql.ErrNoRows {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(config), &t.TunnelConfig); err != nil {
		return t, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if expires.Valid {
		v := time.Unix(0, expires.Int64).UTC()
		t.ExpiresAt = &v
	}
	return t, nil
}
func (r *Repository) ListTunnels(ctx context.Context, orgID string) ([]Tunnel, error) {
	q := `SELECT ` + tunnelColumns + ` FROM tunnels`
	var args []any
	if orgID != "" {
		q += ` WHERE organization_id=?`
		args = append(args, orgID)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Tunnel{}
	for rows.Next() {
		t, err := scanTunnel(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (r *Repository) GetTunnel(ctx context.Context, id string) (Tunnel, error) {
	return scanTunnel(r.db.QueryRowContext(ctx, `SELECT `+tunnelColumns+` FROM tunnels WHERE id=?`, id))
}
func (r *Repository) SaveTunnel(ctx context.Context, t Tunnel) (Tunnel, error) {
	config, err := json.Marshal(t.TunnelConfig)
	if err != nil {
		return t, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if t.ID == "" {
		t.ID = uuid.NewString()
		_, err = r.db.ExecContext(ctx, `INSERT INTO tunnels(id,organization_id,created_by,config,created_at,updated_at) VALUES(?,?,?,?,?,?)`, t.ID, t.OrganizationID, t.CreatedBy, string(config), now, now)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE tunnels SET config=?,revision=revision+1,updated_at=? WHERE id=?`, string(config), now, t.ID)
	}
	if err != nil {
		return t, err
	}
	return r.GetTunnel(ctx, t.ID)
}
func (t Tunnel) OperatorID() string {
	if t.EnabledBy != "" {
		return t.EnabledBy
	}
	return t.CreatedBy
}
func (r *Repository) SetTunnelEnabled(ctx context.Context, id string, enabled bool, seconds int64) (Tunnel, error) {
	return r.setTunnelEnabled(ctx, id, enabled, seconds, "")
}
func (r *Repository) EnableTunnelForUser(ctx context.Context, id string, seconds int64, userID string) (Tunnel, error) {
	return r.setTunnelEnabled(ctx, id, true, seconds, userID)
}
func (r *Repository) setTunnelEnabled(ctx context.Context, id string, enabled bool, seconds int64, userID string) (Tunnel, error) {
	var expires any
	if enabled && seconds > 0 {
		expires = time.Now().Add(time.Duration(seconds) * time.Second).UnixNano()
	}
	_, err := r.db.ExecContext(ctx, `UPDATE tunnels SET enabled=?,expires_at=?,enabled_by=COALESCE(NULLIF(?,''),enabled_by),revision=revision+1,updated_at=? WHERE id=?`, enabled, expires, userID, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return Tunnel{}, err
	}
	return r.GetTunnel(ctx, id)
}
func (r *Repository) ExpireTunnels(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tunnels SET enabled=0,revision=revision+1 WHERE enabled=1 AND expires_at IS NOT NULL AND expires_at<=?`, now.UnixNano())
	return err
}
func (r *Repository) DeleteTunnel(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tunnels WHERE id=?`, id)
	return err
}
