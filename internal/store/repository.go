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

func (r *Repository) CreateSession(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) (Session, error) {
	session := Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: append([]byte(nil), tokenHash...),
		ExpiresAt: expiresAt.UTC(),
		CreatedAt: time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, session.ID, session.UserID, session.TokenHash, formatTime(session.ExpiresAt), formatTime(session.CreatedAt))
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (r *Repository) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	var session Session
	var expires, created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE token_hash = ?
	`, tokenHash).Scan(&session.ID, &session.UserID, &session.TokenHash, &expires, &created)
	if err != nil {
		return Session{}, wrapScanErr(err)
	}
	session.ExpiresAt = parseTime(expires)
	session.CreatedAt = parseTime(created)
	return session, nil
}

func (r *Repository) DeleteSessionByTokenHash(ctx context.Context, tokenHash []byte) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
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
	groupID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO organization_user_groups (id, organization_id, name, slug, is_default, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, groupID, org.ID, "All Members", "all", 1, formatTime(now)); err != nil {
		return Organization{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO organization_user_group_members (group_id, user_id, created_at)
		VALUES (?, ?, ?)
	`, groupID, org.OwnerUserID, formatTime(now)); err != nil {
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

func (r *Repository) ListOrganizationsForUser(ctx context.Context, userID string) ([]Organization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.id, o.name, o.slug, o.owner_user_id, o.created_at
		FROM organizations o
		JOIN organization_members m ON m.organization_id = o.id
		WHERE m.user_id = ?
		ORDER BY o.created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orgs []Organization
	for rows.Next() {
		var org Organization
		var created string
		if err := rows.Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &created); err != nil {
			return nil, err
		}
		org.CreatedAt = parseTime(created)
		orgs = append(orgs, org)
	}
	return orgs, rows.Err()
}

