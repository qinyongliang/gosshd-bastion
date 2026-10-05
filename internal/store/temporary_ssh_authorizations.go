package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TemporarySSHAuthorization struct {
	ID        string
	TargetID  string
	Name      string
	Token     string
	CreatedBy string
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateTemporarySSHAuthorizationParams struct {
	TargetID  string
	Name      string
	CreatedBy string
	ExpiresAt time.Time
}

const temporarySSHAuthorizationColumns = `id, target_id, name, encrypted_token, created_by, expires_at, created_at, updated_at`

func (r *Repository) CreateTemporarySSHAuthorization(ctx context.Context, params CreateTemporarySSHAuthorizationParams) (TemporarySSHAuthorization, error) {
	if r.secretBox == nil {
		return TemporarySSHAuthorization{}, errors.New("temporary SSH authorizations require an encryption key")
	}
	now := time.Now().UTC()
	grant := TemporarySSHAuthorization{
		ID: uuid.NewString(), TargetID: params.TargetID, Name: strings.TrimSpace(params.Name),
		Token: uuid.NewString(), CreatedBy: params.CreatedBy, ExpiresAt: params.ExpiresAt.UTC(),
		CreatedAt: now, UpdatedAt: now,
	}
	encryptedToken, err := r.sealSecret([]byte(grant.Token))
	if err != nil {
		return TemporarySSHAuthorization{}, err
	}
	hash := sha256.Sum256([]byte(grant.Token))
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO temporary_ssh_authorizations (
			id, target_id, name, encrypted_token, token_hash, created_by, expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, grant.ID, grant.TargetID, grant.Name, encryptedToken, hash[:], grant.CreatedBy,
		formatTime(grant.ExpiresAt), formatTime(grant.CreatedAt), formatTime(grant.UpdatedAt)); err != nil {
		return TemporarySSHAuthorization{}, err
	}
	return grant, nil
}

func (r *Repository) ListTemporarySSHAuthorizations(ctx context.Context, targetID string) ([]TemporarySSHAuthorization, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+temporarySSHAuthorizationColumns+`
		FROM temporary_ssh_authorizations WHERE target_id = ? ORDER BY created_at ASC, id ASC`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []TemporarySSHAuthorization{}
	for rows.Next() {
		grant, err := r.scanTemporarySSHAuthorization(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func (r *Repository) GetTemporarySSHAuthorization(ctx context.Context, id string) (TemporarySSHAuthorization, error) {
	return r.scanTemporarySSHAuthorization(r.db.QueryRowContext(ctx, `SELECT `+temporarySSHAuthorizationColumns+`
		FROM temporary_ssh_authorizations WHERE id = ?`, id))
}

func (r *Repository) GetTemporarySSHAuthorizationByToken(ctx context.Context, token string) (TemporarySSHAuthorization, error) {
	hash := sha256.Sum256([]byte(token))
	return r.scanTemporarySSHAuthorization(r.db.QueryRowContext(ctx, `SELECT `+temporarySSHAuthorizationColumns+`
		FROM temporary_ssh_authorizations WHERE token_hash = ?`, hash[:]))
}

func (r *Repository) RenewTemporarySSHAuthorization(ctx context.Context, id, targetID string, duration time.Duration, now time.Time) (TemporarySSHAuthorization, error) {
	if duration <= 0 {
		return TemporarySSHAuthorization{}, errors.New("renewal duration must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return TemporarySSHAuthorization{}, err
	}
	defer tx.Rollback()
	grant, err := r.scanTemporarySSHAuthorization(tx.QueryRowContext(ctx, `SELECT `+temporarySSHAuthorizationColumns+`
		FROM temporary_ssh_authorizations WHERE id = ? AND target_id = ?`, id, targetID))
	if err != nil {
		return TemporarySSHAuthorization{}, err
	}
	base := grant.ExpiresAt
	if base.Before(now) {
		base = now
	}
	grant.ExpiresAt = base.Add(duration).UTC()
	grant.UpdatedAt = now.UTC()
	res, err := tx.ExecContext(ctx, `UPDATE temporary_ssh_authorizations
		SET expires_at = ?, updated_at = ? WHERE id = ? AND target_id = ?`,
		formatTime(grant.ExpiresAt), formatTime(grant.UpdatedAt), id, targetID)
	if err != nil {
		return TemporarySSHAuthorization{}, err
	}
	if err := requireRowsAffected(res); err != nil {
		return TemporarySSHAuthorization{}, err
	}
	if err := tx.Commit(); err != nil {
		return TemporarySSHAuthorization{}, err
	}
	return grant, nil
}

func (r *Repository) DeleteTemporarySSHAuthorization(ctx context.Context, id, targetID string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM temporary_ssh_authorizations WHERE id = ? AND target_id = ?`, id, targetID)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) scanTemporarySSHAuthorization(row targetScanner) (TemporarySSHAuthorization, error) {
	var grant TemporarySSHAuthorization
	var encryptedToken []byte
	var expiresAt, createdAt, updatedAt string
	if err := row.Scan(&grant.ID, &grant.TargetID, &grant.Name, &encryptedToken, &grant.CreatedBy,
		&expiresAt, &createdAt, &updatedAt); err != nil {
		return TemporarySSHAuthorization{}, wrapScanErr(err)
	}
	token, err := r.openSecret(encryptedToken)
	if err != nil {
		return TemporarySSHAuthorization{}, err
	}
	grant.Token = string(token)
	grant.ExpiresAt = parseTime(expiresAt)
	grant.CreatedAt = parseTime(createdAt)
	grant.UpdatedAt = parseTime(updatedAt)
	return grant, nil
}
