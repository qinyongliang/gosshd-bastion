package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiMCPToken struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ToolGroups []string `json:"tool_groups"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

type apiMCPTokensResponse struct {
	Tokens []apiMCPToken `json:"tokens"`
}

type apiCreateMCPTokenResponse struct {
	Token      apiMCPToken    `json:"token"`
	TokenValue string         `json:"token_value"`
	MCPJSON    map[string]any `json:"mcp_json"`
}

func (a *App) handleListMCPTokens(w http.ResponseWriter, r *http.Request, user store.User) {
	tokens, err := a.store.Repository().ListMCPTokensForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := apiMCPTokensResponse{}
	for _, token := range tokens {
		out.Tokens = append(out.Tokens, apiMCPTokenFromStore(token))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateMCPToken(w http.ResponseWriter, r *http.Request, user store.User) {
	var req struct {
		Name       string   `json:"name"`
		ToolGroups []string `json:"tool_groups"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > 80 {
		writeError(w, http.StatusBadRequest, "token name must be at most 80 characters")
		return
	}
	value, hash, err := randomMCPToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, err := a.store.Repository().CreateMCPToken(r.Context(), store.CreateMCPTokenParams{
		UserID:     user.ID,
		Name:       name,
		TokenHash:  hash,
		ToolGroups: req.ToolGroups,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiCreateMCPTokenResponse{
		Token:      apiMCPTokenFromStore(token),
		TokenValue: value,
		MCPJSON:    mcpClientJSON(publicBaseURL(r, a.cfg.publicHost()), value),
	})
}

func (a *App) handleDeleteMCPToken(w http.ResponseWriter, r *http.Request, user store.User) {
	if err := a.store.Repository().DeleteMCPToken(r.Context(), user.ID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiMCPTokenFromStore(token store.MCPToken) apiMCPToken {
	out := apiMCPToken{
		ID:         token.ID,
		Name:       token.Name,
		ToolGroups: append([]string(nil), token.ToolGroups...),
		CreatedAt:  token.CreatedAt.Format(time.RFC3339),
	}
	if token.LastUsedAt != nil {
		out.LastUsedAt = token.LastUsedAt.Format(time.RFC3339)
	}
	return out
}

func randomMCPToken() (string, []byte, error) {
	code, _, err := randomCode()
	if err != nil {
		return "", nil, err
	}
	value := "gosshd_mcp_" + code
	return value, codeHash(value), nil
}

func mcpClientJSON(baseURL, token string) map[string]any {
	return map[string]any{
		"mcpServers": map[string]any{
			"gosshd-bastion": map[string]any{
				"url": strings.TrimRight(baseURL, "/") + "/mcp",
				"headers": map[string]string{
					"Authorization": "Bearer " + token,
				},
			},
		},
	}
}