func (r *Repository) CreateOrganizationUserGroup(ctx context.Context, params CreateOrganizationUserGroupParams) (OrganizationUserGroup, error) {
	group := OrganizationUserGroup{
		ID:             uuid.NewString(),
		OrganizationID: params.OrganizationID,
		Name:           strings.TrimSpace(params.Name),
		Slug:           strings.TrimSpace(params.Slug),
		IsDefault:      params.IsDefault,
		CreatedAt:      time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO organization_user_groups (id, organization_id, name, slug, is_default, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, group.ID, group.OrganizationID, group.Name, group.Slug, boolInt(group.IsDefault), formatTime(group.CreatedAt))
	if err != nil {
		return OrganizationUserGroup{}, err
	}
	return group, nil
}

func (r *Repository) ListOrganizationUserGroups(ctx context.Context, organizationID string) ([]OrganizationUserGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, organization_id, name, slug, is_default, created_at
		FROM organization_user_groups
		WHERE organization_id = ?
		ORDER BY is_default DESC, created_at ASC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []OrganizationUserGroup
	for rows.Next() {
		var group OrganizationUserGroup
		var isDefault int
		var created string
		if err := rows.Scan(&group.ID, &group.OrganizationID, &group.Name, &group.Slug, &isDefault, &created); err != nil {
			return nil, err
		}
		group.IsDefault = isDefault == 1
		group.CreatedAt = parseTime(created)
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r *Repository) GetDefaultOrganizationUserGroup(ctx context.Context, organizationID string) (OrganizationUserGroup, error) {
	var group OrganizationUserGroup
	var isDefault int
	var created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, organization_id, name, slug, is_default, created_at
		FROM organization_user_groups
		WHERE organization_id = ? AND is_default = 1
	`, organizationID).Scan(&group.ID, &group.OrganizationID, &group.Name, &group.Slug, &isDefault, &created)
	if err != nil {
		return OrganizationUserGroup{}, wrapScanErr(err)
	}
	group.IsDefault = isDefault == 1
	group.CreatedAt = parseTime(created)
	return group, nil
}

func (r *Repository) AddUserToGroup(ctx context.Context, groupID, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO organization_user_group_members (group_id, user_id, created_at)
		VALUES (?, ?, ?)
	`, groupID, userID, formatTime(time.Now().UTC()))
	return err
}

func (r *Repository) RemoveUserFromGroup(ctx context.Context, groupID, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM organization_user_group_members
		WHERE group_id = ? AND user_id = ?
	`, groupID, userID)
	return err
}

func (r *Repository) UserInGroup(ctx context.Context, groupID, userID string) (bool, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organization_user_group_members
		WHERE group_id = ? AND user_id = ?
	`, groupID, userID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) GetOrganization(ctx context.Context, id string) (Organization, error) {
	var org Organization
	var created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, slug, owner_user_id, created_at
		FROM organizations WHERE id = ?
	`, id).Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &created)
	if err != nil {
		return Organization{}, wrapScanErr(err)
	}
	org.CreatedAt = parseTime(created)
	return org, nil
}

func (r *Repository) CreateOrganizationInvite(ctx context.Context, params CreateOrganizationInviteParams) (OrganizationInvite, error) {
	invite := OrganizationInvite{
		ID:             uuid.NewString(),
		OrganizationID: params.OrganizationID,
		CodeHash:       append([]byte(nil), params.CodeHash...),
		Role:           params.Role,
		ExpiresAt:      params.ExpiresAt.UTC(),
		CreatedBy:      params.CreatedBy,
		CreatedAt:      time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO organization_invites (id, organization_id, code_hash, role, expires_at, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, invite.ID, invite.OrganizationID, invite.CodeHash, invite.Role, formatTime(invite.ExpiresAt), invite.CreatedBy, formatTime(invite.CreatedAt))
	if err != nil {
		return OrganizationInvite{}, err
	}
	return invite, nil
}

func (r *Repository) GetOrganizationInviteByCodeHash(ctx context.Context, codeHash []byte) (OrganizationInvite, error) {
	var invite OrganizationInvite
	var expires, created string
	var consumed sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, organization_id, code_hash, role, expires_at, created_by, created_at, consumed_at
		FROM organization_invites WHERE code_hash = ?
	`, codeHash).Scan(&invite.ID, &invite.OrganizationID, &invite.CodeHash, &invite.Role, &expires, &invite.CreatedBy, &created, &consumed)
	if err != nil {
		return OrganizationInvite{}, wrapScanErr(err)
	}
	invite.ExpiresAt = parseTime(expires)
	invite.CreatedAt = parseTime(created)
	if consumed.Valid {
		v := parseTime(consumed.String)
		invite.ConsumedAt = &v
	}
	return invite, nil
}

