package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")

type Repository struct {
	db *sql.DB
}

func (r *Repository) CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
	now := time.Now().UTC()
	user := User{
		ID:           uuid.NewString(),
		Email:        strings.ToLower(strings.TrimSpace(params.Email)),
		DisplayName:  strings.TrimSpace(params.DisplayName),
		PasswordHash: append([]byte(nil), params.PasswordHash...),
		CreatedAt:    now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, user.ID, user.Email, user.DisplayName, user.PasswordHash, formatTime(user.CreatedAt))
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (r *Repository) GetUser(ctx context.Context, id string) (User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, email, display_name, password_hash, created_at
		FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, email, display_name, password_hash, created_at
		FROM users WHERE email = ?
	`, strings.ToLower(strings.TrimSpace(email)))
	return scanUser(row)
}

func (r *Repository) CreateOrganization(ctx context.Context, params CreateOrganizationParams) (Organization, error) {
	now := time.Now().UTC()
	org := Organization{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(params.Name),
		Slug:        strings.TrimSpace(params.Slug),
		OwnerUserID: params.OwnerUserID,
		CreatedAt:   now,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Organization{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO organizations (id, name, slug, owner_user_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, org.ID, org.Name, org.Slug, org.OwnerUserID, formatTime(org.CreatedAt)); err != nil {
		return Organization{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO organization_members (organization_id, user_id, role, created_at)
		VALUES (?, ?, ?, ?)
	`, org.ID, org.OwnerUserID, RoleOwner, formatTime(now)); err != nil {
		return Organization{}, err
	}
	if err := tx.Commit(); err != nil {
		return Organization{}, err
	}
	return org, nil
}

func (r *Repository) GetOrganizationMember(ctx context.Context, organizationID, userID string) (OrganizationMember, error) {
	var member OrganizationMember
	var created string
	err := r.db.QueryRowContext(ctx, `
		SELECT organization_id, user_id, role, created_at
		FROM organization_members
		WHERE organization_id = ? AND user_id = ?
	`, organizationID, userID).Scan(&member.OrganizationID, &member.UserID, &member.Role, &created)
	if err != nil {
		return OrganizationMember{}, wrapScanErr(err)
	}
	member.CreatedAt = parseTime(created)
	return member, nil
}

func (r *Repository) CreatePublicKey(ctx context.Context, params CreatePublicKeyParams) (PublicKey, error) {
	key := PublicKey{
		ID:            uuid.NewString(),
		UserID:        params.UserID,
		Name:          strings.TrimSpace(params.Name),
		AuthorizedKey: strings.TrimSpace(params.AuthorizedKey),
		Fingerprint:   strings.TrimSpace(params.Fingerprint),
		CreatedAt:     time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_public_keys (id, user_id, name, authorized_key, fingerprint, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, key.ID, key.UserID, key.Name, key.AuthorizedKey, key.Fingerprint, formatTime(key.CreatedAt))
	if err != nil {
		return PublicKey{}, err
	}
	return key, nil
}

func (r *Repository) GetUserByPublicKeyFingerprint(ctx context.Context, fingerprint string) (User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, u.password_hash, u.created_at
		FROM users u
		JOIN user_public_keys k ON k.user_id = u.id
		WHERE k.fingerprint = ?
	`, strings.TrimSpace(fingerprint))
	return scanUser(row)
}

func (r *Repository) CreateSSHTarget(ctx context.Context, params CreateSSHTargetParams) (SSHTarget, error) {
	now := time.Now().UTC()
	target := SSHTarget{
		ID:              uuid.NewString(),
		OwnerType:       params.OwnerType,
		OwnerID:         params.OwnerID,
		Alias:           strings.TrimSpace(params.Alias),
		TargetType:      params.TargetType,
		Host:            strings.TrimSpace(params.Host),
		Port:            params.Port,
		RemoteUsername:  strings.TrimSpace(params.RemoteUsername),
		AuthType:        params.AuthType,
		EncryptedSecret: append([]byte(nil), params.EncryptedSecret...),
		AgentID:         params.AgentID,
		CreatedBy:       params.CreatedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ssh_targets (
			id, owner_type, owner_id, alias, target_type, host, port,
			remote_username, auth_type, encrypted_secret, agent_id,
			created_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, target.ID, target.OwnerType, target.OwnerID, target.Alias, target.TargetType, target.Host, target.Port,
		target.RemoteUsername, target.AuthType, nullableBytes(target.EncryptedSecret), nullableString(target.AgentID),
		target.CreatedBy, formatTime(target.CreatedAt), formatTime(target.UpdatedAt))
	if err != nil {
		return SSHTarget{}, err
	}
	return target, nil
}

func (r *Repository) ResolveUserTarget(ctx context.Context, userID, alias string) (SSHTarget, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, alias, target_type, host, port, remote_username,
			auth_type, encrypted_secret, COALESCE(agent_id, ''), created_by, created_at, updated_at
		FROM ssh_targets
		WHERE owner_type = ? AND owner_id = ? AND alias = ?
	`, OwnerUser, userID, strings.TrimSpace(alias))
	return scanTarget(row)
}

