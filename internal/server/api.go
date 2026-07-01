package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
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
	DisabledAt    string `json:"disabled_at,omitempty"`
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
	Runtime       apiRuntime        `json:"runtime"`
}

type apiRuntime struct {
	SSHHost               string `json:"ssh_host"`
	SSHPort               int    `json:"ssh_port"`
	ClientMode            bool   `json:"client_mode"`
	LocalTerminalTargetID string `json:"local_terminal_target_id,omitempty"`
	AppName               string `json:"app_name"`
	AppDescription        string `json:"app_description"`
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
	mux.HandleFunc("PUT /api/me/password", a.requireUser(a.handleChangeOwnPassword))
	mux.HandleFunc("GET /api/admin/settings", a.requireSystemAdmin(a.handleAdminSettings))
	mux.HandleFunc("PUT /api/admin/settings/branding", a.requireSystemAdmin(a.handleUpdateBrandingSettings))
	mux.HandleFunc("PUT /api/admin/settings/auth", a.requireSystemAdmin(a.handleUpdateAuthSettings))
	mux.HandleFunc("PUT /api/admin/settings/dingtalk", a.requireSystemAdmin(a.handleUpdateDingTalkSettings))
	mux.HandleFunc("PUT /api/admin/settings/ldap", a.requireSystemAdmin(a.handleUpdateLDAPSettings))
	mux.HandleFunc("GET /api/admin/users", a.requireSystemAdmin(a.handleAdminListUsers))
	mux.HandleFunc("PATCH /api/admin/users/{id}", a.requireSystemAdmin(a.handleAdminUpdateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", a.requireSystemAdmin(a.handleAdminDeleteUser))
	mux.HandleFunc("PUT /api/admin/users/{id}/password", a.requireSystemAdmin(a.handleAdminResetUserPassword))
	mux.HandleFunc("GET /api/admin/orgs", a.requireSystemAdmin(a.handleAdminListOrganizations))
	mux.HandleFunc("DELETE /api/admin/orgs/{id}", a.requireSystemAdmin(a.handleAdminDeleteOrganization))
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
	mux.HandleFunc("GET /api/mcp-tokens", a.requireUser(a.handleListMCPTokens))
	mux.HandleFunc("POST /api/mcp-tokens", a.requireUser(a.handleCreateMCPToken))
	mux.HandleFunc("PATCH /api/mcp-tokens/{id}", a.requireUser(a.handleUpdateMCPToken))
	mux.HandleFunc("DELETE /api/mcp-tokens/{id}", a.requireUser(a.handleDeleteMCPToken))
	mux.HandleFunc("GET /api/targets", a.requireUser(a.handleListTargets))
	mux.HandleFunc("POST /api/targets", a.requireUser(a.handleCreateTarget))
	mux.HandleFunc("POST /api/targets/{id}/copy", a.requireUser(a.handleCopyTarget))
	mux.HandleFunc("PATCH /api/targets/{id}", a.requireUser(a.handleUpdateTarget))
	mux.HandleFunc("DELETE /api/targets/{id}", a.requireUser(a.handleDeleteTarget))
	mux.HandleFunc("GET /api/credentials", a.requireUser(a.handleListSSHCredentials))
	mux.HandleFunc("POST /api/credentials", a.requireUser(a.handleCreateSSHCredential))
	mux.HandleFunc("PATCH /api/credentials/{id}", a.requireUser(a.handleUpdateSSHCredential))
	mux.HandleFunc("DELETE /api/credentials/{id}", a.requireUser(a.handleDeleteSSHCredential))
	mux.HandleFunc("GET /api/target-folders", a.requireUser(a.handleListTargetFolders))
	mux.HandleFunc("POST /api/target-folders", a.requireUser(a.handleCreateTargetFolder))
	mux.HandleFunc("PATCH /api/target-folders/{id}", a.requireUser(a.handleUpdateTargetFolder))
	mux.HandleFunc("DELETE /api/target-folders/{id}", a.requireUser(a.handleDeleteTargetFolder))
	mux.HandleFunc("GET /api/batch-command-histories", a.requireUser(a.handleListBatchCommandHistories))
	mux.HandleFunc("POST /api/batch-command-histories", a.requireUser(a.handleRecordBatchCommandHistory))
	mux.HandleFunc("GET /api/me/settings", a.requireUser(a.handleMySettings))
	mux.HandleFunc("PUT /api/me/settings", a.requireUser(a.handleUpdateMySettings))
	mux.HandleFunc("GET /api/targets/{id}/terminal/ws", a.requireUser(a.handleTargetTerminalWS))
	mux.HandleFunc("GET /api/targets/{id}/system", a.requireUser(a.handleTargetSystem))
	mux.HandleFunc("GET /api/targets/{id}/files", a.requireUser(a.handleTargetFiles))
	mux.HandleFunc("GET /api/targets/{id}/files/download", a.requireUser(a.handleTargetFileDownload))
	mux.HandleFunc("POST /api/targets/{id}/files/open", a.requireUser(a.handleTargetFileOpen))
	mux.HandleFunc("POST /api/targets/{id}/files/upload", a.requireUser(a.handleTargetFileUpload))
	mux.HandleFunc("GET /api/targets/{id}/files/stat", a.requireUser(a.handleTargetFileStat))
	mux.HandleFunc("GET /api/targets/{id}/files/read", a.requireUser(a.handleTargetFileRead))
	mux.HandleFunc("POST /api/targets/{id}/files/write", a.requireUser(a.handleTargetFileWrite))
	mux.HandleFunc("POST /api/targets/{id}/files/touch", a.requireUser(a.handleTargetFileTouch))
	mux.HandleFunc("POST /api/targets/{id}/files/mkdir", a.requireUser(a.handleTargetFileMkdir))
	mux.HandleFunc("POST /api/targets/{id}/files/delete", a.requireUser(a.handleTargetFileDelete))
	mux.HandleFunc("POST /api/targets/{id}/files/move", a.requireUser(a.handleTargetFileMove))
	mux.HandleFunc("POST /api/targets/{id}/files/copy", a.requireUser(a.handleTargetFileCopy))
	mux.HandleFunc("PATCH /api/target-tags", a.requireUser(a.handleUpdateTargetTagColor))
	mux.HandleFunc("POST /api/agent-enrollments", a.requireUser(a.handleCreateAgentEnrollment))
	mux.HandleFunc("GET /api/llm-configs", a.requireUser(a.handleListLLMConfigs))
	mux.HandleFunc("POST /api/llm-configs", a.requireUser(a.handleCreateLLMConfig))
	mux.HandleFunc("PATCH /api/llm-configs/{id}", a.requireUser(a.handleUpdateLLMConfig))
	mux.HandleFunc("DELETE /api/llm-configs/{id}", a.requireUser(a.handleDeleteLLMConfig))
	mux.HandleFunc("GET /api/llm-prompts", a.requireUser(a.handleListLLMPrompts))
	mux.HandleFunc("POST /api/llm-prompts", a.requireUser(a.handleCreateLLMPrompt))
	mux.HandleFunc("PATCH /api/llm-prompts/{id}", a.requireUser(a.handleUpdateLLMPrompt))
	mux.HandleFunc("DELETE /api/llm-prompts/{id}", a.requireUser(a.handleDeleteLLMPrompt))
	mux.HandleFunc("GET /api/policies", a.requireUser(a.handleListPolicies))
	mux.HandleFunc("POST /api/policies", a.requireUser(a.handleCreatePolicy))
	mux.HandleFunc("PATCH /api/policies/{id}", a.requireUser(a.handleUpdatePolicy))
	mux.HandleFunc("DELETE /api/policies/{id}", a.requireUser(a.handleDeletePolicy))
	mux.HandleFunc("POST /api/policies/{id}/copy", a.requireUser(a.handleCopyPolicy))
	mux.HandleFunc("POST /api/policies/{id}/rules", a.requireUser(a.handleCreatePolicyRule))
	mux.HandleFunc("DELETE /api/policies/{id}/rules/{rule_id}", a.requireUser(a.handleDeletePolicyRule))
	mux.HandleFunc("POST /api/policies/{id}/targets", a.requireUser(a.handleAttachPolicyTarget))
	mux.HandleFunc("DELETE /api/policies/{id}/targets/{target_id}", a.requireUser(a.handleDetachPolicyTarget))
	mux.HandleFunc("POST /api/policies/{id}/target-tags", a.requireUser(a.handleAttachPolicyTargetTag))
	mux.HandleFunc("DELETE /api/policies/{id}/target-tags/{tag}", a.requireUser(a.handleDetachPolicyTargetTag))
	mux.HandleFunc("POST /api/policies/{id}/user-groups", a.requireUser(a.handleAttachPolicyUserGroup))
	mux.HandleFunc("DELETE /api/policies/{id}/user-groups/{group_id}", a.requireUser(a.handleDetachPolicyUserGroup))
	mux.HandleFunc("GET /api/manual-reviews", a.requireUser(a.handleListManualReviews))
	mux.HandleFunc("POST /api/manual-reviews/{id}/decision", a.requireUser(a.handleDecideManualReview))
	mux.HandleFunc("GET /api/audit", a.requireUser(a.handleListAuditLogs))
	mux.HandleFunc("GET /api/audit/{id}/recording", a.requireUser(a.handleAuditRecording))
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
		user, err := a.userForRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r, user)
	}
}

func (a *App) userForRequest(r *http.Request) (store.User, error) {
	if err := a.ensureServices(r.Context()); err != nil {
		return store.User{}, err
	}
	if a.cfg.ClientMode && isLoopbackRequest(r) {
		return a.store.Repository().EnsureClientUser(r.Context())
	}
	cookie, err := r.Cookie(a.sessionCookieName())
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return store.User{}, store.ErrNotFound
	}
	return a.auth.UserForSession(r.Context(), cookie.Value)
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
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

func setSessionCookie(w http.ResponseWriter, r *http.Request, name, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPSRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPSRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func isHTTPSRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func apiUserFromStore(user store.User) apiUser {
	out := apiUser{
		ID:            user.ID,
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		IsSystemAdmin: user.IsSystemAdmin,
		AuthProvider:  user.AuthProvider,
	}
	if user.DisabledAt != nil {
		out.DisabledAt = user.DisabledAt.Format(time.RFC3339)
	}
	return out
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