func (r *Repository) AddOrganizationMember(ctx context.Context, organizationID, userID, role string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO organization_members (organization_id, user_id, role, created_at)
		VALUES (?, ?, ?, ?)
	`, organizationID, userID, role, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	var defaultGroupID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM organization_user_groups
		WHERE organization_id = ? AND is_default = 1
	`, organizationID).Scan(&defaultGroupID); err != nil {
		return wrapScanErr(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO organization_user_group_members (group_id, user_id, created_at)
		VALUES (?, ?, ?)
	`, defaultGroupID, userID, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) MarkOrganizationInviteConsumed(ctx context.Context, inviteID string, consumedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE organization_invites SET consumed_at = ? WHERE id = ?
	`, formatTime(consumedAt), inviteID)
	return err
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

func (r *Repository) ListPublicKeysForUser(ctx context.Context, userID string) ([]PublicKey, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, authorized_key, fingerprint, created_at
		FROM user_public_keys
		WHERE user_id = ?
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []PublicKey
	for rows.Next() {
		var key PublicKey
		var created string
		if err := rows.Scan(&key.ID, &key.UserID, &key.Name, &key.AuthorizedKey, &key.Fingerprint, &created); err != nil {
			return nil, err
		}
		key.CreatedAt = parseTime(created)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *Repository) DeletePublicKey(ctx context.Context, userID, keyID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_public_keys WHERE user_id = ? AND id = ?`, userID, keyID)
	return err
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

func (r *Repository) ListSSHTargets(ctx context.Context, ownerType, ownerID string) ([]SSHTarget, error) {
	query := `
		SELECT id, owner_type, owner_id, alias, target_type, host, port, remote_username,
			auth_type, encrypted_secret, COALESCE(agent_id, ''), created_by, created_at, updated_at
		FROM ssh_targets
		WHERE 1 = 1`
	args := []any{}
	if ownerType != "" {
		query += ` AND owner_type = ?`
		args = append(args, ownerType)
	}
	if ownerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, ownerID)
	}
	query += ` ORDER BY created_at ASC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []SSHTarget
	for rows.Next() {
		target, err := scanTargetRows(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
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

func (r *Repository) CreateAgentEnrollment(ctx context.Context, params CreateAgentEnrollmentParams) (AgentEnrollment, error) {
	enrollment := AgentEnrollment{
		ID:          uuid.NewString(),
		OwnerType:   params.OwnerType,
		OwnerID:     params.OwnerID,
		TokenHash:   append([]byte(nil), params.TokenHash...),
		Label:       strings.TrimSpace(params.Label),
		DefaultHost: strings.TrimSpace(params.DefaultHost),
		DefaultPort: params.DefaultPort,
		CreatedBy:   params.CreatedBy,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   params.ExpiresAt.UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_enrollments (
			id, owner_type, owner_id, token_hash, label, default_host, default_port,
			created_by, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, enrollment.ID, enrollment.OwnerType, enrollment.OwnerID, enrollment.TokenHash, enrollment.Label,
		enrollment.DefaultHost, enrollment.DefaultPort, enrollment.CreatedBy, formatTime(enrollment.CreatedAt), formatTime(enrollment.ExpiresAt))
	if err != nil {
		return AgentEnrollment{}, err
	}
	return enrollment, nil
}

func (r *Repository) ListAgentEnrollments(ctx context.Context, ownerType, ownerID string) ([]AgentEnrollment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_type, owner_id, token_hash, label, default_host, default_port,
			created_by, created_at, expires_at, COALESCE(consumed_agent_id, '')
		FROM agent_enrollments
		WHERE owner_type = ? AND owner_id = ?
		ORDER BY created_at ASC
	`, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var enrollments []AgentEnrollment
	for rows.Next() {
		var enrollment AgentEnrollment
		var created, expires string
		if err := rows.Scan(&enrollment.ID, &enrollment.OwnerType, &enrollment.OwnerID, &enrollment.TokenHash, &enrollment.Label,
			&enrollment.DefaultHost, &enrollment.DefaultPort, &enrollment.CreatedBy, &created, &expires, &enrollment.ConsumedAgentID); err != nil {
			return nil, err
		}
		enrollment.CreatedAt = parseTime(created)
		enrollment.ExpiresAt = parseTime(expires)
		enrollments = append(enrollments, enrollment)
	}
	return enrollments, rows.Err()
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

func (r *Repository) ListCommandPolicies(ctx context.Context, ownerType, ownerID string) ([]CommandPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_type, owner_id, name, default_action, COALESCE(llm_config_id, ''), created_at
		FROM command_policies
		WHERE owner_type = ? AND owner_id = ?
		ORDER BY created_at ASC
	`, ownerType, ownerID)
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
		userGroupIDs, err := r.listPolicyUserGroupIDs(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.UserGroupIDs = userGroupIDs
		policies = append(policies, policy)
	}
	return policies, rows.Err()
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

func (r *Repository) AttachPolicyToUserGroup(ctx context.Context, policyID, groupID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO policy_user_groups (policy_id, group_id)
		VALUES (?, ?)
	`, policyID, groupID)
	return err
}

func (r *Repository) DetachPolicyFromUserGroup(ctx context.Context, policyID, groupID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM policy_user_groups WHERE policy_id = ? AND group_id = ?
	`, policyID, groupID)
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
		userGroupIDs, err := r.listPolicyUserGroupIDs(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.UserGroupIDs = userGroupIDs
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *Repository) listPolicyUserGroupIDs(ctx context.Context, policyID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT group_id FROM policy_user_groups
		WHERE policy_id = ?
		ORDER BY group_id ASC
	`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

type targetScanner interface {
	Scan(dest ...any) error
}

func scanTargetRows(row targetScanner) (SSHTarget, error) {
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

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (r *Repository) String() string {
	return fmt.Sprintf("Repository{%p}", r.db)
}
