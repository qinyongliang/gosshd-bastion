package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	DisplayName   string `json:"display_name"`
	IsSystemAdmin bool   `json:"is_system_admin"`
	AuthProvider  string `json:"auth_provider"`
}

type apiOrganization struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	IsPersonal bool   `json:"is_personal"`
	Role       string `json:"role,omitempty"`
}

type apiPublicKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
}

type apiUserResponse struct {
	User apiUser `json:"user"`
}

type apiMeResponse struct {
	User          apiUser           `json:"user"`
	Organizations []apiOrganization `json:"organizations"`
}

type apiOrganizationResponse struct {
	Organization apiOrganization `json:"organization"`
}

type apiInviteResponse struct {
	Code string `json:"code"`
}

type apiPublicKeyResponse struct {
	Key apiPublicKey `json:"key"`
}

type apiPublicKeysResponse struct {
	Keys []apiPublicKey `json:"keys"`
}

func (a *App) apiRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", a.handleRegister)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/auth/providers", a.handleAuthProviders)
	mux.HandleFunc("GET /api/auth/dingtalk/start", a.handleDingTalkStart)
	mux.HandleFunc("GET /api/auth/dingtalk/callback", a.handleDingTalkCallback)
	mux.HandleFunc("GET /api/me", a.requireUser(a.handleMe))
	mux.HandleFunc("GET /api/admin/settings", a.requireSystemAdmin(a.handleAdminSettings))
	mux.HandleFunc("PUT /api/admin/settings/dingtalk", a.requireSystemAdmin(a.handleUpdateDingTalkSettings))
	mux.HandleFunc("PUT /api/admin/settings/ldap", a.requireSystemAdmin(a.handleUpdateLDAPSettings))
	mux.HandleFunc("GET /api/admin/users", a.requireSystemAdmin(a.handleAdminListUsers))
	mux.HandleFunc("PATCH /api/admin/users/{id}", a.requireSystemAdmin(a.handleAdminUpdateUser))
	mux.HandleFunc("PUT /api/admin/users/{id}/password", a.requireSystemAdmin(a.handleAdminResetUserPassword))
	mux.HandleFunc("GET /api/admin/orgs", a.requireSystemAdmin(a.handleAdminListOrganizations))
	mux.HandleFunc("GET /api/admin/orgs/{id}/members", a.requireSystemAdmin(a.handleAdminListOrganizationMembers))
	mux.HandleFunc("PATCH /api/admin/orgs/{id}/members/{user_id}", a.requireSystemAdmin(a.handleAdminUpdateOrganizationMember))
	mux.HandleFunc("POST /api/admin/orgs/{id}/transfer-owner", a.requireSystemAdmin(a.handleAdminTransferOrganizationOwner))
	mux.HandleFunc("POST /api/orgs", a.requireUser(a.handleCreateOrganization))
	mux.HandleFunc("GET /api/orgs", a.requireUser(a.handleListOrganizations))
	mux.HandleFunc("POST /api/orgs/{id}/invites", a.requireUser(a.handleCreateOrganizationInvite))
	mux.HandleFunc("POST /api/orgs/join", a.requireUser(a.handleJoinOrganization))
	mux.HandleFunc("POST /api/orgs/{id}/leave", a.requireUser(a.handleLeaveOrganization))
	mux.HandleFunc("GET /api/orgs/{id}/members", a.requireUser(a.handleListOrganizationMembers))
	mux.HandleFunc("POST /api/orgs/{id}/members", a.requireUser(a.handleAddOrganizationMember))
	mux.HandleFunc("PATCH /api/orgs/{id}/members/{user_id}", a.requireUser(a.handleUpdateOrganizationMember))
	mux.HandleFunc("DELETE /api/orgs/{id}/members/{user_id}", a.requireUser(a.handleRemoveOrganizationMember))
	mux.HandleFunc("POST /api/orgs/{id}/transfer-owner", a.requireUser(a.handleTransferOrganizationOwner))
	mux.HandleFunc("GET /api/orgs/{id}/groups", a.requireUser(a.handleListOrganizationGroups))
	mux.HandleFunc("POST /api/orgs/{id}/groups", a.requireUser(a.handleCreateOrganizationGroup))
	mux.HandleFunc("POST /api/orgs/{id}/groups/{group_id}/members", a.requireUser(a.handleAddOrganizationGroupMember))
	mux.HandleFunc("DELETE /api/orgs/{id}/groups/{group_id}/members/{user_id}", a.requireUser(a.handleRemoveOrganizationGroupMember))
	mux.HandleFunc("GET /api/keys", a.requireUser(a.handleListPublicKeys))
	mux.HandleFunc("POST /api/keys", a.requireUser(a.handleCreatePublicKey))
	mux.HandleFunc("DELETE /api/keys/{id}", a.requireUser(a.handleDeletePublicKey))
	mux.HandleFunc("GET /api/targets", a.requireUser(a.handleListTargets))
	mux.HandleFunc("POST /api/targets", a.requireUser(a.handleCreateTarget))
	mux.HandleFunc("PATCH /api/targets/{id}", a.requireUser(a.handleUpdateTarget))
	mux.HandleFunc("POST /api/agent-enrollments", a.requireUser(a.handleCreateAgentEnrollment))
	mux.HandleFunc("GET /api/llm-configs", a.requireUser(a.handleListLLMConfigs))
	mux.HandleFunc("POST /api/llm-configs", a.requireUser(a.handleCreateLLMConfig))
	mux.HandleFunc("GET /api/llm-prompts", a.requireUser(a.handleListLLMPrompts))
	mux.HandleFunc("POST /api/llm-prompts", a.requireUser(a.handleCreateLLMPrompt))
	mux.HandleFunc("GET /api/policies", a.requireUser(a.handleListPolicies))
	mux.HandleFunc("POST /api/policies", a.requireUser(a.handleCreatePolicy))
	mux.HandleFunc("POST /api/policies/{id}/rules", a.requireUser(a.handleCreatePolicyRule))
	mux.HandleFunc("POST /api/policies/{id}/targets", a.requireUser(a.handleAttachPolicyTarget))
	mux.HandleFunc("POST /api/policies/{id}/target-tags", a.requireUser(a.handleAttachPolicyTargetTag))
	mux.HandleFunc("POST /api/policies/{id}/user-groups", a.requireUser(a.handleAttachPolicyUserGroup))
	mux.HandleFunc("GET /api/audit", a.requireUser(a.handleListAuditLogs))
	mux.HandleFunc("GET /install/{file}", a.handleInstall)
}

