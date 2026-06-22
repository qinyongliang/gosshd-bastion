package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
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
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
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
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	group, err := a.organizationGroupForWrite(r, orgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, userID); err != nil {
		writeError(w, http.StatusBadRequest, "user must belong to the organization")
		return
	}
	if err := a.store.Repository().AddUserToGroup(r.Context(), group.ID, userID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleRemoveOrganizationGroupMember(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	group, err := a.organizationGroupForWrite(r, orgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.Repository().RemoveUserFromGroup(r.Context(), group.ID, r.PathValue("user_id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) organizationGroupForWrite(r *http.Request, orgID string) (store.OrganizationUserGroup, error) {
	group, err := a.store.Repository().GetOrganizationUserGroup(r.Context(), r.PathValue("group_id"))
	if err != nil {
		return store.OrganizationUserGroup{}, err
	}
	if group.OrganizationID != orgID {
		return store.OrganizationUserGroup{}, errors.New("group must belong to the organization")
	}
	return group, nil
}

func apiUserGroupFromStore(group store.OrganizationUserGroup) apiUserGroup {
	return apiUserGroup{
		ID:        group.ID,
		Name:      group.Name,
		Slug:      group.Slug,
		IsDefault: group.IsDefault,
	}
}
