package server

import (
	"net/http"

	"github.com/qinyongliang/gosshd/internal/store"
)

type apiTarget struct {
	ID             string `json:"id"`
	OwnerType      string `json:"owner_type"`
	OwnerID        string `json:"owner_id"`
	Alias          string `json:"alias"`
	TargetType     string `json:"target_type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	RemoteUsername string `json:"remote_username"`
	AuthType       string `json:"auth_type"`
	AgentID        string `json:"agent_id,omitempty"`
}

type apiTargetResponse struct {
	Target apiTarget `json:"target"`
}

type apiTargetsResponse struct {
	Targets []apiTarget `json:"targets"`
}

func (a *App) handleListTargets(w http.ResponseWriter, r *http.Request, user store.User) {
	targets, err := a.store.Repository().ListSSHTargets(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiTargetsResponse{}
	for _, target := range targets {
		out.Targets = append(out.Targets, apiTargetFromStore(target))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType      string `json:"owner_type"`
		OwnerID        string `json:"owner_id"`
		Alias          string `json:"alias"`
		TargetType     string `json:"target_type"`
		Host           string `json:"host"`
		Port           int    `json:"port"`
		RemoteUsername string `json:"remote_username"`
		AuthType       string `json:"auth_type"`
		Secret         string `json:"secret"`
		AgentID        string `json:"agent_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ownerType, ownerID := resolveOwner(req.OwnerType, req.OwnerID, user.ID)
	target, err := a.store.Repository().CreateSSHTarget(r.Context(), store.CreateSSHTargetParams{
		OwnerType:       ownerType,
		OwnerID:         ownerID,
		Alias:           req.Alias,
		TargetType:      req.TargetType,
		Host:            req.Host,
		Port:            req.Port,
		RemoteUsername:  req.RemoteUsername,
		AuthType:        req.AuthType,
		EncryptedSecret: []byte(req.Secret),
		AgentID:         req.AgentID,
		CreatedBy:       user.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiTargetResponse{Target: apiTargetFromStore(target)})
}

func apiTargetFromStore(target store.SSHTarget) apiTarget {
	return apiTarget{
		ID:             target.ID,
		OwnerType:      target.OwnerType,
		OwnerID:        target.OwnerID,
		Alias:          target.Alias,
		TargetType:     target.TargetType,
		Host:           target.Host,
		Port:           target.Port,
		RemoteUsername: target.RemoteUsername,
		AuthType:       target.AuthType,
		AgentID:        target.AgentID,
	}
}

func resolveOwner(ownerType, ownerID, userID string) (string, string) {
	if ownerType == "" || ownerID == "" || ownerID == "me" {
		return store.OwnerUser, userID
	}
	return ownerType, ownerID
}
