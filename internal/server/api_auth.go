package server

import (
	"net/http"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
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

func (a *App) handleMe(w http.ResponseWriter, r *http.Request, user store.User) {
	orgs, err := a.store.Repository().ListOrganizationsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiMeResponse{User: apiUserFromStore(user)}
	for _, org := range orgs {
		out.Organizations = append(out.Organizations, apiOrganizationFromStore(org))
	}
	writeJSON(w, http.StatusOK, out)
}
