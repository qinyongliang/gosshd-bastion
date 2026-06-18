package server

import (
	"net/http"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiPolicy struct {
	ID            string   `json:"id"`
	OwnerType     string   `json:"owner_type"`
	OwnerID       string   `json:"owner_id"`
	Name          string   `json:"name"`
	DefaultAction string   `json:"default_action"`
	LLMConfigID   string   `json:"llm_config_id,omitempty"`
	LLMPromptID   string   `json:"llm_prompt_id,omitempty"`
	UserGroupIDs  []string `json:"user_group_ids"`
	TargetTags    []string `json:"target_tags"`
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
		writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusBadRequest, err.Error())
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

func (a *App) handleListLLMPrompts(w http.ResponseWriter, r *http.Request, user store.User) {
	ownerType, ownerID, err := a.resolveOwner(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"), user.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusBadRequest, err.Error())
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

func (a *App) handleListPolicies(w http.ResponseWriter, r *http.Request, user store.User) {
	policies, err := a.store.Repository().ListCommandPolicies(r.Context(), r.URL.Query().Get("owner_type"), r.URL.Query().Get("owner_id"))
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
		OwnerType     string `json:"owner_type"`
		OwnerID       string `json:"owner_id"`
		Name          string `json:"name"`
		DefaultAction string `json:"default_action"`
		LLMConfigID   string `json:"llm_config_id"`
		LLMPromptID   string `json:"llm_prompt_id"`
		PromptTitle   string `json:"prompt_title"`
		PromptContent string `json:"prompt_content"`
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
		OwnerType:     ownerType,
		OwnerID:       ownerID,
		Name:          req.Name,
		DefaultAction: req.DefaultAction,
		LLMConfigID:   req.LLMConfigID,
		LLMPromptID:   llmPromptID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiPolicyResponse{Policy: apiPolicyFromStore(policy)})
}

func (a *App) handleCreatePolicyRule(w http.ResponseWriter, r *http.Request, user store.User) {
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

func (a *App) handleAttachPolicyTarget(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		TargetID string `json:"target_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.store.Repository().AttachPolicyToTarget(r.Context(), r.PathValue("id"), req.TargetID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAttachPolicyTargetTag(w http.ResponseWriter, r *http.Request, user store.User) {
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.Repository().AttachPolicyToTargetTag(r.Context(), r.PathValue("id"), ownerType, ownerID, req.Tag); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAttachPolicyUserGroup(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.store.Repository().AttachPolicyToUserGroup(r.Context(), r.PathValue("id"), req.GroupID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func apiPolicyFromStore(policy store.CommandPolicy) apiPolicy {
	return apiPolicy{
		ID:            policy.ID,
		OwnerType:     policy.OwnerType,
		OwnerID:       policy.OwnerID,
		Name:          policy.Name,
		DefaultAction: policy.DefaultAction,
		LLMConfigID:   policy.LLMConfigID,
		LLMPromptID:   policy.LLMPromptID,
		UserGroupIDs:  policy.UserGroupIDs,
		TargetTags:    policy.TargetTags,
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
