package server

import (
	"net/http"

	"github.com/qinyongliang/gosshd/internal/store"
)

type apiUserGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsDefault bool   `json:"is_default"`
}

type apiUserGroupResponse struct {
	Group apiUserGroup `json:"group"`
}

type apiUserGroupsResponse struct {
	Groups []apiUserGroup `json:"groups"`
}

func (a *App) handleListOrganizationGroups(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	groups, err := a.store.Repository().ListOrganizationUserGroups(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiUserGroupsResponse{}
	for _, group := range groups {
		out.Groups = append(out.Groups, apiUserGroupFromStore(group))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateOrganizationGroup(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	group, err := a.store.Repository().CreateOrganizationUserGroup(r.Context(), store.CreateOrganizationUserGroupParams{
		OrganizationID: orgID,
		Name:           req.Name,
		Slug:           req.Slug,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiUserGroupResponse{Group: apiUserGroupFromStore(group)})
}

func (a *App) handleAddOrganizationGroupMember(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.store.Repository().AddUserToGroup(r.Context(), r.PathValue("group_id"), req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleRemoveOrganizationGroupMember(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	if err := a.store.Repository().RemoveUserFromGroup(r.Context(), r.PathValue("group_id"), r.PathValue("user_id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiUserGroupFromStore(group store.OrganizationUserGroup) apiUserGroup {
	return apiUserGroup{
		ID:        group.ID,
		Name:      group.Name,
		Slug:      group.Slug,
		IsDefault: group.IsDefault,
	}
}
