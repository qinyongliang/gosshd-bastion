package server

import (
	"context"
	"net/http"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiPolicy struct {
	ID               string          `json:"id"`
	OwnerType        string          `json:"owner_type"`
	OwnerID          string          `json:"owner_id"`
	Name             string          `json:"name"`
	DefaultAction    string          `json:"default_action"`
	LLMConfigID      string          `json:"llm_config_id"`
	LLMPromptID      string          `json:"llm_prompt_id"`
	IPAllowlist      string          `json:"ip_allowlist"`
	AllowPortForward bool            `json:"allow_port_forward"`
	AllowUpload      bool            `json:"allow_upload"`
	AllowDownload    bool            `json:"allow_download"`
	AllowInteractive bool            `json:"allow_interactive"`
	TargetIDs        []string        `json:"target_ids"`
	UserGroupIDs     []string        `json:"user_group_ids"`
	TargetTags       []string        `json:"target_tags"`
	Rules            []apiPolicyRule `json:"rules"`
}

type apiPolicyRule struct {
	ID          string `json:"id"`
	RuleType    string `json:"rule_type"`
	PatternType string `json:"pattern_type"`
	Pattern     string `json:"pattern"`
}

type apiPolicyResponse struct {
	Policy apiPolicy `json:"policy"`
}

type apiLLMConfig struct {
	ID             string `json:"id"`
	OwnerType      string `json:"owner_type"`
	OwnerID        string `json:"owner_id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type apiLLMConfigResponse struct {
	Config apiLLMConfig `json:"config"`
}

type apiLLMConfigsResponse struct {
	Configs []apiLLMConfig `json:"configs"`
}

type apiLLMPrompt struct {
	ID         string `json:"id"`
	OwnerType  string `json:"owner_type"`
	OwnerID    string `json:"owner_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	IsDefault  bool   `json:"is_default"`
	IsReadonly bool   `json:"is_readonly"`
}

type apiLLMPromptResponse struct {
	Prompt apiLLMPrompt `json:"prompt"`
}

type apiLLMPromptsResponse struct {
	Prompts []apiLLMPrompt `json:"prompts"`
}

func (a *App) handleListLLMConfigs(w http.ResponseWriter, r *http.Request, user store.User) {
	ownerType, ownerID, err := a.resolveOwner(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"), user.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	configs, err := a.store.Repository().ListLLMPolicyConfigs(r.Context(), ownerType, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiLLMConfigsResponse{}
	for _, cfg := range configs {
		out.Configs = append(out.Configs, apiLLMConfigFromStore(cfg))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateLLMConfig(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType      string `json:"owner_type"`
		OwnerID        string `json:"owner_id"`
		Name           string `json:"name"`
		BaseURL        string `json:"base_url"`
		APIKey         string `json:"api_key"`
		Model          string `json:"model"`
		TimeoutSeconds int    `json:"timeout_seconds"`
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
	if err := a.requireOrganizationAdmin(r.Context(), ownerID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	cfg, err := a.store.Repository().CreateLLMPolicyConfig(r.Context(), store.CreateLLMPolicyConfigParams{
		OwnerType:       ownerType,
		OwnerID:         ownerID,
		Name:            req.Name,
		BaseURL:         req.BaseURL,
		EncryptedAPIKey: []byte(req.APIKey),
		Model:           req.Model,
		TimeoutSeconds:  req.TimeoutSeconds,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiLLMConfigResponse{Config: apiLLMConfigFromStore(cfg)})
}

func (a *App) handleUpdateLLMConfig(w http.ResponseWriter, r *http.Request, user store.User) {
	cfg, err := a.llmConfigForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		Name           string `json:"name"`
		BaseURL        string `json:"base_url"`
		APIKey         string `json:"api_key"`
		Model          string `json:"model"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		req.Name = cfg.Name
	}
	if req.BaseURL == "" {
		req.BaseURL = cfg.BaseURL
	}
	if req.Model == "" {
		req.Model = cfg.Model
	}
	updated, err := a.store.Repository().UpdateLLMPolicyConfig(r.Context(), cfg.ID, store.UpdateLLMPolicyConfigParams{
		Name:            req.Name,
		BaseURL:         req.BaseURL,
		EncryptedAPIKey: []byte(req.APIKey),
		Model:           req.Model,
		TimeoutSeconds:  req.TimeoutSeconds,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiLLMConfigResponse{Config: apiLLMConfigFromStore(updated)})
}

func (a *App) handleDeleteLLMConfig(w http.ResponseWriter, r *http.Request, user store.User) {
	if _, err := a.llmConfigForWrite(r.Context(), r.PathValue("id"), user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := a.store.Repository().DeleteLLMPolicyConfig(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleListLLMPrompts(w http.ResponseWriter, r *http.Request, user store.User) {
	ownerType, ownerID, err := a.resolveOwner(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"), user.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	prompts, err := a.store.Repository().ListLLMPromptResources(r.Context(), ownerType, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiLLMPromptsResponse{}
	for _, prompt := range prompts {
		out.Prompts = append(out.Prompts, apiLLMPromptFromStore(prompt))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateLLMPrompt(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType string `json:"owner_type"`
		OwnerID   string `json:"owner_id"`
		Title     string `json:"title"`
		Content   string `json:"content"`
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
	if err := a.requireOrganizationAdmin(r.Context(), ownerID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	prompt, err := a.store.Repository().CreateLLMPromptResource(r.Context(), store.CreateLLMPromptResourceParams{
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Title:     req.Title,
		Content:   req.Content,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiLLMPromptResponse{Prompt: apiLLMPromptFromStore(prompt)})
}

func (a *App) handleUpdateLLMPrompt(w http.ResponseWriter, r *http.Request, user store.User) {
	prompt, err := a.llmPromptForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if prompt.IsReadonly {
		writeError(w, http.StatusBadRequest, "readonly prompt cannot be modified")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Title == "" {
		req.Title = prompt.Title
	}
	if req.Content == "" {
		req.Content = prompt.Content
	}
	updated, err := a.store.Repository().UpdateLLMPromptResource(r.Context(), prompt.ID, store.UpdateLLMPromptResourceParams{
		Title:   req.Title,
		Content: req.Content,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiLLMPromptResponse{Prompt: apiLLMPromptFromStore(updated)})
}

func (a *App) handleDeleteLLMPrompt(w http.ResponseWriter, r *http.Request, user store.User) {
	prompt, err := a.llmPromptForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if prompt.IsReadonly {
		writeError(w, http.StatusBadRequest, "readonly prompt cannot be deleted")
		return
	}
	if err := a.store.Repository().DeleteLLMPromptResource(r.Context(), prompt.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleListPolicies(w http.ResponseWriter, r *http.Request, user store.User) {
	ownerType, ownerID, err := a.resolveOwner(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"), user.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	policies, err := a.store.Repository().ListCommandPolicies(r.Context(), ownerType, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := struct {
		Policies []apiPolicy `json:"policies"`
	}{}
	for _, policy := range policies {
		out.Policies = append(out.Policies, apiPolicyFromStore(policy))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreatePolicy(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		OwnerType        string `json:"owner_type"`
		OwnerID          string `json:"owner_id"`
		Name             string `json:"name"`
		DefaultAction    string `json:"default_action"`
		LLMConfigID      string `json:"llm_config_id"`
		LLMPromptID      string `json:"llm_prompt_id"`
		IPAllowlist      string `json:"ip_allowlist"`
		AllowPortForward bool   `json:"allow_port_forward"`
		AllowUpload      bool   `json:"allow_upload"`
		AllowDownload    bool   `json:"allow_download"`
		AllowInteractive bool   `json:"allow_interactive"`
		PromptTitle      string `json:"prompt_title"`
		PromptContent    string `json:"prompt_content"`
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
	if err := a.requireOrganizationAdmin(r.Context(), ownerID, user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	llmPromptID := req.LLMPromptID
	if req.PromptTitle != "" || req.PromptContent != "" {
		prompt, err := a.store.Repository().CreateLLMPromptResource(r.Context(), store.CreateLLMPromptResourceParams{
			OwnerType: ownerType,
			OwnerID:   ownerID,
			Title:     req.PromptTitle,
			Content:   req.PromptContent,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		llmPromptID = prompt.ID
	}
	policy, err := a.store.Repository().CreateCommandPolicy(r.Context(), store.CreateCommandPolicyParams{
		OwnerType:        ownerType,
		OwnerID:          ownerID,
		Name:             req.Name,
		DefaultAction:    req.DefaultAction,
		LLMConfigID:      req.LLMConfigID,
		LLMPromptID:      llmPromptID,
		IPAllowlist:      req.IPAllowlist,
		AllowPortForward: req.AllowPortForward,
		AllowUpload:      req.AllowUpload,
		AllowDownload:    req.AllowDownload,
		AllowInteractive: req.AllowInteractive,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiPolicyResponse{Policy: apiPolicyFromStore(policy)})
}

func (a *App) handleUpdatePolicy(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		Name             string `json:"name"`
		DefaultAction    string `json:"default_action"`
		LLMConfigID      string `json:"llm_config_id"`
		LLMPromptID      string `json:"llm_prompt_id"`
		IPAllowlist      string `json:"ip_allowlist"`
		AllowPortForward bool   `json:"allow_port_forward"`
		AllowUpload      bool   `json:"allow_upload"`
		AllowDownload    bool   `json:"allow_download"`
		AllowInteractive bool   `json:"allow_interactive"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		req.Name = policy.Name
	}
	updated, err := a.store.Repository().UpdateCommandPolicy(r.Context(), policy.ID, store.UpdateCommandPolicyParams{
		Name:             req.Name,
		DefaultAction:    req.DefaultAction,
		LLMConfigID:      req.LLMConfigID,
		LLMPromptID:      req.LLMPromptID,
		IPAllowlist:      req.IPAllowlist,
		AllowPortForward: req.AllowPortForward,
		AllowUpload:      req.AllowUpload,
		AllowDownload:    req.AllowDownload,
		AllowInteractive: req.AllowInteractive,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiPolicyResponse{Policy: apiPolicyFromStore(updated)})
}

func (a *App) handleDeletePolicy(w http.ResponseWriter, r *http.Request, user store.User) {
	if _, err := a.policyForWrite(r.Context(), r.PathValue("id"), user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := a.store.Repository().DeleteCommandPolicy(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleCopyPolicy(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	copied, err := a.store.Repository().CopyCommandPolicy(r.Context(), policy.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiPolicyResponse{Policy: apiPolicyFromStore(copied)})
}

func (a *App) handleCreatePolicyRule(w http.ResponseWriter, r *http.Request, user store.User) {
	if _, err := a.policyForWrite(r.Context(), r.PathValue("id"), user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		RuleType    string `json:"rule_type"`
		PatternType string `json:"pattern_type"`
		Pattern     string `json:"pattern"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := a.store.Repository().CreatePolicyRule(r.Context(), store.CreatePolicyRuleParams{
		PolicyID:    r.PathValue("id"),
		RuleType:    req.RuleType,
		PatternType: req.PatternType,
		Pattern:     req.Pattern,
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (a *App) handleDeletePolicyRule(w http.ResponseWriter, r *http.Request, user store.User) {
	if _, err := a.policyForWrite(r.Context(), r.PathValue("id"), user); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := a.store.Repository().DeletePolicyRule(r.Context(), r.PathValue("id"), r.PathValue("rule_id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) policyForWrite(ctx context.Context, id string, user store.User) (store.CommandPolicy, error) {
	policy, err := a.store.Repository().GetCommandPolicy(ctx, id)
	if err != nil {
		return store.CommandPolicy{}, err
	}
	if policy.OwnerType == store.OwnerOrganization {
		if err := a.requireOrganizationAdmin(ctx, policy.OwnerID, user); err != nil {
			return store.CommandPolicy{}, err
		}
	}
	return policy, nil
}

func (a *App) llmConfigForWrite(ctx context.Context, id string, user store.User) (store.LLMPolicyConfig, error) {
	cfg, err := a.store.Repository().GetLLMPolicyConfig(ctx, id)
	if err != nil {
		return store.LLMPolicyConfig{}, err
	}
	if cfg.OwnerType == store.OwnerOrganization {
		if err := a.requireOrganizationAdmin(ctx, cfg.OwnerID, user); err != nil {
			return store.LLMPolicyConfig{}, err
		}
	}
	return cfg, nil
}

func (a *App) llmPromptForWrite(ctx context.Context, id string, user store.User) (store.LLMPromptResource, error) {
	prompt, err := a.store.Repository().GetLLMPromptResource(ctx, id)
	if err != nil {
		return store.LLMPromptResource{}, err
	}
	if prompt.OwnerType == store.OwnerOrganization {
		if err := a.requireOrganizationAdmin(ctx, prompt.OwnerID, user); err != nil {
			return store.LLMPromptResource{}, err
		}
	}
	return prompt, nil
}

func (a *App) handleAttachPolicyTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		TargetID string `json:"target_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	target, err := a.store.Repository().GetSSHTarget(r.Context(), req.TargetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if target.OwnerType != policy.OwnerType || target.OwnerID != policy.OwnerID {
		writeError(w, http.StatusBadRequest, "policy target must belong to the same owner")
		return
	}
	if err := a.store.Repository().AttachPolicyToTarget(r.Context(), r.PathValue("id"), req.TargetID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleDetachPolicyTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	target, err := a.store.Repository().GetSSHTarget(r.Context(), r.PathValue("target_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if target.OwnerType != policy.OwnerType || target.OwnerID != policy.OwnerID {
		writeError(w, http.StatusBadRequest, "policy target must belong to the same owner")
		return
	}
	if err := a.store.Repository().DetachPolicyFromTarget(r.Context(), policy.ID, target.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAttachPolicyTargetTag(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		OwnerType string `json:"owner_type"`
		OwnerID   string `json:"owner_id"`
		Tag       string `json:"tag"`
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
	if ownerType != policy.OwnerType || ownerID != policy.OwnerID {
		writeError(w, http.StatusBadRequest, "policy tag must belong to the same owner")
		return
	}
	if err := a.store.Repository().AttachPolicyToTargetTag(r.Context(), r.PathValue("id"), ownerType, ownerID, req.Tag); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleDetachPolicyTargetTag(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := a.store.Repository().DetachPolicyFromTargetTag(r.Context(), policy.ID, policy.OwnerType, policy.OwnerID, r.PathValue("tag")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAttachPolicyUserGroup(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	group, err := a.store.Repository().GetOrganizationUserGroup(r.Context(), req.GroupID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if policy.OwnerType != store.OwnerOrganization || group.OrganizationID != policy.OwnerID {
		writeError(w, http.StatusBadRequest, "policy user group must belong to the same organization")
		return
	}
	if err := a.store.Repository().AttachPolicyToUserGroup(r.Context(), r.PathValue("id"), req.GroupID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleDetachPolicyUserGroup(w http.ResponseWriter, r *http.Request, user store.User) {
	policy, err := a.policyForWrite(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	group, err := a.store.Repository().GetOrganizationUserGroup(r.Context(), r.PathValue("group_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if policy.OwnerType != store.OwnerOrganization || group.OrganizationID != policy.OwnerID {
		writeError(w, http.StatusBadRequest, "policy user group must belong to the same organization")
		return
	}
	if err := a.store.Repository().DetachPolicyFromUserGroup(r.Context(), policy.ID, group.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func apiPolicyFromStore(policy store.CommandPolicy) apiPolicy {
	rules := make([]apiPolicyRule, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		rules = append(rules, apiPolicyRule{
			ID:          rule.ID,
			RuleType:    rule.RuleType,
			PatternType: rule.PatternType,
			Pattern:     rule.Pattern,
		})
	}
	return apiPolicy{
		ID:               policy.ID,
		OwnerType:        policy.OwnerType,
		OwnerID:          policy.OwnerID,
		Name:             policy.Name,
		DefaultAction:    policy.DefaultAction,
		LLMConfigID:      policy.LLMConfigID,
		LLMPromptID:      policy.LLMPromptID,
		IPAllowlist:      policy.IPAllowlist,
		AllowPortForward: policy.AllowPortForward,
		AllowUpload:      policy.AllowUpload,
		AllowDownload:    policy.AllowDownload,
		AllowInteractive: policy.AllowInteractive,
		TargetIDs:        policy.TargetIDs,
		UserGroupIDs:     policy.UserGroupIDs,
		TargetTags:       policy.TargetTags,
		Rules:            rules,
	}
}

func apiLLMConfigFromStore(cfg store.LLMPolicyConfig) apiLLMConfig {
	return apiLLMConfig{
		ID:             cfg.ID,
		OwnerType:      cfg.OwnerType,
		OwnerID:        cfg.OwnerID,
		Name:           cfg.Name,
		BaseURL:        cfg.BaseURL,
		Model:          cfg.Model,
		TimeoutSeconds: cfg.TimeoutSeconds,
	}
}

func apiLLMPromptFromStore(prompt store.LLMPromptResource) apiLLMPrompt {
	return apiLLMPrompt{
		ID:         prompt.ID,
		OwnerType:  prompt.OwnerType,
		OwnerID:    prompt.OwnerID,
		Title:      prompt.Title,
		Content:    prompt.Content,
		IsDefault:  prompt.IsDefault,
		IsReadonly: prompt.IsReadonly,
	}
}
