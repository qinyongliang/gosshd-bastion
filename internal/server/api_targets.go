package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiTarget struct {
	ID             string            `json:"id"`
	OwnerType      string            `json:"owner_type"`
	OwnerID        string            `json:"owner_id"`
	Name           string            `json:"name"`
	Alias          string            `json:"alias"`
	TargetType     string            `json:"target_type"`
	Host           string            `json:"host"`
	Port           int               `json:"port"`
	RemoteUsername string            `json:"remote_username"`
	AuthType       string            `json:"auth_type"`
	AgentID        string            `json:"agent_id,omitempty"`
	ProxyTargetID  string            `json:"proxy_target_id,omitempty"`
	Tags           []string          `json:"tags"`
	TagColors      map[string]string `json:"tag_colors,omitempty"`
}

type apiTargetResponse struct {
	Target apiTarget `json:"target"`
}

type apiTargetsResponse struct {
	Targets []apiTarget `json:"targets"`
}

func (a *App) handleListTargets(w http.ResponseWriter, r *http.Request, user store.User) {
	out := apiTargetsResponse{}
	tags := parseTargetTags(r.URL.Query().Get("tags"))
	ownerType := strings.TrimSpace(r.URL.Query().Get("owner_type"))
	ownerID := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	if ownerType != "" || ownerID != "" {
		resolvedType, resolvedID, err := a.resolveOwner(r.Context(), ownerType, ownerID, user.ID)
		if err != nil {
			writeOwnerError(w, err)
			return
		}
		targets, err := a.store.Repository().ListSSHTargetsFiltered(r.Context(), store.SSHTargetFilter{
			OwnerType: resolvedType,
			OwnerID:   resolvedID,
			Tags:      tags,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, target := range targets {
			out.Targets = append(out.Targets, apiTargetFromStore(target))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if user.IsSystemAdmin {
		targets, err := a.store.Repository().ListSSHTargetsFiltered(r.Context(), store.SSHTargetFilter{Tags: tags})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, target := range targets {
			out.Targets = append(out.Targets, apiTargetFromStore(target))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	orgs, err := a.store.Repository().ListOrganizationsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, org := range orgs {
		targets, err := a.store.Repository().ListSSHTargetsFiltered(r.Context(), store.SSHTargetFilter{
			OwnerType: store.OwnerOrganization,
			OwnerID:   org.ID,
			Tags:      tags,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, target := range targets {
			out.Targets = append(out.Targets, apiTargetFromStore(target))
		}
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
		writeOwnerError(w, err)
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
		ProxyTargetID  *string  `json:"proxy_target_id"`
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
	if _, _, err := a.resolveOwner(r.Context(), current.OwnerType, current.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	proxyTargetID := ""
	replaceProxy := req.ProxyTargetID != nil
	if replaceProxy {
		proxyTargetID = *req.ProxyTargetID
		if err := a.validateProxyTarget(r.Context(), current.OwnerType, current.OwnerID, proxyTargetID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
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
		ProxyTargetID:   proxyTargetID,
		ReplaceProxy:    replaceProxy,
		Tags:            req.Tags,
		ReplaceTags:     req.Tags != nil,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiTargetResponse{Target: apiTargetFromStore(target)})
}

func (a *App) handleUpdateTargetTagColor(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType string `json:"owner_type"`
		OwnerID   string `json:"owner_id"`
		Name      string `json:"name"`
		Color     string `json:"color"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ownerType, ownerID, err := a.resolveOwner(r.Context(), req.OwnerType, req.OwnerID, user.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	if err := a.store.Repository().UpdateTargetTagColor(r.Context(), ownerType, ownerID, req.Name, req.Color); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": strings.TrimSpace(req.Name), "color": strings.ToLower(strings.TrimSpace(req.Color))})
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
		TagColors:      cloneStringMap(target.TagColors),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
	if _, err := a.store.Repository().GetOrganization(ctx, ownerID); err != nil {
		return "", "", err
	}
	user, err := a.store.Repository().GetUser(ctx, userID)
	if err != nil {
		return "", "", errOwnerAccess
	}
	if user.IsSystemAdmin {
		return ownerType, ownerID, nil
	}
	if _, err := a.store.Repository().GetOrganizationMember(ctx, ownerID, userID); err != nil {
		return "", "", errOwnerAccess
	}
	return ownerType, ownerID, nil
}

var errOwnerAccess = errors.New("organization access required")

func writeOwnerError(w http.ResponseWriter, err error) {
	if errors.Is(err, errOwnerAccess) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
