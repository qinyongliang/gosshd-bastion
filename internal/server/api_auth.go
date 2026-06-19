package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/auth"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

const (
	settingDingTalk = "dingtalk"
	settingLDAP     = "ldap"
)

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	user, token, err := a.auth.Register(r.Context(), req.Email, req.DisplayName, req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	setSessionCookie(w, a.sessionCookieName(), token)
	writeJSON(w, http.StatusCreated, apiUserResponse{User: apiUserFromStore(user)})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	user, token, err := a.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	setSessionCookie(w, a.sessionCookieName(), token)
	writeJSON(w, http.StatusOK, apiUserResponse{User: apiUserFromStore(user)})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cookie, err := r.Cookie(a.sessionCookieName()); err == nil {
		_ = a.auth.Logout(r.Context(), cookie.Value)
	}
	clearSessionCookie(w, a.sessionCookieName())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := a.dingTalkConfig(r)
	if err != nil && !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dingtalk": map[string]any{
			"enabled": cfg.Enabled,
		},
	})
}

func (a *App) handleDingTalkStart(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := a.dingTalkConfig(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	redirectAfter := r.URL.Query().Get("redirect_after")
	if redirectAfter == "" {
		redirectAfter = "/"
	}
	authURL, err := a.auth.BuildDingTalkAuthURL(r.Context(), cfg, redirectAfter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *App) handleDingTalkCallback(w http.ResponseWriter, r *http.Request) {
	if err := a.ensureServices(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := a.dingTalkConfig(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, token, err := a.auth.CompleteDingTalkLogin(r.Context(), cfg, r.URL.Query().Get("code"), r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	setSessionCookie(w, a.sessionCookieName(), token)
	_ = user
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request, user store.User) {
	orgs, err := a.store.Repository().ListOrganizationsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiMeResponse{User: apiUserFromStore(user), Runtime: a.runtimeInfo(r)}
	for _, org := range orgs {
		apiOrg := apiOrganizationFromStore(org)
		if member, err := a.store.Repository().GetOrganizationMember(r.Context(), org.ID, user.ID); err == nil {
			apiOrg.Role = member.Role
		}
		out.Organizations = append(out.Organizations, apiOrg)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if user.AuthProvider != "local" {
		writeError(w, http.StatusBadRequest, "password changes are only available for local users")
		return
	}
	newPassword := strings.TrimSpace(req.NewPassword)
	if len(newPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if newPassword != strings.TrimSpace(req.ConfirmPassword) {
		writeError(w, http.StatusBadRequest, "password confirmation does not match")
		return
	}
	if err := a.auth.ChangePassword(r.Context(), user, req.CurrentPassword, newPassword); err != nil {
		if err == auth.ErrInvalidCredentials {
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) dingTalkConfig(r *http.Request) (auth.DingTalkConfig, error) {
	setting, err := a.store.Repository().GetSystemSetting(r.Context(), settingDingTalk)
	if err != nil {
		return auth.DingTalkConfig{}, err
	}
	var raw struct {
		Enabled      bool   `json:"enabled"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthURL      string `json:"auth_url"`
		TokenURL     string `json:"token_url"`
		UserInfoURL  string `json:"userinfo_url"`
		RedirectURL  string `json:"redirect_url"`
		DefaultOrgID string `json:"default_org_id"`
		DefaultRole  string `json:"default_role"`
	}
	if err := json.Unmarshal([]byte(setting.ValueJSON), &raw); err != nil {
		return auth.DingTalkConfig{}, err
	}
	redirectURL := strings.TrimSpace(raw.RedirectURL)
	if redirectURL == "" {
		redirectURL = publicBaseURL(r, a.cfg.publicHost()) + "/api/auth/dingtalk/callback"
	}
	return auth.DingTalkConfig{
		Enabled:             raw.Enabled,
		ClientID:            raw.ClientID,
		ClientSecret:        raw.ClientSecret,
		AuthURL:             raw.AuthURL,
		TokenURL:            raw.TokenURL,
		UserInfoURL:         raw.UserInfoURL,
		RedirectURL:         redirectURL,
		DefaultOrganization: raw.DefaultOrgID,
		DefaultRole:         raw.DefaultRole,
	}, nil
}
