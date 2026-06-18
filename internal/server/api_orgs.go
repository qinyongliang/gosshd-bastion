package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

var errPersonalInvite = errors.New("personal organization cannot invite users")

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
		apiOrg := apiOrganizationFromStore(org)
		if member, err := a.store.Repository().GetOrganizationMember(r.Context(), org.ID, user.ID); err == nil {
			apiOrg.Role = member.Role
		}
		out.Organizations = append(out.Organizations, apiOrg)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateOrganizationInvite(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	org, err := a.store.Repository().GetOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}
	if org.IsPersonal {
		writeError(w, http.StatusBadRequest, errPersonalInvite.Error())
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
	org, err := a.joinOrganizationWithCode(r.Context(), user.ID, req.Code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiOrganizationResponse{Organization: apiOrganizationFromStore(org)})
}

func (a *App) joinOrganizationWithCode(ctx context.Context, userID, code string) (store.Organization, error) {
	invite, err := a.store.Repository().GetOrganizationInviteByCodeHash(ctx, codeHash(code))
	if err != nil {
		return store.Organization{}, err
	}
	if invite.ConsumedAt != nil || time.Now().UTC().After(invite.ExpiresAt) {
		return store.Organization{}, errors.New("invite expired")
	}
	if err := a.store.Repository().AddOrganizationMember(ctx, invite.OrganizationID, userID, invite.Role); err != nil {
		return store.Organization{}, err
	}
	_ = a.store.Repository().MarkOrganizationInviteConsumed(ctx, invite.ID, time.Now().UTC())
	org, err := a.store.Repository().GetOrganization(ctx, invite.OrganizationID)
	if err != nil {
		return store.Organization{}, err
	}
	return org, nil
}

func (a *App) handleLeaveOrganization(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if _, err := a.store.Repository().GetOrganizationMember(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusForbidden, "organization access required")
		return
	}
	if err := a.store.Repository().LeaveOrganization(r.Context(), orgID, user.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleListOrganizationMembers(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	a.writeOrganizationMembers(w, r, orgID)
}

func (a *App) handleAddOrganizationMember(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" && strings.TrimSpace(req.Email) != "" {
		target, err := a.store.Repository().GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		userID = target.ID
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id or email is required")
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = store.RoleMember
	}
	if role == store.RoleOwner {
		writeError(w, http.StatusBadRequest, "use transfer owner endpoint")
		return
	}
	if err := a.store.Repository().AddOrganizationMember(r.Context(), orgID, userID, role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleUpdateOrganizationMember(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	a.updateOrganizationMemberRole(w, r, orgID, r.PathValue("user_id"))
}

func (a *App) handleRemoveOrganizationMember(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if err := a.requireOrganizationAdmin(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := a.store.Repository().RemoveOrganizationMember(r.Context(), orgID, r.PathValue("user_id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleTransferOrganizationOwner(w http.ResponseWriter, r *http.Request, user store.User) {
	orgID := r.PathValue("id")
	if err := a.requireOrganizationOwner(r.Context(), orgID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	a.transferOrganizationOwner(w, r, orgID)
}

func (a *App) writeOrganizationMembers(w http.ResponseWriter, r *http.Request, orgID string) {
	members, err := a.store.Repository().ListOrganizationMembers(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiOrganizationMembersResponse{}
	for _, member := range members {
		out.Members = append(out.Members, apiOrganizationMemberFromStore(member))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) updateOrganizationMemberRole(w http.ResponseWriter, r *http.Request, orgID, userID string) {
	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Role == store.RoleOwner {
		writeError(w, http.StatusBadRequest, "use transfer owner endpoint")
		return
	}
	if err := a.store.Repository().UpdateOrganizationMemberRole(r.Context(), orgID, userID, req.Role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) transferOrganizationOwner(w http.ResponseWriter, r *http.Request, orgID string) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.store.Repository().TransferOrganizationOwner(r.Context(), orgID, req.UserID, store.RoleAdmin); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
