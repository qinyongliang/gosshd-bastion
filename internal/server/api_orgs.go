package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd/internal/store"
)

func (a *App) handleCreateOrganization(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	org, err := a.store.Repository().CreateOrganization(r.Context(), store.CreateOrganizationParams{
		Name:        req.Name,
		Slug:        req.Slug,
		OwnerUserID: user.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiOrganizationResponse{Organization: apiOrganizationFromStore(org)})
}

func (a *App) handleListOrganizations(w http.ResponseWriter, r *http.Request, user store.User) {
	orgs, err := a.store.Repository().ListOrganizationsForUser(r.Context(), user.ID)
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

func (a *App) handleCreateOrganizationInvite(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = store.RoleMember
	}
	code, hash, err := randomCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := a.store.Repository().CreateOrganizationInvite(r.Context(), store.CreateOrganizationInviteParams{
		OrganizationID: orgID,
		CodeHash:       hash,
		Role:           role,
		ExpiresAt:      time.Now().UTC().Add(7 * 24 * time.Hour),
		CreatedBy:      user.ID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiInviteResponse{Code: code})
}

func (a *App) handleJoinOrganization(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	invite, err := a.store.Repository().GetOrganizationInviteByCodeHash(r.Context(), codeHash(req.Code))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invite")
		return
	}
	if invite.ConsumedAt != nil || time.Now().UTC().After(invite.ExpiresAt) {
		writeError(w, http.StatusBadRequest, "invite expired")
		return
	}
	if err := a.store.Repository().AddOrganizationMember(r.Context(), invite.OrganizationID, user.ID, invite.Role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.store.Repository().MarkOrganizationInviteConsumed(r.Context(), invite.ID, time.Now().UTC())
	org, err := a.store.Repository().GetOrganization(r.Context(), invite.OrganizationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiOrganizationResponse{Organization: apiOrganizationFromStore(org)})
}
