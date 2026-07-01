package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiOrganizationMember struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	Email          string `json:"email"`
	DisplayName    string `json:"display_name"`
	Role           string `json:"role"`
	CreatedAt      string `json:"created_at"`
}

type apiOrganizationMembersResponse struct {
	Members []apiOrganizationMember `json:"members"`
}

func (a *App) handleAdminSettings(w http.ResponseWriter, r *http.Request, user store.User) {
	out := map[string]any{}
	authConfig, err := a.loadAuthSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out[settingAuth] = authConfig
	brandingConfig, err := a.loadBrandingSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out[settingBranding] = brandingConfig
	for _, key := range []string{settingDingTalk, settingLDAP} {
		setting, err := a.store.Repository().GetSystemSetting(r.Context(), key)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		redactSecretFields(value)
		out[key] = value
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleUpdateBrandingSettings(w http.ResponseWriter, r *http.Request, user store.User) {
	var req brandingSettings
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	branding := normalizeBrandingSettings(req)
	if err := validateBrandingIcon(branding.AppIcon); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := json.Marshal(branding)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.Repository().UpsertSystemSetting(r.Context(), settingBranding, payload, user.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.clearBrandingCache()
	writeJSON(w, http.StatusOK, map[string]any{settingBranding: branding})
}

func (a *App) handleUpdateAuthSettings(w http.ResponseWriter, r *http.Request, user store.User) {
	var req authSettings
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.Repository().UpsertSystemSetting(r.Context(), settingAuth, payload, user.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auth": req})
}

func (a *App) handleUpdateDingTalkSettings(w http.ResponseWriter, r *http.Request, user store.User) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.upsertJSONSetting(r, settingDingTalk, req, user.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	redactSecretFields(req)
	writeJSON(w, http.StatusOK, map[string]any{"dingtalk": req})
}

func (a *App) handleUpdateLDAPSettings(w http.ResponseWriter, r *http.Request, user store.User) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.upsertJSONSetting(r, settingLDAP, req, user.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	redactSecretFields(req)
	writeJSON(w, http.StatusOK, map[string]any{"ldap": req})
}

func (a *App) handleAdminListUsers(w http.ResponseWriter, r *http.Request, user store.User) {
	users, err := a.store.Repository().ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := struct {
		Users []apiUser `json:"users"`
	}{}
	for _, item := range users {
		out.Users = append(out.Users, apiUserFromStore(item))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		IsSystemAdmin *bool `json:"is_system_admin"`
		Disabled      *bool `json:"disabled"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	targetID := r.PathValue("id")
	if req.Disabled != nil && *req.Disabled && targetID == user.ID {
		writeError(w, http.StatusBadRequest, "cannot disable current user")
		return
	}
	if req.IsSystemAdmin != nil {
		if err := a.store.Repository().UpdateUserSystemAdmin(r.Context(), targetID, *req.IsSystemAdmin); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Disabled != nil {
		if err := a.store.Repository().UpdateUserDisabled(r.Context(), targetID, *req.Disabled); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request, user store.User) {
	targetID := r.PathValue("id")
	if targetID == user.ID {
		writeError(w, http.StatusBadRequest, "cannot delete current user")
		return
	}
	if err := a.store.Repository().DeleteUser(r.Context(), targetID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (a *App) handleAdminResetUserPassword(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	password := strings.TrimSpace(req.Password)
	if len(password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	target, err := a.store.Repository().GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if target.AuthProvider != "local" {
		writeError(w, http.StatusBadRequest, "password reset is only available for local users")
		return
	}
	if err := a.auth.ResetPassword(r.Context(), target.ID, password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAdminListOrganizations(w http.ResponseWriter, r *http.Request, user store.User) {
	orgs, err := a.store.Repository().ListOrganizations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := struct {
		Organizations []apiOrganization `json:"organizations"`
	}{}
	for _, org := range orgs {
		if org.IsPersonal {
			continue
		}
		out.Organizations = append(out.Organizations, apiOrganizationFromStore(org))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleAdminDeleteOrganization(w http.ResponseWriter, r *http.Request, user store.User) {
	if err := a.store.Repository().DeleteOrganization(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (a *App) handleAdminListOrganizationMembers(w http.ResponseWriter, r *http.Request, user store.User) {
	a.writeOrganizationMembers(w, r, r.PathValue("id"))
}

func (a *App) handleAdminUpdateOrganizationMember(w http.ResponseWriter, r *http.Request, user store.User) {
	a.updateOrganizationMemberRole(w, r, r.PathValue("id"), r.PathValue("user_id"))
}

func (a *App) handleAdminTransferOrganizationOwner(w http.ResponseWriter, r *http.Request, user store.User) {
	a.transferOrganizationOwner(w, r, r.PathValue("id"))
}

func (a *App) upsertJSONSetting(r *http.Request, key string, value map[string]any, updatedBy string) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return a.store.Repository().UpsertSystemSetting(r.Context(), key, payload, updatedBy)
}

func redactSecretFields(value map[string]any) {
	for _, key := range []string{"client_secret", "bind_password", "api_key", "secret"} {
		if _, ok := value[key]; ok {
			value[key] = ""
		}
	}
}

func validateBrandingIcon(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	const maxIconBytes = 512 * 1024
	if !strings.HasPrefix(value, "data:image/png;base64,") && !strings.HasPrefix(value, "data:image/jpeg;base64,") && !strings.HasPrefix(value, "data:image/webp;base64,") && !strings.HasPrefix(value, "data:image/x-icon;base64,") && !strings.HasPrefix(value, "data:image/vnd.microsoft.icon;base64,") {
		return errors.New("icon must be a png, jpeg, webp, or ico data url")
	}
	_, encoded, ok := strings.Cut(value, ",")
	if !ok {
		return errors.New("invalid icon data url")
	}
	if base64.StdEncoding.DecodedLen(len(encoded)) > maxIconBytes {
		return errors.New("icon must be smaller than 512 KiB")
	}
	if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
		return errors.New("invalid icon base64")
	}
	return nil
}

func apiOrganizationMemberFromStore(member store.OrganizationMemberWithUser) apiOrganizationMember {
	return apiOrganizationMember{
		OrganizationID: member.OrganizationID,
		UserID:         member.UserID,
		Email:          member.Email,
		DisplayName:    member.DisplayName,
		Role:           member.Role,
		CreatedAt:      member.CreatedAt.Format(time.RFC3339),
	}
}
