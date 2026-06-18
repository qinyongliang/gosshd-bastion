package server

import (
	"net/http"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func (a *App) handleListPublicKeys(w http.ResponseWriter, r *http.Request, user store.User) {
	keys, err := a.store.Repository().ListPublicKeysForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiPublicKeysResponse{}
	for _, key := range keys {
		out.Keys = append(out.Keys, apiPublicKeyFromStore(key))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreatePublicKey(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Name          string `json:"name"`
		AuthorizedKey string `json:"authorized_key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	normalized, fingerprint, err := a.bastion.NormalizeAuthorizedKey(req.AuthorizedKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid public key")
		return
	}
	key, err := a.store.Repository().CreatePublicKey(r.Context(), store.CreatePublicKeyParams{
		UserID:        user.ID,
		Name:          req.Name,
		AuthorizedKey: normalized,
		Fingerprint:   fingerprint,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiPublicKeyResponse{Key: apiPublicKeyFromStore(key)})
}

func (a *App) handleDeletePublicKey(w http.ResponseWriter, r *http.Request, user store.User) {
	if err := a.store.Repository().DeletePublicKey(r.Context(), user.ID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