func (a *App) requireSystemAdmin(next func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return a.requireUser(func(w http.ResponseWriter, r *http.Request, user store.User) {
		if !user.IsSystemAdmin {
			writeError(w, http.StatusForbidden, "system admin required")
			return
		}
		next(w, r, user)
	})
}

func (a *App) requireOrganizationAdmin(ctx context.Context, orgID string, user store.User) error {
	if user.IsSystemAdmin {
		return nil
	}
	member, err := a.store.Repository().GetOrganizationMember(ctx, orgID, user.ID)
	if err != nil {
		return err
	}
	if member.Role != store.RoleOwner && member.Role != store.RoleAdmin {
		return errors.New("organization admin required")
	}
	return nil
}

func (a *App) requireOrganizationOwner(ctx context.Context, orgID string, user store.User) error {
	if user.IsSystemAdmin {
		return nil
	}
	member, err := a.store.Repository().GetOrganizationMember(ctx, orgID, user.ID)
	if err != nil {
		return err
	}
	if member.Role != store.RoleOwner {
		return errors.New("organization owner required")
	}
	return nil
}

func (a *App) requireUser(next func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := a.ensureServices(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cookie, err := r.Cookie(a.sessionCookieName())
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		user, err := a.auth.UserForSession(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r, user)
	}
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func setSessionCookie(w http.ResponseWriter, name, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
}

func clearSessionCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func apiUserFromStore(user store.User) apiUser {
	return apiUser{
		ID:            user.ID,
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		IsSystemAdmin: user.IsSystemAdmin,
		AuthProvider:  user.AuthProvider,
	}
}

func apiOrganizationFromStore(org store.Organization) apiOrganization {
	return apiOrganization{ID: org.ID, Name: org.Name, Slug: org.Slug, IsPersonal: org.IsPersonal}
}

func apiPublicKeyFromStore(key store.PublicKey) apiPublicKey {
	return apiPublicKey{
		ID:          key.ID,
		Name:        key.Name,
		Fingerprint: key.Fingerprint,
		CreatedAt:   key.CreatedAt.Format(time.RFC3339),
	}
}

func randomCode() (string, []byte, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(code))
	return code, sum[:], nil
}

func codeHash(code string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return sum[:]
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}

func contextBackground() context.Context {
	return context.Background()
}
