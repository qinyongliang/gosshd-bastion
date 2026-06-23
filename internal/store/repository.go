package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("not found")

const defaultAuthProvider = "local"

type Repository struct {
	db *sql.DB
}

func (r *Repository) CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
	now := time.Now().UTC()
	user := User{
		ID:            uuid.NewString(),
		Email:         strings.ToLower(strings.TrimSpace(params.Email)),
		DisplayName:   strings.TrimSpace(params.DisplayName),
		PasswordHash:  append([]byte(nil), params.PasswordHash...),
		IsSystemAdmin: params.IsSystemAdmin,
		AuthProvider:  normalizeAuthProvider(params.AuthProvider),
		CreatedAt:     now,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (
			id, email, display_name, password_hash, is_system_admin, auth_provider,
			disabled_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, user.ID, user.Email, user.DisplayName, user.PasswordHash, boolInt(user.IsSystemAdmin),
		user.AuthProvider, nullableTime(user.DisabledAt), formatTime(user.CreatedAt)); err != nil {
		return User{}, err
	}
	if _, err := r.createOrganizationTx(ctx, tx, CreateOrganizationParams{
		Name:        personalOrganizationName(user),
		Slug:        personalOrganizationSlug(user),
		OwnerUserID: user.ID,
		IsPersonal:  true,
	}, now); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return user, nil
}

func (r *Repository) GetUser(ctx context.Context, id string) (User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, email, display_name, password_hash, is_system_admin,
			auth_provider, disabled_at, created_at
		FROM users WHERE id = ?
	`, id)
	return scanUser(row)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, email, display_name, password_hash, is_system_admin,
			auth_provider, disabled_at, created_at
		FROM users WHERE email = ?
	`, strings.ToLower(strings.TrimSpace(email)))
	return scanUser(row)
}

func (r *Repository) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, email, display_name, password_hash, is_system_admin,
			auth_provider, disabled_at, created_at
		FROM users
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *Repository) UpdateUserSystemAdmin(ctx context.Context, userID string, isAdmin bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE users SET is_system_admin = ? WHERE id = ?
	`, boolInt(isAdmin), userID)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) UpdateUserPasswordHash(ctx context.Context, userID string, passwordHash []byte) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ? WHERE id = ?
	`, append([]byte(nil), passwordHash...), userID)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) EnsureBootstrapAdmin(ctx context.Context, password string) (User, string, error) {
	if existing, err := r.GetUserByEmail(ctx, "admin"); err == nil {
		if !existing.IsSystemAdmin {
			if err := r.UpdateUserSystemAdmin(ctx, existing.ID, true); err != nil {
				return User{}, "", err
			}
			existing.IsSystemAdmin = true
		}
		return existing, "", nil
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, "", err
	}
	password = strings.TrimSpace(password)
	if password == "" {
		password = uuid.NewString()
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", err
	}
	user, err := r.CreateUser(ctx, CreateUserParams{
		Email:         "admin",
		DisplayName:   "Administrator",
		PasswordHash:  hash,
		IsSystemAdmin: true,
		AuthProvider:  defaultAuthProvider,
	})
	if err != nil {
		return User{}, "", err
	}
	return user, password, nil
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

func (r *Repository) UpsertSystemSetting(ctx context.Context, key string, valueJSON []byte, updatedBy string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("setting key is required")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO system_settings (key, value_json, updated_at, updated_by)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by
	`, key, string(valueJSON), formatTime(time.Now().UTC()), nullableString(updatedBy))
	return err
}

func (r *Repository) GetSystemSetting(ctx context.Context, key string) (SystemSetting, error) {
	var setting SystemSetting
	var updated string
	var updatedBy sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT key, value_json, updated_at, updated_by
		FROM system_settings
		WHERE key = ?
	`, strings.TrimSpace(key)).Scan(&setting.Key, &setting.ValueJSON, &updated, &updatedBy)
	if err != nil {
		return SystemSetting{}, wrapScanErr(err)
	}
	setting.UpdatedAt = parseTime(updated)
	if updatedBy.Valid {
		setting.UpdatedBy = updatedBy.String
	}
	return setting, nil
}

func (r *Repository) CreateMCPToken(ctx context.Context, params CreateMCPTokenParams) (MCPToken, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = "MCP token"
	}
	if params.UserID == "" {
		return MCPToken{}, errors.New("user id is required")
	}
	if len(params.TokenHash) == 0 {
		return MCPToken{}, errors.New("token hash is required")
	}
	token := MCPToken{
		ID:         uuid.NewString(),
		UserID:     params.UserID,
		Name:       name,
		TokenHash:  append([]byte(nil), params.TokenHash...),
		ToolGroups: normalizeMCPToolGroups(params.ToolGroups),
		CreatedAt:  time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO mcp_tokens (id, user_id, name, token_hash, tool_groups, last_used_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, token.ID, token.UserID, token.Name, token.TokenHash, encodeMCPToolGroups(token.ToolGroups), nullableTime(token.LastUsedAt), formatTime(token.CreatedAt))
	if err != nil {
		return MCPToken{}, err
	}
	return token, nil
}

