package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	CredentialID   string            `json:"credential_id,omitempty"`
	FolderID       string            `json:"folder_id,omitempty"`
	Tags           []string          `json:"tags"`
	TagColors      map[string]string `json:"tag_colors,omitempty"`
}

type apiSSHCredential struct {
	ID        string `json:"id"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	AuthType  string `json:"auth_type"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type apiTargetFolder struct {
	ID        string `json:"id"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	ParentID  string `json:"parent_id,omitempty"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type apiTargetResponse struct {
	Target apiTarget `json:"target"`
}

type apiTargetsResponse struct {
	Targets []apiTarget `json:"targets"`
}

type apiSSHCredentialsResponse struct {
	Credentials []apiSSHCredential `json:"credentials"`
}

type apiSSHCredentialResponse struct {
	Credential apiSSHCredential `json:"credential"`
}

type apiTargetFoldersResponse struct {
	Folders []apiTargetFolder `json:"folders"`
}

type apiTargetFolderResponse struct {
	Folder apiTargetFolder `json:"folder"`
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
		CredentialID   string   `json:"credential_id"`
		FolderID       string   `json:"folder_id"`
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
	if err := a.validateCredential(r.Context(), ownerType, ownerID, req.CredentialID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.validateTargetFolder(r.Context(), ownerType, ownerID, req.FolderID); err != nil {
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
		CredentialID:    req.CredentialID,
		FolderID:        req.FolderID,
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
		CredentialID   *string  `json:"credential_id"`
		FolderID       *string  `json:"folder_id"`
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
	credentialID := ""
	replaceCredential := req.CredentialID != nil
	if replaceCredential {
		credentialID = *req.CredentialID
		if err := a.validateCredential(r.Context(), current.OwnerType, current.OwnerID, credentialID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	folderID := ""
	replaceFolder := req.FolderID != nil
	if replaceFolder {
		folderID = *req.FolderID
		if err := a.validateTargetFolder(r.Context(), current.OwnerType, current.OwnerID, folderID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	target, err := a.store.Repository().UpdateSSHTarget(r.Context(), r.PathValue("id"), store.UpdateSSHTargetParams{
		Name:              req.Name,
		Alias:             req.Alias,
		Host:              req.Host,
		Port:              req.Port,
		RemoteUsername:    req.RemoteUsername,
		AuthType:          req.AuthType,
		EncryptedSecret:   secret,
		AgentID:           req.AgentID,
		ProxyTargetID:     proxyTargetID,
		ReplaceProxy:      replaceProxy,
		CredentialID:      credentialID,
		ReplaceCredential: replaceCredential,
		FolderID:          folderID,
		ReplaceFolder:     replaceFolder,
		Tags:              req.Tags,
		ReplaceTags:       req.Tags != nil,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiTargetResponse{Target: apiTargetFromStore(target)})
}

func (a *App) handleDeleteTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	target, err := a.store.Repository().GetSSHTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, _, err := a.resolveOwner(r.Context(), target.OwnerType, target.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	if err := a.store.Repository().DeleteSSHTarget(r.Context(), target.ID); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (a *App) handleCopyTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	current, err := a.store.Repository().GetSSHTarget(r.Context(), r.PathValue("id"))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, _, err := a.resolveOwner(r.Context(), current.OwnerType, current.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	targets, err := a.store.Repository().ListSSHTargets(r.Context(), current.OwnerType, current.OwnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	used := map[string]bool{}
	for _, target := range targets {
		used[target.Alias] = true
	}
	alias := nextCopyAlias(current.Alias, used)
	name := strings.TrimSpace(current.Name) + " copy"
	if strings.TrimSpace(current.Name) == "" {
		name = alias
	}
	copied, err := a.store.Repository().CreateSSHTarget(r.Context(), store.CreateSSHTargetParams{
		OwnerType:       current.OwnerType,
		OwnerID:         current.OwnerID,
		Name:            name,
		Alias:           alias,
		TargetType:      current.TargetType,
		Host:            current.Host,
		Port:            current.Port,
		RemoteUsername:  current.RemoteUsername,
		AuthType:        current.AuthType,
		EncryptedSecret: append([]byte(nil), current.EncryptedSecret...),
		AgentID:         current.AgentID,
		ProxyTargetID:   current.ProxyTargetID,
		CredentialID:    current.CredentialID,
		FolderID:        current.FolderID,
		Tags:            append([]string(nil), current.Tags...),
		CreatedBy:       user.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiTargetResponse{Target: apiTargetFromStore(copied)})
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

func (a *App) handleListSSHCredentials(w http.ResponseWriter, r *http.Request, user store.User) {
	ownerType, ownerID, err := a.resolveOwner(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"), user.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	credentials, err := a.store.Repository().ListSSHCredentials(r.Context(), ownerType, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiSSHCredentialsResponse{}
	for _, credential := range credentials {
		out.Credentials = append(out.Credentials, apiSSHCredentialFromStore(credential))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateSSHCredential(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType string `json:"owner_type"`
		OwnerID   string `json:"owner_id"`
		Name      string `json:"name"`
		Username  string `json:"username"`
		AuthType  string `json:"auth_type"`
		Secret    string `json:"secret"`
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
	credential, err := a.store.Repository().CreateSSHCredential(r.Context(), store.CreateSSHCredentialParams{
		OwnerType: ownerType, OwnerID: ownerID, Name: req.Name, Username: req.Username,
		AuthType: req.AuthType, EncryptedSecret: []byte(req.Secret), CreatedBy: user.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiSSHCredentialResponse{Credential: apiSSHCredentialFromStore(credential)})
}

func (a *App) handleUpdateSSHCredential(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		AuthType string `json:"auth_type"`
		Secret   string `json:"secret"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	current, err := a.store.Repository().GetSSHCredential(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, _, err := a.resolveOwner(r.Context(), current.OwnerType, current.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	var secret []byte
	if req.Secret != "" {
		secret = []byte(req.Secret)
	}
	credential, err := a.store.Repository().UpdateSSHCredential(r.Context(), current.ID, store.UpdateSSHCredentialParams{
		Name: req.Name, Username: req.Username, AuthType: req.AuthType, EncryptedSecret: secret,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiSSHCredentialResponse{Credential: apiSSHCredentialFromStore(credential)})
}

func (a *App) handleDeleteSSHCredential(w http.ResponseWriter, r *http.Request, user store.User) {
	current, err := a.store.Repository().GetSSHCredential(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, _, err := a.resolveOwner(r.Context(), current.OwnerType, current.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	if err := a.store.Repository().DeleteSSHCredential(r.Context(), current.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (a *App) handleListTargetFolders(w http.ResponseWriter, r *http.Request, user store.User) {
	ownerType, ownerID, err := a.resolveOwner(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"), user.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	folders, err := a.store.Repository().ListTargetFolders(r.Context(), ownerType, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiTargetFoldersResponse{}
	for _, folder := range folders {
		out.Folders = append(out.Folders, apiTargetFolderFromStore(folder))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateTargetFolder(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType string `json:"owner_type"`
		OwnerID   string `json:"owner_id"`
		ParentID  string `json:"parent_id"`
		Name      string `json:"name"`
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
	folder, err := a.store.Repository().CreateTargetFolder(r.Context(), store.CreateTargetFolderParams{
		OwnerType: ownerType, OwnerID: ownerID, ParentID: req.ParentID, Name: req.Name, CreatedBy: user.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiTargetFolderResponse{Folder: apiTargetFolderFromStore(folder)})
}

func (a *App) handleUpdateTargetFolder(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		ParentID *string `json:"parent_id"`
		Name     string  `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	current, err := a.store.Repository().GetTargetFolder(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, _, err := a.resolveOwner(r.Context(), current.OwnerType, current.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	parentID := ""
	replaceParent := req.ParentID != nil
	if replaceParent {
		parentID = *req.ParentID
	}
	folder, err := a.store.Repository().UpdateTargetFolder(r.Context(), current.ID, store.UpdateTargetFolderParams{
		Name: req.Name, ParentID: parentID, ReplaceParent: replaceParent,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiTargetFolderResponse{Folder: apiTargetFolderFromStore(folder)})
}

func (a *App) handleDeleteTargetFolder(w http.ResponseWriter, r *http.Request, user store.User) {
	current, err := a.store.Repository().GetTargetFolder(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, _, err := a.resolveOwner(r.Context(), current.OwnerType, current.OwnerID, user.ID); err != nil {
		writeOwnerError(w, err)
		return
	}
	if err := a.store.Repository().DeleteTargetFolder(r.Context(), current.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (a *App) handleMySettings(w http.ResponseWriter, r *http.Request, user store.User) {
	settings, err := a.store.Repository().ListUserSettings(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := map[string]any{
		"connect_open_mode":       "popup",
		"connect_attach_existing": false,
	}
	for key, raw := range settings {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			continue
		}
		out[key] = value
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleUpdateMySettings(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		ConnectOpenMode       string `json:"connect_open_mode"`
		ConnectAttachExisting bool   `json:"connect_attach_existing"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	mode := strings.TrimSpace(req.ConnectOpenMode)
	if mode != "popup" && mode != "tab" {
		writeError(w, http.StatusBadRequest, "invalid connect_open_mode")
		return
	}
	raw, _ := json.Marshal(mode)
	if err := a.store.Repository().UpsertUserSetting(r.Context(), user.ID, "connect_open_mode", raw); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	attachRaw, _ := json.Marshal(req.ConnectAttachExisting)
	if err := a.store.Repository().UpsertUserSetting(r.Context(), user.ID, "connect_attach_existing", attachRaw); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connect_open_mode":       mode,
		"connect_attach_existing": req.ConnectAttachExisting,
	})
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

func (a *App) validateCredential(ctx context.Context, ownerType, ownerID, credentialID string) error {
	if strings.TrimSpace(credentialID) == "" {
		return nil
	}
	credential, err := a.store.Repository().GetSSHCredential(ctx, credentialID)
	if err != nil {
		return err
	}
	if credential.OwnerType != ownerType || credential.OwnerID != ownerID {
		return errors.New("credential belongs to another owner")
	}
	return nil
}

func (a *App) validateTargetFolder(ctx context.Context, ownerType, ownerID, folderID string) error {
	if strings.TrimSpace(folderID) == "" {
		return nil
	}
	folder, err := a.store.Repository().GetTargetFolder(ctx, folderID)
	if err != nil {
		return err
	}
	if folder.OwnerType != ownerType || folder.OwnerID != ownerID {
		return errors.New("folder belongs to another owner")
	}
	return nil
}

func nextCopyAlias(alias string, used map[string]bool) string {
	base := strings.TrimSpace(alias)
	if base == "" {
		base = "target"
	}
	for index := 1; ; index++ {
		suffix := "_copy"
		if index > 1 {
			suffix = fmt.Sprintf("_copy_%d", index)
		}
		next := base + suffix
		if !used[next] {
			return next
		}
	}
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
		CredentialID:   target.CredentialID,
		FolderID:       target.FolderID,
		Tags:           append([]string(nil), target.Tags...),
		TagColors:      cloneStringMap(target.TagColors),
	}
}

func apiSSHCredentialFromStore(credential store.SSHCredential) apiSSHCredential {
	return apiSSHCredential{
		ID: credential.ID, OwnerType: credential.OwnerType, OwnerID: credential.OwnerID,
		Name: credential.Name, Username: credential.Username, AuthType: credential.AuthType,
		CreatedAt: formatAPITime(credential.CreatedAt), UpdatedAt: formatAPITime(credential.UpdatedAt),
	}
}

func apiTargetFolderFromStore(folder store.TargetFolder) apiTargetFolder {
	return apiTargetFolder{
		ID: folder.ID, OwnerType: folder.OwnerType, OwnerID: folder.OwnerID, ParentID: folder.ParentID,
		Name: folder.Name, CreatedAt: formatAPITime(folder.CreatedAt), UpdatedAt: formatAPITime(folder.UpdatedAt),
	}
}

func formatAPITime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
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
