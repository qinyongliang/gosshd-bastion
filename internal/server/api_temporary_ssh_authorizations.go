package server

import (
	"math"
	"net/http"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiTemporarySSHAuthorization struct {
	ID        string `json:"id"`
	TargetID  string `json:"target_id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	CreatedBy string `json:"created_by"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type apiTemporarySSHAuthorizationResponse struct {
	Authorization apiTemporarySSHAuthorization `json:"authorization"`
}

type apiTemporarySSHAuthorizationsResponse struct {
	Authorizations []apiTemporarySSHAuthorization `json:"authorizations"`
}

func temporarySSHAuthorizationAPI(grant store.TemporarySSHAuthorization) apiTemporarySSHAuthorization {
	return apiTemporarySSHAuthorization{
		ID: grant.ID, TargetID: grant.TargetID, Name: grant.Name, Token: grant.Token, CreatedBy: grant.CreatedBy,
		ExpiresAt: grant.ExpiresAt.UTC().Format(time.RFC3339Nano), CreatedAt: formatAPITime(grant.CreatedAt), UpdatedAt: formatAPITime(grant.UpdatedAt),
	}
}

func temporarySSHDuration(seconds *int64) (time.Duration, bool) {
	if seconds == nil {
		return 24 * time.Hour, true
	}
	if *seconds <= 0 || *seconds > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	return time.Duration(*seconds) * time.Second, true
}

func (a *App) temporarySSHAPITarget(w http.ResponseWriter, r *http.Request, user store.User) (store.SSHTarget, bool) {
	target, err := a.targetForUser(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeOwnerError(w, err)
		return store.SSHTarget{}, false
	}
	return target, true
}

func (a *App) temporarySSHAPIGrant(w http.ResponseWriter, r *http.Request, user store.User, target store.SSHTarget) (store.TemporarySSHAuthorization, bool) {
	grant, err := a.store.Repository().GetTemporarySSHAuthorization(r.Context(), r.PathValue("authorization_id"))
	if err != nil {
		writeOwnerError(w, err)
		return store.TemporarySSHAuthorization{}, false
	}
	if grant.TargetID != target.ID {
		writeError(w, http.StatusNotFound, "authorization not found")
		return store.TemporarySSHAuthorization{}, false
	}
	if grant.CreatedBy != user.ID && a.requireOrganizationAdmin(r.Context(), target.OwnerID, user) != nil {
		writeError(w, http.StatusForbidden, "authorization access required")
		return store.TemporarySSHAuthorization{}, false
	}
	return grant, true
}

func (a *App) handleListTemporarySSHAuthorizations(w http.ResponseWriter, r *http.Request, user store.User) {
	target, ok := a.temporarySSHAPITarget(w, r, user)
	if !ok {
		return
	}
	grants, err := a.store.Repository().ListTemporarySSHAuthorizations(r.Context(), target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiTemporarySSHAuthorizationsResponse{Authorizations: make([]apiTemporarySSHAuthorization, 0, len(grants))}
	admin := a.requireOrganizationAdmin(r.Context(), target.OwnerID, user) == nil
	for _, grant := range grants {
		if admin || grant.CreatedBy == user.ID {
			out.Authorizations = append(out.Authorizations, temporarySSHAuthorizationAPI(grant))
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateTemporarySSHAuthorization(w http.ResponseWriter, r *http.Request, user store.User) {
	target, ok := a.temporarySSHAPITarget(w, r, user)
	if !ok {
		return
	}
	var req struct {
		Name            string `json:"name"`
		DurationSeconds *int64 `json:"duration_seconds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	duration, valid := temporarySSHDuration(req.DurationSeconds)
	if !valid {
		writeError(w, http.StatusBadRequest, "duration_seconds must be a positive duration")
		return
	}
	grant, err := a.store.Repository().CreateTemporarySSHAuthorization(r.Context(), store.CreateTemporarySSHAuthorizationParams{
		TargetID: target.ID, Name: req.Name, CreatedBy: user.ID, ExpiresAt: time.Now().UTC().Add(duration),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, apiTemporarySSHAuthorizationResponse{Authorization: temporarySSHAuthorizationAPI(grant)})
}

func (a *App) handleRenewTemporarySSHAuthorization(w http.ResponseWriter, r *http.Request, user store.User) {
	target, ok := a.temporarySSHAPITarget(w, r, user)
	if !ok {
		return
	}
	if _, ok := a.temporarySSHAPIGrant(w, r, user, target); !ok {
		return
	}
	var req struct {
		DurationSeconds *int64 `json:"duration_seconds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	duration, valid := temporarySSHDuration(req.DurationSeconds)
	if !valid {
		writeError(w, http.StatusBadRequest, "duration_seconds must be a positive duration")
		return
	}
	grant, err := a.store.Repository().RenewTemporarySSHAuthorization(r.Context(), r.PathValue("authorization_id"), target.ID, duration, time.Now().UTC())
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, apiTemporarySSHAuthorizationResponse{Authorization: temporarySSHAuthorizationAPI(grant)})
}

func (a *App) handleDeleteTemporarySSHAuthorization(w http.ResponseWriter, r *http.Request, user store.User) {
	target, ok := a.temporarySSHAPITarget(w, r, user)
	if !ok {
		return
	}
	if _, ok := a.temporarySSHAPIGrant(w, r, user, target); !ok {
		return
	}
	if err := a.store.Repository().DeleteTemporarySSHAuthorization(r.Context(), r.PathValue("authorization_id"), target.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
