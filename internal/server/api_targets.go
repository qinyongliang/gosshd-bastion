package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiTarget struct {
	ID             string   `json:"id"`
	OwnerType      string   `json:"owner_type"`
	OwnerID        string   `json:"owner_id"`
	Name           string   `json:"name"`
	Alias          string   `json:"alias"`
	TargetType     string   `json:"target_type"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	RemoteUsername string   `json:"remote_username"`
	AuthType       string   `json:"auth_type"`
	AgentID        string   `json:"agent_id,omitempty"`
	Tags           []string `json:"tags"`
}

type apiTargetResponse struct {
	Target apiTarget `json:"target"`
}

type apiTargetsResponse struct {
	Targets []apiTarget `json:"targets"`
}

func (a *App) handleListTargets(w http.ResponseWriter, r *http.Request, user store.User) {
	targets, err := a.store.Repository().ListSSHTargetsFiltered(r.Context(), store.SSHTargetFilter{
		OwnerType: r.URL.Query().Get("owner_type"),
		OwnerID:   r.URL.Query().Get("owner_id"),
		Tags:      parseTargetTags(r.URL.Query().Get("tags")),
	})
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
		Name           string   `json:"name"`
		OwnerType      string   `json:"owner_type"`
		OwnerID        string   `json:"owner_id"`
		Alias          string   `json:"alias"`
		TargetType     string   `json:"target_type"`
		Host           string   `json:"host"`
		Port           int      `json:"port"`
		RemoteUsername string   `json:"remote_username"`
		AuthType       string   `json:"auth_type"`
		Secret         string   `json:"secret"`
		AgentID        string   `json:"agent_id"`
		Tags           []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ownerType, ownerID, err := a.resolveOwner(r.Context(), req.OwnerType, req.OwnerID, user.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := a.store.Repository().CreateSSHTarget(r.Context(), store.CreateSSHTargetParams{
		OwnerType:       ownerType,
		OwnerID:         ownerID,
		Name:            req.Name,
		Alias:           req.Alias,
		TargetType:      req.TargetType,
		Host:            req.Host,
		Port:            req.Port,
		RemoteUsername:  req.RemoteUsername,
		AuthType:        req.AuthType,
		EncryptedSecret: []byte(req.Secret),
		AgentID:         req.AgentID,
		Tags:            req.Tags,
		CreatedBy:       user.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiTargetResponse{Target: apiTargetFromStore(target)})
}

func (a *App) handleUpdateTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Name           string   `json:"name"`
		Alias          string   `json:"alias"`
		Host           string   `json:"host"`
		Port           int      `json:"port"`
		RemoteUsername string   `json:"remote_username"`
		AuthType       string   `json:"auth_type"`
		Secret         string   `json:"secret"`
		AgentID        string   `json:"agent_id"`
		Tags           []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	var secret []byte
	if req.Secret != "" {
		secret = []byte(req.Secret)
	}
	target, err := a.store.Repository().UpdateSSHTarget(r.Context(), r.PathValue("id"), store.UpdateSSHTargetParams{
		Name:            req.Name,
		Alias:           req.Alias,
		Host:            req.Host,
		Port:            req.Port,
		RemoteUsername:  req.RemoteUsername,
		AuthType:        req.AuthType,
		EncryptedSecret: secret,
		AgentID:         req.AgentID,
		Tags:            req.Tags,
		ReplaceTags:     req.Tags != nil,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiTargetResponse{Target: apiTargetFromStore(target)})
}

func apiTargetFromStore(target store.SSHTarget) apiTarget {
	return apiTarget{
		ID:             target.ID,
		OwnerType:      target.OwnerType,
		OwnerID:        target.OwnerID,
		Name:           target.Name,
		Alias:          target.Alias,
		TargetType:     target.TargetType,
		Host:           target.Host,
		Port:           target.Port,
		RemoteUsername: target.RemoteUsername,
		AuthType:       target.AuthType,
		AgentID:        target.AgentID,
		Tags:           append([]string(nil), target.Tags...),
	}
}

func parseTargetTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func (a *App) resolveOwner(ctx context.Context, ownerType, ownerID, userID string) (string, string, error) {
	if ownerType == "" || ownerID == "" || ownerID == "me" {
		org, err := a.store.Repository().GetPersonalOrganizationForUser(ctx, userID)
		if err != nil {
			return "", "", err
		}
		return store.OwnerOrganization, org.ID, nil
	}
	return ownerType, ownerID, nil
}
