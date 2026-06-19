package server

import (
	"context"
	"errors"
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
	ProxyTargetID  string   `json:"proxy_target_id,omitempty"`
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
		ProxyTargetID  string   `json:"proxy_target_id"`
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
	if err := a.validateProxyTarget(r.Context(), ownerType, ownerID, req.ProxyTargetID); err != nil {
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
		ProxyTargetID:   req.ProxyTargetID,
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
		ProxyTargetID  string   `json:"proxy_target_id"`
		Tags           []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	current, err := a.store.Repository().GetSSHTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := a.validateProxyTarget(r.Context(), current.OwnerType, current.OwnerID, req.ProxyTargetID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
		ProxyTargetID:   req.ProxyTargetID,
		Tags:            req.Tags,
		ReplaceTags:     req.Tags != nil,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiTargetResponse{Target: apiTargetFromStore(target)})
}

func (a *App) validateProxyTarget(ctx context.Context, ownerType, ownerID, proxyTargetID string) error {
	proxyTargetID = strings.TrimSpace(proxyTargetID)
	if proxyTargetID == "" {
		return nil
	}
	proxy, err := a.store.Repository().GetSSHTarget(ctx, proxyTargetID)
	if err != nil {
		return err
	}
	if proxy.OwnerType != ownerType || proxy.OwnerID != ownerID {
		return errors.New("proxy target must belong to the same owner")
	}
	return nil
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
		ProxyTargetID:  target.ProxyTargetID,
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
	ownerType = strings.TrimSpace(ownerType)
	ownerID = strings.TrimSpace(ownerID)
	if ownerType == "" || ownerID == "" {
		return "", "", errors.New("owner_type and owner_id are required")
	}
	if ownerType != store.OwnerOrganization {
		return "", "", errors.New("owner_type must be organization")
	}
	return ownerType, ownerID, nil
}