func (r *Repository) CreateCommandPolicy(ctx context.Context, params CreateCommandPolicyParams) (CommandPolicy, error) {
	policy := CommandPolicy{
		ID:            uuid.NewString(),
		OwnerType:     params.OwnerType,
		OwnerID:       params.OwnerID,
		Name:          strings.TrimSpace(params.Name),
		DefaultAction: params.DefaultAction,
		LLMConfigID:   params.LLMConfigID,
		CreatedAt:     time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO command_policies (id, owner_type, owner_id, name, default_action, llm_config_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, policy.ID, policy.OwnerType, policy.OwnerID, policy.Name, policy.DefaultAction, nullableString(policy.LLMConfigID), formatTime(policy.CreatedAt))
	if err != nil {
		return CommandPolicy{}, err
	}
	return policy, nil
}

func (r *Repository) CreatePolicyRule(ctx context.Context, params CreatePolicyRuleParams) (PolicyRule, error) {
	rule := PolicyRule{
		ID:          uuid.NewString(),
		PolicyID:    params.PolicyID,
		RuleType:    params.RuleType,
		PatternType: params.PatternType,
		Pattern:     params.Pattern,
		CreatedAt:   time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO policy_rules (id, policy_id, rule_type, pattern_type, pattern, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, rule.ID, rule.PolicyID, rule.RuleType, rule.PatternType, rule.Pattern, formatTime(rule.CreatedAt))
	if err != nil {
		return PolicyRule{}, err
	}
	return rule, nil
}

func (r *Repository) AttachPolicyToTarget(ctx context.Context, policyID, targetID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO policy_targets (policy_id, target_id)
		VALUES (?, ?)
	`, policyID, targetID)
	return err
}

func (r *Repository) ListPoliciesForTarget(ctx context.Context, targetID string) ([]CommandPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.owner_type, p.owner_id, p.name, p.default_action, COALESCE(p.llm_config_id, ''), p.created_at
		FROM command_policies p
		JOIN policy_targets pt ON pt.policy_id = p.id
		WHERE pt.target_id = ?
		ORDER BY p.created_at ASC
	`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []CommandPolicy
	for rows.Next() {
		var policy CommandPolicy
		var created string
		if err := rows.Scan(&policy.ID, &policy.OwnerType, &policy.OwnerID, &policy.Name, &policy.DefaultAction, &policy.LLMConfigID, &created); err != nil {
			return nil, err
		}
		policy.CreatedAt = parseTime(created)
		rules, err := r.listPolicyRules(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.Rules = rules
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *Repository) listPolicyRules(ctx context.Context, policyID string) ([]PolicyRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, policy_id, rule_type, pattern_type, pattern, created_at
		FROM policy_rules
		WHERE policy_id = ?
		ORDER BY created_at ASC
	`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []PolicyRule
	for rows.Next() {
		var rule PolicyRule
		var created string
		if err := rows.Scan(&rule.ID, &rule.PolicyID, &rule.RuleType, &rule.PatternType, &rule.Pattern, &created); err != nil {
			return nil, err
		}
		rule.CreatedAt = parseTime(created)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *Repository) CreateCommandAuditLog(ctx context.Context, params CreateCommandAuditLogParams) (CommandAuditLog, error) {
	started := params.StartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	log := CommandAuditLog{
		ID:             uuid.NewString(),
		UserID:         params.UserID,
		TargetID:       params.TargetID,
		OrganizationID: params.OrganizationID,
		SessionID:      params.SessionID,
		Command:        params.Command,
		RequestType:    params.RequestType,
		PolicyDecision: params.PolicyDecision,
		PolicyReason:   params.PolicyReason,
		ExitCode:       params.ExitCode,
		StartedAt:      started.UTC(),
		EndedAt:        params.EndedAt,
		RemoteAddress:  params.RemoteAddress,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO command_audit_logs (
			id, user_id, target_id, organization_id, session_id, command, request_type,
			policy_decision, policy_reason, exit_code, started_at, ended_at, remote_address
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.UserID, log.TargetID, nullableString(log.OrganizationID), log.SessionID, log.Command, log.RequestType,
		log.PolicyDecision, log.PolicyReason, nullableInt(log.ExitCode), formatTime(log.StartedAt), nullableTime(log.EndedAt), log.RemoteAddress)
	if err != nil {
		return CommandAuditLog{}, err
	}
	return log, nil
}

func (r *Repository) ListCommandAuditLogs(ctx context.Context, filter AuditLogFilter) ([]CommandAuditLog, error) {
	query := `
		SELECT id, user_id, target_id, COALESCE(organization_id, ''), session_id, command, request_type,
			policy_decision, policy_reason, exit_code, started_at, ended_at, remote_address
		FROM command_audit_logs
		WHERE 1 = 1`
	args := []any{}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.TargetID != "" {
		query += ` AND target_id = ?`
		args = append(args, filter.TargetID)
	}
	query += ` ORDER BY started_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []CommandAuditLog
	for rows.Next() {
		var log CommandAuditLog
		var exit sql.NullInt64
		var started string
		var ended sql.NullString
		if err := rows.Scan(&log.ID, &log.UserID, &log.TargetID, &log.OrganizationID, &log.SessionID, &log.Command,
			&log.RequestType, &log.PolicyDecision, &log.PolicyReason, &exit, &started, &ended, &log.RemoteAddress); err != nil {
			return nil, err
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
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func scanUser(row *sql.Row) (User, error) {
	var user User
	var created string
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &created)
	if err != nil {
		return User{}, wrapScanErr(err)
	}
	user.CreatedAt = parseTime(created)
	return user, nil
}

func scanTarget(row *sql.Row) (SSHTarget, error) {
	var target SSHTarget
	var created, updated string
	err := row.Scan(&target.ID, &target.OwnerType, &target.OwnerID, &target.Alias, &target.TargetType,
		&target.Host, &target.Port, &target.RemoteUsername, &target.AuthType, &target.EncryptedSecret,
		&target.AgentID, &target.CreatedBy, &created, &updated)
	if err != nil {
		return SSHTarget{}, wrapScanErr(err)
	}
	target.CreatedAt = parseTime(created)
	target.UpdatedAt = parseTime(updated)
	return target, nil
}

func wrapScanErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func (r *Repository) String() string {
	return fmt.Sprintf("Repository{%p}", r.db)
}