func (r *Repository) ListMCPTokensForUser(ctx context.Context, userID string) ([]MCPToken, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, token_hash, tool_groups, last_used_at, created_at
		FROM mcp_tokens
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []MCPToken
	for rows.Next() {
		token, err := scanMCPTokenRows(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (r *Repository) GetMCPTokenByHash(ctx context.Context, tokenHash []byte) (MCPToken, error) {
	token, err := scanMCPTokenRows(r.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, token_hash, tool_groups, last_used_at, created_at
		FROM mcp_tokens
		WHERE token_hash = ?
	`, tokenHash))
	if err != nil {
		return MCPToken{}, wrapScanErr(err)
	}
	return token, nil
}

func (r *Repository) TouchMCPToken(ctx context.Context, tokenID string, usedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE mcp_tokens SET last_used_at = ? WHERE id = ?
	`, formatTime(usedAt.UTC()), tokenID)
	return err
}

func (r *Repository) DeleteMCPToken(ctx context.Context, userID, tokenID string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM mcp_tokens WHERE user_id = ? AND id = ?
	`, userID, tokenID)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) CreateExternalIdentity(ctx context.Context, params CreateExternalIdentityParams) (ExternalIdentity, error) {
	now := time.Now().UTC()
	identity := ExternalIdentity{
		ID:             uuid.NewString(),
		UserID:         params.UserID,
		Provider:       strings.TrimSpace(params.Provider),
		Subject:        strings.TrimSpace(params.Subject),
		Email:          strings.ToLower(strings.TrimSpace(params.Email)),
		DisplayName:    strings.TrimSpace(params.DisplayName),
		RawProfileJSON: strings.TrimSpace(params.RawProfileJSON),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if identity.Provider == "" || identity.Subject == "" {
		return ExternalIdentity{}, errors.New("external identity provider and subject are required")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO external_identities (
			id, user_id, provider, subject, email, display_name, raw_profile_json,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, identity.ID, identity.UserID, identity.Provider, identity.Subject, identity.Email, identity.DisplayName,
		identity.RawProfileJSON, formatTime(identity.CreatedAt), formatTime(identity.UpdatedAt))
	if err != nil {
		return ExternalIdentity{}, err
	}
	return identity, nil
}

func (r *Repository) GetExternalIdentity(ctx context.Context, provider, subject string) (ExternalIdentity, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, provider, subject, email, display_name, raw_profile_json,
			created_at, updated_at
		FROM external_identities
		WHERE provider = ? AND subject = ?
	`, strings.TrimSpace(provider), strings.TrimSpace(subject))
	return scanExternalIdentity(row)
}

func (r *Repository) CreateOAuthState(ctx context.Context, provider string, stateHash []byte, redirectAfter string, expiresAt time.Time) error {
	provider = strings.TrimSpace(provider)
	if provider == "" || len(stateHash) == 0 {
		return errors.New("oauth provider and state are required")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO oauth_states (state_hash, provider, redirect_after, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, append([]byte(nil), stateHash...), provider, strings.TrimSpace(redirectAfter),
		formatTime(expiresAt.UTC()), formatTime(time.Now().UTC()))
	return err
}

func (r *Repository) ConsumeOAuthState(ctx context.Context, provider string, stateHash []byte) (OAuthState, error) {
	provider = strings.TrimSpace(provider)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return OAuthState{}, err
	}
	defer tx.Rollback()
	var state OAuthState
	var expires, created string
	err = tx.QueryRowContext(ctx, `
		SELECT state_hash, provider, redirect_after, expires_at, created_at
		FROM oauth_states
		WHERE provider = ? AND state_hash = ?
	`, provider, stateHash).Scan(&state.StateHash, &state.Provider, &state.RedirectAfter, &expires, &created)
	if err != nil {
		return OAuthState{}, wrapScanErr(err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM oauth_states WHERE provider = ? AND state_hash = ?
	`, provider, stateHash); err != nil {
		return OAuthState{}, err
	}
	state.ExpiresAt = parseTime(expires)
	state.CreatedAt = parseTime(created)
	if time.Now().UTC().After(state.ExpiresAt) {
		if err := tx.Commit(); err != nil {
			return OAuthState{}, err
		}
		return OAuthState{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return OAuthState{}, err
	}
	return state, nil
}

func (r *Repository) CreateOrganization(ctx context.Context, params CreateOrganizationParams) (Organization, error) {
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Organization{}, err
	}
	defer tx.Rollback()
	org, err := r.createOrganizationTx(ctx, tx, params, now)
	if err != nil {
		return Organization{}, err
	}
	if err := tx.Commit(); err != nil {
		return Organization{}, err
	}
	return org, nil
}

func (r *Repository) createOrganizationTx(ctx context.Context, tx *sql.Tx, params CreateOrganizationParams, now time.Time) (Organization, error) {
	org := Organization{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(params.Name),
		Slug:        strings.TrimSpace(params.Slug),
		OwnerUserID: params.OwnerUserID,
		IsPersonal:  params.IsPersonal,
		CreatedAt:   now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO organizations (id, name, slug, owner_user_id, is_personal, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, org.ID, org.Name, org.Slug, org.OwnerUserID, boolInt(org.IsPersonal), formatTime(org.CreatedAt)); err != nil {
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
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO llm_prompt_resources (
			id, owner_type, owner_id, title, content, is_default, is_readonly, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), OwnerOrganization, org.ID, DefaultLLMPromptTitle, DefaultLLMPromptContent, 1, 1, formatTime(now)); err != nil {
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

func (r *Repository) ListOrganizationMembers(ctx context.Context, organizationID string) ([]OrganizationMemberWithUser, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.organization_id, m.user_id, u.email, u.display_name, m.role, m.created_at
		FROM organization_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = ?
		ORDER BY
			CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
			u.email ASC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []OrganizationMemberWithUser
	for rows.Next() {
		var member OrganizationMemberWithUser
		var created string
		if err := rows.Scan(&member.OrganizationID, &member.UserID, &member.Email, &member.DisplayName, &member.Role, &created); err != nil {
			return nil, err
		}
		member.CreatedAt = parseTime(created)
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *Repository) ListOrganizationsForUser(ctx context.Context, userID string) ([]Organization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.id, o.name, o.slug, o.owner_user_id, o.is_personal, o.created_at
		FROM organizations o
		JOIN organization_members m ON m.organization_id = o.id
		WHERE m.user_id = ?
		ORDER BY o.is_personal DESC, o.created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orgs []Organization
	for rows.Next() {
		var org Organization
		var isPersonal int
		var created string
		if err := rows.Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &isPersonal, &created); err != nil {
			return nil, err
		}
		org.IsPersonal = isPersonal == 1
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

func (r *Repository) GetOrganizationUserGroup(ctx context.Context, groupID string) (OrganizationUserGroup, error) {
	var group OrganizationUserGroup
	var isDefault int
	var created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, organization_id, name, slug, is_default, created_at
		FROM organization_user_groups
		WHERE id = ?
	`, groupID).Scan(&group.ID, &group.OrganizationID, &group.Name, &group.Slug, &isDefault, &created)
	if err != nil {
		return OrganizationUserGroup{}, wrapScanErr(err)
	}
	group.IsDefault = isDefault == 1
	group.CreatedAt = parseTime(created)
	return group, nil
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
	var isPersonal int
	var created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, slug, owner_user_id, is_personal, created_at
		FROM organizations WHERE id = ?
	`, id).Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &isPersonal, &created)
	if err != nil {
		return Organization{}, wrapScanErr(err)
	}
	org.IsPersonal = isPersonal == 1
	org.CreatedAt = parseTime(created)
	return org, nil
}

func (r *Repository) ListOrganizations(ctx context.Context) ([]Organization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, slug, owner_user_id, is_personal, created_at
		FROM organizations
		ORDER BY is_personal ASC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orgs []Organization
	for rows.Next() {
		var org Organization
		var isPersonal int
		var created string
		if err := rows.Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &isPersonal, &created); err != nil {
			return nil, err
		}
		org.IsPersonal = isPersonal == 1
		org.CreatedAt = parseTime(created)
		orgs = append(orgs, org)
	}
	return orgs, rows.Err()
}

func (r *Repository) GetPersonalOrganizationForUser(ctx context.Context, userID string) (Organization, error) {
	var org Organization
	var isPersonal int
	var created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, slug, owner_user_id, is_personal, created_at
		FROM organizations
		WHERE owner_user_id = ? AND is_personal = 1
	`, userID).Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &isPersonal, &created)
	if err != nil {
		return Organization{}, wrapScanErr(err)
	}
	org.IsPersonal = isPersonal == 1
	org.CreatedAt = parseTime(created)
	return org, nil
}

func (r *Repository) LeaveOrganization(ctx context.Context, organizationID, userID string) error {
	org, err := r.GetOrganization(ctx, organizationID)
	if err != nil {
		return err
	}
	if org.IsPersonal {
		return errors.New("personal organization cannot be left")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM organization_user_group_members
		WHERE user_id = ? AND group_id IN (
			SELECT id FROM organization_user_groups WHERE organization_id = ?
		)
	`, userID, organizationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM organization_members
		WHERE organization_id = ? AND user_id = ?
	`, organizationID, userID); err != nil {
		return err
	}
	return tx.Commit()
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

func (r *Repository) UpdateOrganizationMemberRole(ctx context.Context, organizationID, userID, role string) error {
	role = strings.TrimSpace(role)
	if role != RoleAdmin && role != RoleMember && role != RoleOwner {
		return errors.New("invalid organization role")
	}
	org, err := r.GetOrganization(ctx, organizationID)
	if err != nil {
		return err
	}
	if org.OwnerUserID == userID && role != RoleOwner {
		return errors.New("organization owner role cannot be changed without transfer")
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE organization_members SET role = ?
		WHERE organization_id = ? AND user_id = ?
	`, role, organizationID, userID)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) RemoveOrganizationMember(ctx context.Context, organizationID, userID string) error {
	org, err := r.GetOrganization(ctx, organizationID)
	if err != nil {
		return err
	}
	if org.OwnerUserID == userID {
		return errors.New("organization owner cannot be removed")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM organization_user_group_members
		WHERE user_id = ? AND group_id IN (
			SELECT id FROM organization_user_groups WHERE organization_id = ?
		)
	`, userID, organizationID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		DELETE FROM organization_members
		WHERE organization_id = ? AND user_id = ?
	`, organizationID, userID)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) TransferOrganizationOwner(ctx context.Context, organizationID, newOwnerID, previousOwnerRole string) error {
	if previousOwnerRole == "" {
		previousOwnerRole = RoleAdmin
	}
	if previousOwnerRole != RoleAdmin && previousOwnerRole != RoleMember {
		return errors.New("previous owner role must be admin or member")
	}
	org, err := r.GetOrganization(ctx, organizationID)
	if err != nil {
		return err
	}
	if org.IsPersonal {
		return errors.New("personal organization owner cannot be transferred")
	}
	if org.OwnerUserID == newOwnerID {
		return nil
	}
	if _, err := r.GetOrganizationMember(ctx, organizationID, newOwnerID); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE organizations SET owner_user_id = ? WHERE id = ?
	`, newOwnerID, organizationID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE organization_members SET role = ?
		WHERE organization_id = ? AND user_id = ?
	`, previousOwnerRole, organizationID, org.OwnerUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE organization_members SET role = ?
		WHERE organization_id = ? AND user_id = ?
	`, RoleOwner, organizationID, newOwnerID); err != nil {
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
		SELECT u.id, u.email, u.display_name, u.password_hash, u.is_system_admin,
			u.auth_provider, u.disabled_at, u.created_at
		FROM users u
		JOIN user_public_keys k ON k.user_id = u.id
		WHERE k.fingerprint = ?
	`, strings.TrimSpace(fingerprint))
	return scanUser(row)
}

func (r *Repository) GetPublicKeyByFingerprint(ctx context.Context, fingerprint string) (PublicKey, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, authorized_key, fingerprint, created_at
		FROM user_public_keys
		WHERE fingerprint = ?
	`, strings.TrimSpace(fingerprint))
	var key PublicKey
	var created string
	if err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.AuthorizedKey, &key.Fingerprint, &created); err != nil {
		return PublicKey{}, wrapScanErr(err)
	}
	key.CreatedAt = parseTime(created)
	return key, nil
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
		Name:            normalizeTargetName(params.Name, params.Alias),
		Alias:           strings.TrimSpace(params.Alias),
		TargetType:      params.TargetType,
		Host:            strings.TrimSpace(params.Host),
		Port:            params.Port,
		RemoteUsername:  strings.TrimSpace(params.RemoteUsername),
		AuthType:        params.AuthType,
		EncryptedSecret: append([]byte(nil), params.EncryptedSecret...),
		AgentID:         params.AgentID,
		ProxyTargetID:   strings.TrimSpace(params.ProxyTargetID),
		Tags:            normalizeTags(params.Tags),
		CreatedBy:       params.CreatedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SSHTarget{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ssh_targets (
			id, owner_type, owner_id, name, alias, target_type, host, port,
			remote_username, auth_type, encrypted_secret, agent_id, proxy_target_id,
			created_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, target.ID, target.OwnerType, target.OwnerID, target.Name, target.Alias, target.TargetType, target.Host, target.Port,
		target.RemoteUsername, target.AuthType, nullableBytes(target.EncryptedSecret), nullableString(target.AgentID),
		nullableString(target.ProxyTargetID), target.CreatedBy, formatTime(target.CreatedAt), formatTime(target.UpdatedAt)); err != nil {
		return SSHTarget{}, err
	}
	if err := r.replaceTargetTagsTx(ctx, tx, target, now); err != nil {
		return SSHTarget{}, err
	}
	if err := tx.Commit(); err != nil {
		return SSHTarget{}, err
	}
	return r.GetSSHTarget(ctx, target.ID)
}

func (r *Repository) ListSSHTargets(ctx context.Context, ownerType, ownerID string) ([]SSHTarget, error) {
	return r.ListSSHTargetsFiltered(ctx, SSHTargetFilter{OwnerType: ownerType, OwnerID: ownerID})
}

func (r *Repository) ListSSHTargetsFiltered(ctx context.Context, filter SSHTargetFilter) ([]SSHTarget, error) {
	query := `
		SELECT id, owner_type, owner_id, COALESCE(name, ''), alias, target_type, host, port, remote_username,
			auth_type, encrypted_secret, COALESCE(agent_id, ''), COALESCE(proxy_target_id, ''), created_by, created_at, updated_at
		FROM ssh_targets t
		WHERE 1 = 1`
	args := []any{}
	if filter.OwnerType != "" {
		query += ` AND owner_type = ?`
		args = append(args, filter.OwnerType)
	}
	if filter.OwnerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, filter.OwnerID)
	}
	for _, tag := range normalizeTags(filter.Tags) {
		query += ` AND EXISTS (
			SELECT 1 FROM target_tag_bindings b
			JOIN target_tags tag ON tag.id = b.tag_id
			WHERE b.target_id = t.id AND tag.name = ?
		)`
		args = append(args, tag)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.loadTargetTags(ctx, targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func (r *Repository) GetSSHTarget(ctx context.Context, targetID string) (SSHTarget, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, COALESCE(name, ''), alias, target_type, host, port, remote_username,
			auth_type, encrypted_secret, COALESCE(agent_id, ''), COALESCE(proxy_target_id, ''), created_by, created_at, updated_at
		FROM ssh_targets
		WHERE id = ?
	`, targetID)
	target, err := scanTarget(row)
	if err != nil {
		return SSHTarget{}, err
	}
	target.Tags, target.TagColors, err = r.listTargetTags(ctx, target.ID)
	if err != nil {
		return SSHTarget{}, err
	}
	return target, nil
}

func (r *Repository) DeleteSSHTarget(ctx context.Context, targetID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE ssh_targets
		SET proxy_target_id = NULL
		WHERE proxy_target_id = ?
	`, targetID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_targets WHERE target_id = ?`, targetID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM target_tag_bindings WHERE target_id = ?`, targetID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM ssh_targets WHERE id = ?`, targetID)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) UpdateSSHTarget(ctx context.Context, targetID string, params UpdateSSHTargetParams) (SSHTarget, error) {
	current, err := r.GetSSHTarget(ctx, targetID)
	if err != nil {
		return SSHTarget{}, err
	}
	if params.Alias != "" {
		current.Alias = strings.TrimSpace(params.Alias)
	}
	if params.Name != "" {
		current.Name = strings.TrimSpace(params.Name)
	}
	if strings.TrimSpace(current.Name) == "" {
		current.Name = current.Alias
	}
	if params.Host != "" {
		current.Host = strings.TrimSpace(params.Host)
	}
	if params.Port != 0 {
		current.Port = params.Port
	}
	if params.RemoteUsername != "" {
		current.RemoteUsername = strings.TrimSpace(params.RemoteUsername)
	}
	if params.AuthType != "" {
		current.AuthType = params.AuthType
	}
	if params.EncryptedSecret != nil {
		current.EncryptedSecret = append([]byte(nil), params.EncryptedSecret...)
	}
	if params.AgentID != "" {
		current.AgentID = params.AgentID
	}
	if params.ReplaceProxy {
		current.ProxyTargetID = strings.TrimSpace(params.ProxyTargetID)
	} else if params.ProxyTargetID != "" {
		current.ProxyTargetID = strings.TrimSpace(params.ProxyTargetID)
	}
	if params.ReplaceTags {
		current.Tags = normalizeTags(params.Tags)
	}
	current.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SSHTarget{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE ssh_targets
		SET name = ?, alias = ?, host = ?, port = ?, remote_username = ?, auth_type = ?,
			encrypted_secret = ?, agent_id = ?, proxy_target_id = ?, updated_at = ?
		WHERE id = ?
	`, current.Name, current.Alias, current.Host, current.Port, current.RemoteUsername, current.AuthType,
		nullableBytes(current.EncryptedSecret), nullableString(current.AgentID), nullableString(current.ProxyTargetID), formatTime(current.UpdatedAt), current.ID); err != nil {
		return SSHTarget{}, err
	}
	if params.ReplaceTags {
		if err := r.replaceTargetTagsTx(ctx, tx, current, current.UpdatedAt); err != nil {
			return SSHTarget{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SSHTarget{}, err
	}
	return r.GetSSHTarget(ctx, current.ID)
}

func (r *Repository) replaceTargetTagsTx(ctx context.Context, tx *sql.Tx, target SSHTarget, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM target_tag_bindings WHERE target_id = ?`, target.ID); err != nil {
		return err
	}
	for _, tagName := range normalizeTags(target.Tags) {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO target_tags (id, owner_type, owner_id, name, color, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), target.OwnerType, target.OwnerID, tagName, randomTargetTagColor(tagName), formatTime(now)); err != nil {
			return err
		}
		var tagID string
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM target_tags
			WHERE owner_type = ? AND owner_id = ? AND name = ?
		`, target.OwnerType, target.OwnerID, tagName).Scan(&tagID); err != nil {
			return wrapScanErr(err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO target_tag_bindings (target_id, tag_id, created_at)
			VALUES (?, ?, ?)
		`, target.ID, tagID, formatTime(now)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) loadTargetTags(ctx context.Context, targets []SSHTarget) error {
	for i := range targets {
		tags, colors, err := r.listTargetTags(ctx, targets[i].ID)
		if err != nil {
			return err
		}
		targets[i].Tags = tags
		targets[i].TagColors = colors
	}
	return nil
}

func (r *Repository) listTargetTags(ctx context.Context, targetID string) ([]string, map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tag.name, COALESCE(tag.color, '')
		FROM target_tags tag
		JOIN target_tag_bindings binding ON binding.tag_id = tag.id
		WHERE binding.target_id = ?
		ORDER BY tag.name ASC
	`, targetID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var tags []string
	colors := map[string]string{}
	for rows.Next() {
		var tag, color string
		if err := rows.Scan(&tag, &color); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(color) == "" {
			color = fallbackTargetTagColor(tag)
		}
		tags = append(tags, tag)
		colors[tag] = color
	}
	return tags, colors, rows.Err()
}

func (r *Repository) UpdateTargetTagColor(ctx context.Context, ownerType, ownerID, tagName, color string) error {
	tagName = strings.TrimSpace(tagName)
	if tagName == "" {
		return errors.New("tag name is required")
	}
	color, err := normalizeTargetTagColor(color)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE target_tags
		SET color = ?
		WHERE owner_type = ? AND owner_id = ? AND name = ?
	`, color, ownerType, ownerID, tagName)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
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

func (r *Repository) GetAgentEnrollmentByTokenHash(ctx context.Context, tokenHash []byte) (AgentEnrollment, error) {
	var enrollment AgentEnrollment
	var created, expires string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, token_hash, label, default_host, default_port,
			created_by, created_at, expires_at, COALESCE(consumed_agent_id, '')
		FROM agent_enrollments
		WHERE token_hash = ?
	`, tokenHash).Scan(&enrollment.ID, &enrollment.OwnerType, &enrollment.OwnerID, &enrollment.TokenHash, &enrollment.Label,
		&enrollment.DefaultHost, &enrollment.DefaultPort, &enrollment.CreatedBy, &created, &expires, &enrollment.ConsumedAgentID)
	if err != nil {
		return AgentEnrollment{}, wrapScanErr(err)
	}
	enrollment.CreatedAt = parseTime(created)
	enrollment.ExpiresAt = parseTime(expires)
	return enrollment, nil
}

func (r *Repository) UpsertAgent(ctx context.Context, params UpsertAgentParams) (Agent, error) {
	now := time.Now().UTC()
	if existing, err := r.GetAgentByEnrollment(ctx, params.EnrollmentID); err == nil {
		_, err := r.db.ExecContext(ctx, `
			UPDATE agents SET current_runtime_id = ?, last_seen_at = ?
			WHERE id = ?
		`, params.CurrentRuntimeID, formatTime(now), existing.ID)
		if err != nil {
			return Agent{}, err
		}
		existing.CurrentRuntimeID = params.CurrentRuntimeID
		existing.LastSeenAt = now
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Agent{}, err
	}
	agent := Agent{
		ID:               uuid.NewString(),
		OwnerType:        params.OwnerType,
		OwnerID:          params.OwnerID,
		EnrollmentID:     params.EnrollmentID,
		Label:            params.Label,
		CurrentRuntimeID: params.CurrentRuntimeID,
		LastSeenAt:       now,
		CreatedAt:        now,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agents (id, owner_type, owner_id, enrollment_id, label, current_runtime_id, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, agent.ID, agent.OwnerType, agent.OwnerID, agent.EnrollmentID, agent.Label, agent.CurrentRuntimeID, formatTime(agent.LastSeenAt), formatTime(agent.CreatedAt)); err != nil {
		return Agent{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_enrollments SET consumed_agent_id = ? WHERE id = ?
	`, agent.ID, agent.EnrollmentID); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (r *Repository) GetAgentByEnrollment(ctx context.Context, enrollmentID string) (Agent, error) {
	var agent Agent
	var lastSeen, created string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, COALESCE(enrollment_id, ''), label, current_runtime_id, last_seen_at, created_at
		FROM agents
		WHERE enrollment_id = ?
	`, enrollmentID).Scan(&agent.ID, &agent.OwnerType, &agent.OwnerID, &agent.EnrollmentID, &agent.Label, &agent.CurrentRuntimeID, &lastSeen, &created)
	if err != nil {
		return Agent{}, wrapScanErr(err)
	}
	agent.LastSeenAt = parseTime(lastSeen)
	agent.CreatedAt = parseTime(created)
	return agent, nil
}

func (r *Repository) CreateLLMPolicyConfig(ctx context.Context, params CreateLLMPolicyConfigParams) (LLMPolicyConfig, error) {
	cfg := LLMPolicyConfig{
		ID:              uuid.NewString(),
		OwnerType:       params.OwnerType,
		OwnerID:         params.OwnerID,
		Name:            strings.TrimSpace(params.Name),
		BaseURL:         strings.TrimRight(strings.TrimSpace(params.BaseURL), "/"),
		EncryptedAPIKey: append([]byte(nil), params.EncryptedAPIKey...),
		Model:           strings.TrimSpace(params.Model),
		TimeoutSeconds:  params.TimeoutSeconds,
		CreatedAt:       time.Now().UTC(),
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO llm_policy_configs (
			id, owner_type, owner_id, name, base_url, api_key_encrypted, model,
			timeout_seconds, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, cfg.ID, cfg.OwnerType, cfg.OwnerID, cfg.Name, cfg.BaseURL, nullableBytes(cfg.EncryptedAPIKey),
		cfg.Model, cfg.TimeoutSeconds, formatTime(cfg.CreatedAt))
	if err != nil {
		return LLMPolicyConfig{}, err
	}
	return cfg, nil
}

func (r *Repository) GetLLMPolicyConfig(ctx context.Context, id string) (LLMPolicyConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, name, base_url, api_key_encrypted, model,
			timeout_seconds, created_at
		FROM llm_policy_configs
		WHERE id = ?
	`, id)
	return scanLLMPolicyConfig(row)
}

func (r *Repository) ListLLMPolicyConfigs(ctx context.Context, ownerType, ownerID string) ([]LLMPolicyConfig, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_type, owner_id, name, base_url, api_key_encrypted, model,
			timeout_seconds, created_at
		FROM llm_policy_configs
		WHERE owner_type = ? AND owner_id = ?
		ORDER BY created_at ASC
	`, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []LLMPolicyConfig
	for rows.Next() {
		cfg, err := scanLLMPolicyConfigRows(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

func (r *Repository) UpdateLLMPolicyConfig(ctx context.Context, id string, params UpdateLLMPolicyConfigParams) (LLMPolicyConfig, error) {
	timeout := params.TimeoutSeconds
	if timeout <= 0 {
		timeout = 10
	}
	apiKey := nullableBytes(params.EncryptedAPIKey)
	res, err := r.db.ExecContext(ctx, `
		UPDATE llm_policy_configs
		SET name = ?, base_url = ?, api_key_encrypted = COALESCE(?, api_key_encrypted),
			model = ?, timeout_seconds = ?
		WHERE id = ?
	`, strings.TrimSpace(params.Name), strings.TrimRight(strings.TrimSpace(params.BaseURL), "/"), apiKey,
		strings.TrimSpace(params.Model), timeout, id)
	if err != nil {
		return LLMPolicyConfig{}, err
	}
	if err := requireRowsAffected(res); err != nil {
		return LLMPolicyConfig{}, err
	}
	return r.GetLLMPolicyConfig(ctx, id)
}

func (r *Repository) DeleteLLMPolicyConfig(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE command_policies SET llm_config_id = NULL WHERE llm_config_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM llm_policy_configs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CreateLLMPromptResource(ctx context.Context, params CreateLLMPromptResourceParams) (LLMPromptResource, error) {
	prompt := LLMPromptResource{
		ID:         uuid.NewString(),
		OwnerType:  params.OwnerType,
		OwnerID:    params.OwnerID,
		Title:      strings.TrimSpace(params.Title),
		Content:    strings.TrimSpace(params.Content),
		IsDefault:  params.IsDefault,
		IsReadonly: params.IsReadonly,
		CreatedAt:  time.Now().UTC(),
	}
	if prompt.Title == "" {
		prompt.Title = DefaultLLMPromptTitle
	}
	if prompt.Content == "" {
		prompt.Content = DefaultLLMPromptContent
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO llm_prompt_resources (
			id, owner_type, owner_id, title, content, is_default, is_readonly, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, prompt.ID, prompt.OwnerType, prompt.OwnerID, prompt.Title, prompt.Content,
		boolInt(prompt.IsDefault), boolInt(prompt.IsReadonly), formatTime(prompt.CreatedAt))
	if err != nil {
		return LLMPromptResource{}, err
	}
	return prompt, nil
}

func (r *Repository) GetLLMPromptResource(ctx context.Context, id string) (LLMPromptResource, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, title, content, is_default, is_readonly, created_at
		FROM llm_prompt_resources
		WHERE id = ?
	`, id)
	return scanLLMPromptResource(row)
}

func (r *Repository) ListLLMPromptResources(ctx context.Context, ownerType, ownerID string) ([]LLMPromptResource, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_type, owner_id, title, content, is_default, is_readonly, created_at
		FROM llm_prompt_resources
		WHERE owner_type = ? AND owner_id = ?
		ORDER BY is_default DESC, created_at ASC
	`, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prompts []LLMPromptResource
	for rows.Next() {
		prompt, err := scanLLMPromptResourceRows(rows)
		if err != nil {
			return nil, err
		}
		prompts = append(prompts, prompt)
	}
	return prompts, rows.Err()
}

func (r *Repository) UpdateLLMPromptResource(ctx context.Context, id string, params UpdateLLMPromptResourceParams) (LLMPromptResource, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE llm_prompt_resources
		SET title = ?, content = ?
		WHERE id = ? AND is_readonly = 0
	`, strings.TrimSpace(params.Title), strings.TrimSpace(params.Content), id)
	if err != nil {
		return LLMPromptResource{}, err
	}
	if err := requireRowsAffected(res); err != nil {
		return LLMPromptResource{}, err
	}
	return r.GetLLMPromptResource(ctx, id)
}

func (r *Repository) DeleteLLMPromptResource(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE command_policies SET llm_prompt_id = NULL WHERE llm_prompt_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM llm_prompt_resources WHERE id = ? AND is_readonly = 0`, id)
	if err != nil {
		return err
	}
	if err := requireRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CreateCommandPolicy(ctx context.Context, params CreateCommandPolicyParams) (CommandPolicy, error) {
	policy := CommandPolicy{
		ID:                         uuid.NewString(),
		OwnerType:                  params.OwnerType,
		OwnerID:                    params.OwnerID,
		Name:                       strings.TrimSpace(params.Name),
		DefaultAction:              normalizePolicyAction(params.DefaultAction),
		LLMConfigID:                strings.TrimSpace(params.LLMConfigID),
		LLMPromptID:                strings.TrimSpace(params.LLMPromptID),
		IPAllowlist:                strings.TrimSpace(params.IPAllowlist),
		AllowPortForward:           params.AllowPortForward,
		AllowUpload:                params.AllowUpload,
		AllowDownload:              params.AllowDownload,
		AllowInteractive:           params.AllowInteractive,
		AllowManualReview:          params.AllowManualReview,
		ManualReviewTimeoutSeconds: normalizeManualReviewTimeoutSeconds(params.ManualReviewTimeoutSeconds),
		CreatedAt:                  time.Now().UTC(),
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO command_policies (
			id, owner_type, owner_id, name, default_action, llm_config_id, llm_prompt_id,
			ip_allowlist, allow_port_forward, allow_upload, allow_download, allow_interactive,
			allow_manual_review, manual_review_timeout_seconds, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, policy.ID, policy.OwnerType, policy.OwnerID, policy.Name, policy.DefaultAction,
		nullableString(policy.LLMConfigID), nullableString(policy.LLMPromptID), policy.IPAllowlist,
		boolInt(policy.AllowPortForward), boolInt(policy.AllowUpload), boolInt(policy.AllowDownload),
		boolInt(policy.AllowInteractive), boolInt(policy.AllowManualReview), policy.ManualReviewTimeoutSeconds,
		formatTime(policy.CreatedAt))
	if err != nil {
		return CommandPolicy{}, err
	}
	return policy, nil
}

func (r *Repository) ListCommandPolicies(ctx context.Context, ownerType, ownerID string) ([]CommandPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_type, owner_id, name, default_action, COALESCE(llm_config_id, ''),
			COALESCE(llm_prompt_id, ''), ip_allowlist, allow_port_forward, allow_upload,
			allow_download, allow_interactive, allow_manual_review, manual_review_timeout_seconds, created_at
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
		policy, err := scanCommandPolicyRows(rows)
		if err != nil {
			return nil, err
		}
		rules, err := r.listPolicyRules(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.Rules = rules
		targetIDs, err := r.listPolicyTargetIDs(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.TargetIDs = targetIDs
		userGroupIDs, err := r.listPolicyUserGroupIDs(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.UserGroupIDs = userGroupIDs
		targetTags, err := r.listPolicyTargetTags(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.TargetTags = targetTags
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *Repository) GetCommandPolicy(ctx context.Context, id string) (CommandPolicy, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, name, default_action, COALESCE(llm_config_id, ''),
			COALESCE(llm_prompt_id, ''), ip_allowlist, allow_port_forward, allow_upload,
			allow_download, allow_interactive, allow_manual_review, manual_review_timeout_seconds, created_at
		FROM command_policies
		WHERE id = ?
	`, id)
	policy, err := scanCommandPolicyRows(row)
	if err != nil {
		return CommandPolicy{}, wrapScanErr(err)
	}
	policy.Rules, err = r.listPolicyRules(ctx, policy.ID)
	if err != nil {
		return CommandPolicy{}, err
	}
	policy.TargetIDs, err = r.listPolicyTargetIDs(ctx, policy.ID)
	if err != nil {
		return CommandPolicy{}, err
	}
	policy.UserGroupIDs, err = r.listPolicyUserGroupIDs(ctx, policy.ID)
	if err != nil {
		return CommandPolicy{}, err
	}
	policy.TargetTags, err = r.listPolicyTargetTags(ctx, policy.ID)
	if err != nil {
		return CommandPolicy{}, err
	}
	return policy, nil
}

func (r *Repository) UpdateCommandPolicy(ctx context.Context, id string, params UpdateCommandPolicyParams) (CommandPolicy, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE command_policies
		SET name = ?, default_action = ?, llm_config_id = ?, llm_prompt_id = ?,
			ip_allowlist = ?, allow_port_forward = ?, allow_upload = ?,
			allow_download = ?, allow_interactive = ?, allow_manual_review = ?,
			manual_review_timeout_seconds = ?
		WHERE id = ?
	`, strings.TrimSpace(params.Name), normalizePolicyAction(params.DefaultAction),
		nullableString(params.LLMConfigID), nullableString(params.LLMPromptID),
		strings.TrimSpace(params.IPAllowlist), boolInt(params.AllowPortForward),
		boolInt(params.AllowUpload), boolInt(params.AllowDownload), boolInt(params.AllowInteractive),
		boolInt(params.AllowManualReview), normalizeManualReviewTimeoutSeconds(params.ManualReviewTimeoutSeconds), id)
	if err != nil {
		return CommandPolicy{}, err
	}
	if err := requireRowsAffected(res); err != nil {
		return CommandPolicy{}, err
	}
	return r.GetCommandPolicy(ctx, id)
}

func (r *Repository) DeleteCommandPolicy(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM command_policies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) CopyCommandPolicy(ctx context.Context, id string, name string) (CommandPolicy, error) {
	source, err := r.GetCommandPolicy(ctx, id)
	if err != nil {
		return CommandPolicy{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = source.Name + " Copy"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandPolicy{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	copy := CommandPolicy{
		ID:                         uuid.NewString(),
		OwnerType:                  source.OwnerType,
		OwnerID:                    source.OwnerID,
		Name:                       strings.TrimSpace(name),
		DefaultAction:              source.DefaultAction,
		LLMConfigID:                source.LLMConfigID,
		LLMPromptID:                source.LLMPromptID,
		IPAllowlist:                source.IPAllowlist,
		AllowPortForward:           source.AllowPortForward,
		AllowUpload:                source.AllowUpload,
		AllowDownload:              source.AllowDownload,
		AllowInteractive:           source.AllowInteractive,
		AllowManualReview:          source.AllowManualReview,
		ManualReviewTimeoutSeconds: normalizeManualReviewTimeoutSeconds(source.ManualReviewTimeoutSeconds),
		CreatedAt:                  now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO command_policies (
			id, owner_type, owner_id, name, default_action, llm_config_id, llm_prompt_id,
			ip_allowlist, allow_port_forward, allow_upload, allow_download, allow_interactive,
			allow_manual_review, manual_review_timeout_seconds, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, copy.ID, copy.OwnerType, copy.OwnerID, copy.Name, copy.DefaultAction,
		nullableString(copy.LLMConfigID), nullableString(copy.LLMPromptID), copy.IPAllowlist,
		boolInt(copy.AllowPortForward), boolInt(copy.AllowUpload), boolInt(copy.AllowDownload),
		boolInt(copy.AllowInteractive), boolInt(copy.AllowManualReview), copy.ManualReviewTimeoutSeconds,
		formatTime(copy.CreatedAt)); err != nil {
		return CommandPolicy{}, err
	}
	for _, rule := range source.Rules {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO policy_rules (id, policy_id, rule_type, pattern_type, pattern, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), copy.ID, rule.RuleType, rule.PatternType, rule.Pattern, formatTime(now)); err != nil {
			return CommandPolicy{}, err
		}
	}
	if err := copyPolicyRelation(ctx, tx, "policy_targets", "target_id", source.ID, copy.ID); err != nil {
		return CommandPolicy{}, err
	}
	if err := copyPolicyRelation(ctx, tx, "policy_target_tags", "tag_id", source.ID, copy.ID); err != nil {
		return CommandPolicy{}, err
	}
	if err := copyPolicyRelation(ctx, tx, "policy_user_groups", "group_id", source.ID, copy.ID); err != nil {
		return CommandPolicy{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandPolicy{}, err
	}
	return r.GetCommandPolicy(ctx, copy.ID)
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

func (r *Repository) DeletePolicyRule(ctx context.Context, policyID, ruleID string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM policy_rules WHERE policy_id = ? AND id = ?
	`, policyID, ruleID)
	if err != nil {
		return err
	}
	return requireRowsAffected(res)
}

func (r *Repository) AttachPolicyToTarget(ctx context.Context, policyID, targetID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO policy_targets (policy_id, target_id)
		VALUES (?, ?)
	`, policyID, targetID)
	return err
}

func (r *Repository) DetachPolicyFromTarget(ctx context.Context, policyID, targetID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM policy_targets WHERE policy_id = ? AND target_id = ?
	`, policyID, targetID)
	return err
}

func (r *Repository) AttachPolicyToTargetTag(ctx context.Context, policyID, ownerType, ownerID, tagName string) error {
	tagName = strings.TrimSpace(tagName)
	if tagName == "" {
		return errors.New("tag name is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO target_tags (id, owner_type, owner_id, name, color, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), ownerType, ownerID, tagName, randomTargetTagColor(tagName), formatTime(time.Now().UTC())); err != nil {
		return err
	}
	var tagID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM target_tags WHERE owner_type = ? AND owner_id = ? AND name = ?
	`, ownerType, ownerID, tagName).Scan(&tagID); err != nil {
		return wrapScanErr(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO policy_target_tags (policy_id, tag_id)
		VALUES (?, ?)
	`, policyID, tagID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) DetachPolicyFromTargetTag(ctx context.Context, policyID, ownerType, ownerID, tagName string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM policy_target_tags
		WHERE policy_id = ? AND tag_id IN (
			SELECT id FROM target_tags WHERE owner_type = ? AND owner_id = ? AND name = ?
		)
	`, policyID, ownerType, ownerID, strings.TrimSpace(tagName))
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
		SELECT p.id, p.owner_type, p.owner_id, p.name, p.default_action,
			COALESCE(p.llm_config_id, ''), COALESCE(p.llm_prompt_id, ''),
			p.ip_allowlist, p.allow_port_forward, p.allow_upload, p.allow_download,
			p.allow_interactive, p.allow_manual_review, p.manual_review_timeout_seconds, p.created_at
		FROM command_policies p
		WHERE EXISTS (
			SELECT 1 FROM policy_targets pt
			WHERE pt.policy_id = p.id AND pt.target_id = ?
		) OR EXISTS (
			SELECT 1
			FROM policy_target_tags ptt
			JOIN target_tag_bindings ttb ON ttb.tag_id = ptt.tag_id
			WHERE ptt.policy_id = p.id AND ttb.target_id = ?
		)
		ORDER BY p.created_at ASC
	`, targetID, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []CommandPolicy
	for rows.Next() {
		policy, err := scanCommandPolicyRows(rows)
		if err != nil {
			return nil, err
		}
		rules, err := r.listPolicyRules(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.Rules = rules
		targetIDs, err := r.listPolicyTargetIDs(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.TargetIDs = targetIDs
		userGroupIDs, err := r.listPolicyUserGroupIDs(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.UserGroupIDs = userGroupIDs
		targetTags, err := r.listPolicyTargetTags(ctx, policy.ID)
		if err != nil {
			return nil, err
		}
		policy.TargetTags = targetTags
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *Repository) listPolicyTargetIDs(ctx context.Context, policyID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT target_id FROM policy_targets
		WHERE policy_id = ?
		ORDER BY target_id ASC
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

func (r *Repository) listPolicyTargetTags(ctx context.Context, policyID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tag.name
		FROM target_tags tag
		JOIN policy_target_tags binding ON binding.tag_id = tag.id
		WHERE binding.policy_id = ?
		ORDER BY tag.name ASC
	`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
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

func scanCommandPolicyRows(row targetScanner) (CommandPolicy, error) {
	var policy CommandPolicy
	var created string
	var allowPortForward, allowUpload, allowDownload, allowInteractive, allowManualReview int
	err := row.Scan(&policy.ID, &policy.OwnerType, &policy.OwnerID, &policy.Name, &policy.DefaultAction,
		&policy.LLMConfigID, &policy.LLMPromptID, &policy.IPAllowlist, &allowPortForward,
		&allowUpload, &allowDownload, &allowInteractive, &allowManualReview, &policy.ManualReviewTimeoutSeconds, &created)
	if err != nil {
		return CommandPolicy{}, err
	}
	policy.AllowPortForward = allowPortForward == 1
	policy.AllowUpload = allowUpload == 1
	policy.AllowDownload = allowDownload == 1
	policy.AllowInteractive = allowInteractive == 1
	policy.AllowManualReview = allowManualReview == 1
	policy.ManualReviewTimeoutSeconds = normalizeManualReviewTimeoutSeconds(policy.ManualReviewTimeoutSeconds)
	policy.CreatedAt = parseTime(created)
	return policy, nil
}

func copyPolicyRelation(ctx context.Context, tx *sql.Tx, table, valueColumn, sourceID, copyID string) error {
	switch table + "." + valueColumn {
	case "policy_targets.target_id", "policy_target_tags.tag_id", "policy_user_groups.group_id":
	default:
		return fmt.Errorf("unsupported policy relation %s.%s", table, valueColumn)
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT OR IGNORE INTO %s (policy_id, %s)
		SELECT ?, %s FROM %s WHERE policy_id = ?
	`, table, valueColumn, valueColumn, table), copyID, sourceID)
	return err
}

func normalizePolicyAction(action string) string {
	if strings.TrimSpace(action) == DecisionDeny {
		return DecisionDeny
	}
	return DecisionAllow
}

func normalizeManualReviewTimeoutSeconds(seconds int) int {
	if seconds <= 0 {
		return DefaultManualReviewTimeoutSeconds
	}
	return seconds
}

func (r *Repository) CreateCommandAuditLog(ctx context.Context, params CreateCommandAuditLogParams) (CommandAuditLog, error) {
	started := params.StartedAt
	if started.IsZero() {
		started = time.Now().UTC()
	}
	log := CommandAuditLog{
		ID:                   uuid.NewString(),
		UserID:               params.UserID,
		TargetID:             params.TargetID,
		OrganizationID:       params.OrganizationID,
		PublicKeyFingerprint: strings.TrimSpace(params.PublicKeyFingerprint),
		SessionID:            params.SessionID,
		Command:              params.Command,
		RequestType:          params.RequestType,
		PolicyDecision:       params.PolicyDecision,
		PolicyReason:         params.PolicyReason,
		ExitCode:             params.ExitCode,
		StartedAt:            started.UTC(),
		EndedAt:              params.EndedAt,
		RemoteAddress:        params.RemoteAddress,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO command_audit_logs (
			id, user_id, target_id, organization_id, session_id, command, request_type,
			policy_decision, policy_reason, public_key_fingerprint, exit_code, started_at, ended_at, remote_address
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.UserID, log.TargetID, nullableString(log.OrganizationID), log.SessionID, log.Command, log.RequestType,
		log.PolicyDecision, log.PolicyReason, log.PublicKeyFingerprint, nullableInt(log.ExitCode), formatTime(log.StartedAt), nullableTime(log.EndedAt), log.RemoteAddress)
	if err != nil {
		return CommandAuditLog{}, err
	}
	return log, nil
}

func (r *Repository) ListCommandAuditLogs(ctx context.Context, filter AuditLogFilter) ([]CommandAuditLog, error) {
	query := `
		SELECT log.id, log.user_id, COALESCE(u.email, ''), COALESCE(u.display_name, ''),
			log.target_id, COALESCE(target.name, ''), COALESCE(target.alias, ''), COALESCE(target.host, ''),
			COALESCE(target.port, 0), COALESCE(target.remote_username, ''),
			COALESCE(log.organization_id, ''), log.session_id, log.command, log.request_type,
			log.policy_decision, log.policy_reason, COALESCE(log.public_key_fingerprint, ''),
			COALESCE(key.name, ''), log.exit_code, log.started_at, log.ended_at, log.remote_address
		FROM command_audit_logs log
		LEFT JOIN users u ON u.id = log.user_id
		LEFT JOIN ssh_targets target ON target.id = log.target_id
		LEFT JOIN user_public_keys key ON key.fingerprint = log.public_key_fingerprint
		WHERE 1 = 1`
	args := []any{}
	if filter.UserID != "" {
		query += ` AND log.user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.TargetID != "" {
		query += ` AND log.target_id = ?`
		args = append(args, filter.TargetID)
	}
	query += ` ORDER BY log.started_at DESC`

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
		if err := rows.Scan(&log.ID, &log.UserID, &log.UserEmail, &log.UserDisplayName,
			&log.TargetID, &log.TargetName, &log.TargetAlias, &log.TargetHost, &log.TargetPort, &log.TargetUsername,
			&log.OrganizationID, &log.SessionID, &log.Command, &log.RequestType, &log.PolicyDecision,
			&log.PolicyReason, &log.PublicKeyFingerprint, &log.PublicKeyName, &exit, &started, &ended, &log.RemoteAddress); err != nil {
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
	user, err := scanUserRows(row)
	if err != nil {
		return User{}, wrapScanErr(err)
	}
	return user, nil
}

func scanUserRows(row targetScanner) (User, error) {
	var user User
	var created string
	var isSystemAdmin int
	var disabled sql.NullString
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash,
		&isSystemAdmin, &user.AuthProvider, &disabled, &created)
	if err != nil {
		return User{}, err
	}
	user.IsSystemAdmin = isSystemAdmin == 1
	if strings.TrimSpace(user.AuthProvider) == "" {
		user.AuthProvider = defaultAuthProvider
	}
	if disabled.Valid {
		v := parseTime(disabled.String)
		user.DisabledAt = &v
	}
	user.CreatedAt = parseTime(created)
	return user, nil
}

func scanExternalIdentity(row targetScanner) (ExternalIdentity, error) {
	var identity ExternalIdentity
	var created, updated string
	err := row.Scan(&identity.ID, &identity.UserID, &identity.Provider, &identity.Subject,
		&identity.Email, &identity.DisplayName, &identity.RawProfileJSON, &created, &updated)
	if err != nil {
		return ExternalIdentity{}, wrapScanErr(err)
	}
	identity.CreatedAt = parseTime(created)
	identity.UpdatedAt = parseTime(updated)
	return identity, nil
}

func scanMCPTokenRows(row targetScanner) (MCPToken, error) {
	var token MCPToken
	var lastUsed sql.NullString
	var toolGroups string
	var created string
	err := row.Scan(&token.ID, &token.UserID, &token.Name, &token.TokenHash, &toolGroups, &lastUsed, &created)
	if err != nil {
		return MCPToken{}, err
	}
	token.ToolGroups = decodeMCPToolGroups(toolGroups)
	if lastUsed.Valid {
		v := parseTime(lastUsed.String)
		token.LastUsedAt = &v
	}
	token.CreatedAt = parseTime(created)
	return token, nil
}

func normalizeMCPToolGroups(groups []string) []string {
	allowed := map[string]bool{
		"session": true,
		"auth":    true,
		"member":  true,
		"target":  true,
		"policy":  true,
		"audit":   true,
	}
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		group = strings.TrimSpace(strings.ToLower(group))
		if allowed[group] && !seen[group] {
			seen[group] = true
			out = append(out, group)
		}
	}
	if len(out) == 0 {
		return []string{"session"}
	}
	return out
}

func encodeMCPToolGroups(groups []string) string {
	return strings.Join(normalizeMCPToolGroups(groups), ",")
}

func decodeMCPToolGroups(raw string) []string {
	return normalizeMCPToolGroups(strings.Split(raw, ","))
}

func scanTarget(row *sql.Row) (SSHTarget, error) {
	var target SSHTarget
	var created, updated string
	err := row.Scan(&target.ID, &target.OwnerType, &target.OwnerID, &target.Name, &target.Alias, &target.TargetType,
		&target.Host, &target.Port, &target.RemoteUsername, &target.AuthType, &target.EncryptedSecret,
		&target.AgentID, &target.ProxyTargetID, &target.CreatedBy, &created, &updated)
	if err != nil {
		return SSHTarget{}, wrapScanErr(err)
	}
	if strings.TrimSpace(target.Name) == "" {
		target.Name = target.Alias
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
	err := row.Scan(&target.ID, &target.OwnerType, &target.OwnerID, &target.Name, &target.Alias, &target.TargetType,
		&target.Host, &target.Port, &target.RemoteUsername, &target.AuthType, &target.EncryptedSecret,
		&target.AgentID, &target.ProxyTargetID, &target.CreatedBy, &created, &updated)
	if err != nil {
		return SSHTarget{}, wrapScanErr(err)
	}
	if strings.TrimSpace(target.Name) == "" {
		target.Name = target.Alias
	}
	target.CreatedAt = parseTime(created)
	target.UpdatedAt = parseTime(updated)
	return target, nil
}

func scanLLMPolicyConfig(row *sql.Row) (LLMPolicyConfig, error) {
	cfg, err := scanLLMPolicyConfigRows(row)
	if err != nil {
		return LLMPolicyConfig{}, wrapScanErr(err)
	}
	return cfg, nil
}

func scanLLMPolicyConfigRows(row targetScanner) (LLMPolicyConfig, error) {
	var cfg LLMPolicyConfig
	var apiKey []byte
	var created string
	err := row.Scan(&cfg.ID, &cfg.OwnerType, &cfg.OwnerID, &cfg.Name, &cfg.BaseURL,
		&apiKey, &cfg.Model, &cfg.TimeoutSeconds, &created)
	if err != nil {
		return LLMPolicyConfig{}, err
	}
	cfg.EncryptedAPIKey = append([]byte(nil), apiKey...)
	cfg.CreatedAt = parseTime(created)
	return cfg, nil
}

func scanLLMPromptResource(row *sql.Row) (LLMPromptResource, error) {
	prompt, err := scanLLMPromptResourceRows(row)
	if err != nil {
		return LLMPromptResource{}, wrapScanErr(err)
	}
	return prompt, nil
}

func scanLLMPromptResourceRows(row targetScanner) (LLMPromptResource, error) {
	var prompt LLMPromptResource
	var isDefault, isReadonly int
	var created string
	err := row.Scan(&prompt.ID, &prompt.OwnerType, &prompt.OwnerID, &prompt.Title, &prompt.Content,
		&isDefault, &isReadonly, &created)
	if err != nil {
		return LLMPromptResource{}, err
	}
	prompt.IsDefault = isDefault == 1
	prompt.IsReadonly = isReadonly == 1
	prompt.CreatedAt = parseTime(created)
	return prompt, nil
}

func wrapScanErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
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

func normalizeAuthProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return defaultAuthProvider
	}
	return provider
}

func requireRowsAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func personalOrganizationName(user User) string {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Email
	}
	return name + " Personal"
}

func personalOrganizationSlug(user User) string {
	local := user.Email
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	var b strings.Builder
	for _, ch := range strings.ToLower(local) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "user"
	}
	return "personal-" + slug + "-" + strings.ReplaceAll(user.ID[:8], "-", "")
}

func normalizeTargetName(name, alias string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(alias)
	}
	return name
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func (r *Repository) String() string {
	return fmt.Sprintf("Repository{%p}", r.db)
}
