package server

import (
	"encoding/json"
	"net/http"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiOrganizationMember struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	Email          string `json:"email"`
	DisplayName    string `json:"display_name"`
	Role           string `json:"role"`
}

type apiOrganizationMembersResponse struct {
	Members []apiOrganizationMember `json:"members"`
}

func (a *App) handleAdminSettings(w http.ResponseWriter, r *http.Request, user store.User) {
	out := map[string]any{}
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
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.IsSystemAdmin != nil {
		if err := a.store.Repository().UpdateUserSystemAdmin(r.Context(), r.PathValue("id"), *req.IsSystemAdmin); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
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
		out.Organizations = append(out.Organizations, apiOrganizationFromStore(org))
	}
	writeJSON(w, http.StatusOK, out)
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

func apiOrganizationMemberFromStore(member store.OrganizationMemberWithUser) apiOrganizationMember {
	return apiOrganizationMember{
		OrganizationID: member.OrganizationID,
		UserID:         member.UserID,
		Email:          member.Email,
		DisplayName:    member.DisplayName,
		Role:           member.Role,
	}
}
