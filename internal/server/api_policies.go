package server

import (
	"net/http"

	"github.com/qinyongliang/gosshd/internal/store"
)

type apiPolicy struct {
	ID            string   `json:"id"`
	OwnerType     string   `json:"owner_type"`
	OwnerID       string   `json:"owner_id"`
	Name          string   `json:"name"`
	DefaultAction string   `json:"default_action"`
	UserGroupIDs  []string `json:"user_group_ids"`
}

type apiPolicyResponse struct {
	Policy apiPolicy `json:"policy"`
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
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ownerType, ownerID := resolveOwner(req.OwnerType, req.OwnerID, user.ID)
	policy, err := a.store.Repository().CreateCommandPolicy(r.Context(), store.CreateCommandPolicyParams{
		OwnerType:     ownerType,
		OwnerID:       ownerID,
		Name:          req.Name,
		DefaultAction: req.DefaultAction,
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
		UserGroupIDs:  policy.UserGroupIDs,
	}
}
