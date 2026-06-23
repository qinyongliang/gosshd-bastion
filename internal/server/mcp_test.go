package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestMCPToolsControlBastionObjects(t *testing.T) {
	app := NewApp(Config{
		DatabasePath:           filepath.Join(t.TempDir(), "gosshd.db"),
		BootstrapAdminPassword: "admin-pass",
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})

	httpClient := apiClient(t)
	postJSON(t, httpClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, nil)

	client := mcp.NewClient(&mcp.Implementation{Name: "gosshd-test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/mcp",
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mcpHasTool(tools, "target_create") || !mcpHasTool(tools, "target_delete") || !mcpHasTool(tools, "policy_create") || !mcpHasTool(tools, "policy_bind_target_tag") || !mcpHasTool(tools, "llm_prompt_create") {
		t.Fatalf("expected bastion tools, got %+v", tools.Tools)
	}

	reg, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "auth_register",
		Arguments: map[string]any{
			"email":        "mcp@example.com",
			"display_name": "MCP",
			"password":     "secret-pass",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	userID := stringField(t, reg.StructuredContent, "user", "id")
	if userID == "" {
		t.Fatalf("missing user id in %#v", reg.StructuredContent)
	}

	createdOrg, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "org_create",
		Arguments: map[string]any{
			"user_id": userID,
			"name":    "Ops",
			"slug":    "ops-mcp",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	orgID := stringField(t, createdOrg.StructuredContent, "organization", "id")
	if orgID == "" {
		t.Fatalf("missing org id in %#v", createdOrg.StructuredContent)
	}
	listedOrgs, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "org_list",
		Arguments: map[string]any{
			"user_id": userID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsStructuredID(listedOrgs.StructuredContent, "organizations", orgID) {
		t.Fatalf("org_list missing created organization: %#v", listedOrgs.StructuredContent)
	}

	missingOwner, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "target_create",
		Arguments: map[string]any{
			"user_id":         userID,
			"name":            "No owner",
			"alias":           "no-owner",
			"target_type":     "direct",
			"host":            "127.0.0.1",
			"port":            22,
			"remote_username": "root",
			"auth_type":       "password",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if missingOwner == nil || !missingOwner.IsError {
		t.Fatalf("target_create without owner scope should return MCP error, got %#v", missingOwner)
	}

	secondReg, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "auth_register",
		Arguments: map[string]any{
			"email":        "mcp-join@example.com",
			"display_name": "MCP Join",
			"password":     "secret-pass",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondUserID := stringField(t, secondReg.StructuredContent, "user", "id")
	invite, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "org_invite_create",
		Arguments: map[string]any{
			"user_id":         userID,
			"organization_id": orgID,
			"role":            store.RoleMember,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inviteCode := stringField(t, invite.StructuredContent, "code")
	if inviteCode == "" {
		t.Fatalf("missing invite code in %#v", invite.StructuredContent)
	}
	joined, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "org_join",
		Arguments: map[string]any{
			"user_id": secondUserID,
			"code":    inviteCode,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if joinedOrg := stringField(t, joined.StructuredContent, "organization", "id"); joinedOrg != orgID {
		t.Fatalf("joined org mismatch: %q in %#v", joinedOrg, joined.StructuredContent)
	}

	signer := testMCPSigner(t)
	key, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "public_key_add",
		Arguments: map[string]any{
			"user_id":        userID,
			"name":           "mcp-key",
			"authorized_key": string(gossh.MarshalAuthorizedKey(signer.PublicKey())),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint := stringField(t, key.StructuredContent, "key", "fingerprint"); fingerprint != gossh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("public key fingerprint mismatch: %q", fingerprint)
	}

	target, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "target_create",
		Arguments: map[string]any{
			"user_id":         userID,
			"owner_type":      "organization",
			"owner_id":        orgID,
			"name":            "Test service",
			"alias":           "test2",
			"target_type":     "direct",
			"host":            "127.0.0.1",
			"port":            22,
			"remote_username": "root",
			"auth_type":       "password",
			"secret":          "secret",
			"tags":            []string{"测试环境"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if alias := stringField(t, target.StructuredContent, "target", "alias"); alias != "test2" {
		t.Fatalf("target alias mismatch: %q in %#v", alias, target.StructuredContent)
	}
	if name := stringField(t, target.StructuredContent, "target", "name"); name != "Test service" {
		t.Fatalf("target name mismatch: %q in %#v", name, target.StructuredContent)
	}
	targetID := stringField(t, target.StructuredContent, "target", "id")
	listedTargets, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "target_list",
		Arguments: map[string]any{
			"user_id":    userID,
			"owner_type": "organization",
			"owner_id":   orgID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if command := targetConnectionCommandFromStructured(listedTargets.StructuredContent, targetID); !strings.HasPrefix(command, "ssh -p 22 test2@") {
		t.Fatalf("target_list connection command mismatch: %q in %#v", command, listedTargets.StructuredContent)
	}
	llmConfig, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "llm_config_create",
		Arguments: map[string]any{
			"user_id":         userID,
			"owner_type":      "organization",
			"owner_id":        orgID,
			"name":            "mcp-reviewer",
			"base_url":        "https://llm.example.test/v1",
			"api_key":         "secret",
			"model":           "deepseek-flash",
			"timeout_seconds": 9,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	llmConfigID := stringField(t, llmConfig.StructuredContent, "config", "id")
	prompt, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "llm_prompt_create",
		Arguments: map[string]any{
			"user_id":    userID,
			"owner_type": "organization",
			"owner_id":   orgID,
			"title":      "MCP prompt",
			"content":    "Allow readonly commands only.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	promptID := stringField(t, prompt.StructuredContent, "prompt", "id")
	policy, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "policy_create",
		Arguments: map[string]any{
			"user_id":        userID,
			"owner_type":     "organization",
			"owner_id":       orgID,
			"name":           "MCP readonly",
			"default_action": store.DecisionDeny,
			"llm_config_id":  llmConfigID,
			"llm_prompt_id":  promptID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	policyID := stringField(t, policy.StructuredContent, "policy", "id")
	for _, call := range []mcp.CallToolParams{
		{
			Name: "policy_rule_add",
			Arguments: map[string]any{
				"policy_id":    policyID,
				"rule_type":    store.RuleWhitelist,
				"pattern_type": store.PatternExact,
				"pattern":      "whoami",
			},
		},
		{
			Name: "policy_bind_target",
			Arguments: map[string]any{
				"policy_id": policyID,
				"target_id": targetID,
			},
		},
		{
			Name: "policy_bind_target_tag",
			Arguments: map[string]any{
				"user_id":    userID,
				"owner_type": "organization",
				"owner_id":   orgID,
				"policy_id":  policyID,
				"tag":        "测试环境",
			},
		},
	} {
		if _, err := session.CallTool(context.Background(), &call); err != nil {
			t.Fatalf("%s failed: %v", call.Name, err)
		}
	}
	defaultGroup, err := app.store.Repository().GetDefaultOrganizationUserGroup(context.Background(), orgID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "policy_bind_user_group",
		Arguments: map[string]any{
			"policy_id": policyID,
			"group_id":  defaultGroup.ID,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.createAuditLog(context.Background(), store.CreateCommandAuditLogParams{
		UserID:         userID,
		TargetID:       targetID,
		OrganizationID: orgID,
		SessionID:      "mcp-session",
		Command:        "whoami",
		RequestType:    store.RequestExec,
		PolicyDecision: store.DecisionAllow,
		PolicyReason:   "mcp whitelist",
	}); err != nil {
		t.Fatal(err)
	}
	audit, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit_list",
		Arguments: map[string]any{
			"user_id": userID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsStructuredField(audit.StructuredContent, "logs", "command", "whoami") {
		t.Fatalf("audit_list missing whoami audit: %#v", audit.StructuredContent)
	}
	deletedTarget, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "target_delete",
		Arguments: map[string]any{
			"user_id":   userID,
			"target_id": targetID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deletedTarget.IsError {
		t.Fatalf("target_delete returned MCP error: %#v", deletedTarget)
	}
	if _, err := app.store.Repository().GetSSHTarget(context.Background(), targetID); err == nil {
		t.Fatalf("target_delete did not remove target %s", targetID)
	}
	policyAfterTargetDelete, err := app.store.Repository().GetCommandPolicy(context.Background(), policyID)
	if err != nil {
		t.Fatal(err)
	}
	for _, boundTargetID := range policyAfterTargetDelete.TargetIDs {
		if boundTargetID == targetID {
			t.Fatalf("target_delete should remove policy target binding: %#v", policyAfterTargetDelete.TargetIDs)
		}
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "org_leave",
		Arguments: map[string]any{
			"user_id":         secondUserID,
			"organization_id": orgID,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPRequiresAuthentication(t *testing.T) {
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "gosshd-test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/mcp",
		HTTPClient:           srv.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		_ = session.Close()
		t.Fatalf("expected unauthenticated MCP connection to fail")
	}
}

func TestMCPAcceptsUserToken(t *testing.T) {
	srv, httpClient, _ := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, httpClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, nil)

	var created apiCreateMCPTokenResponse
	postJSON(t, httpClient, srv.URL+"/api/mcp-tokens", map[string]string{
		"name": "codex",
	}, http.StatusCreated, &created)
	if created.Token.ID == "" || created.TokenValue == "" || created.MCPJSON["mcpServers"] == nil {
		t.Fatalf("mcp token create response mismatch: %+v", created)
	}
	if created.Token.TokenValue != created.TokenValue {
		t.Fatalf("expected created token response to include token value, got token=%q value=%q", created.Token.TokenValue, created.TokenValue)
	}
	if len(created.Token.ToolGroups) != 1 || created.Token.ToolGroups[0] != "session" {
		t.Fatalf("expected mcp token to default to session tools, got %+v", created.Token.ToolGroups)
	}

	bearerHTTPClient := &http.Client{Transport: bearerRoundTripper{token: created.TokenValue}}
	client := mcp.NewClient(&mcp.Implementation{Name: "gosshd-token-test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/mcp",
		HTTPClient:           bearerHTTPClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if init := session.InitializeResult(); init == nil || !strings.Contains(init.Instructions, "prefer the session tool group") {
		t.Fatalf("expected MCP instructions to prefer session tools, got %+v", init)
	}
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mcpHasTool(tools, "session_list") {
		t.Fatalf("expected session_list tool with default token auth, got %+v", tools.Tools)
	}
	if description := mcpToolDescription(tools, "session_send_command"); !strings.Contains(description, "Preferred tool for running commands on remote servers") {
		t.Fatalf("expected session_send_command to prefer session use, got %q", description)
	}
	if mcpHasTool(tools, "org_list") {
		t.Fatalf("did not expect org_list tool with default session-only token auth, got %+v", tools.Tools)
	}
	_ = session.Close()

	var listed apiMCPTokensResponse
	getJSON(t, httpClient, srv.URL+"/api/mcp-tokens", http.StatusOK, &listed)
	if len(listed.Tokens) != 1 || listed.Tokens[0].LastUsedAt == "" {
		t.Fatalf("mcp token last_used_at was not updated: %+v", listed)
	}
	if listed.Tokens[0].TokenValue != created.TokenValue {
		t.Fatalf("mcp token list did not expose token value: %+v", listed.Tokens[0])
	}
	var updated struct {
		Token apiMCPToken `json:"token"`
	}
	patchJSON(t, httpClient, srv.URL+"/api/mcp-tokens/"+created.Token.ID, map[string]any{
		"tool_groups": []string{"session", "audit"},
	}, http.StatusOK, &updated)
	if len(updated.Token.ToolGroups) != 2 || updated.Token.ToolGroups[0] != "session" || updated.Token.ToolGroups[1] != "audit" {
		t.Fatalf("mcp token tool groups were not updated: %+v", updated.Token.ToolGroups)
	}
	if updated.Token.TokenValue != created.TokenValue {
		t.Fatalf("mcp token update did not expose token value: %+v", updated.Token)
	}
	getJSON(t, httpClient, srv.URL+"/api/mcp-tokens", http.StatusOK, &listed)
	if len(listed.Tokens) != 1 || len(listed.Tokens[0].ToolGroups) != 2 || listed.Tokens[0].ToolGroups[1] != "audit" {
		t.Fatalf("mcp token list did not reflect updated tool groups: %+v", listed)
	}

	deleteJSON(t, httpClient, srv.URL+"/api/mcp-tokens/"+created.Token.ID, http.StatusNoContent)
	session, err = client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/mcp",
		HTTPClient:           bearerHTTPClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		_ = session.Close()
		t.Fatalf("expected deleted mcp token to be rejected")
	}
}

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	next := req.Clone(req.Context())
	next.Header.Set("Authorization", "Bearer "+b.token)
	return base.RoundTrip(next)
}

func mcpHasTool(tools *mcp.ListToolsResult, name string) bool {
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func mcpToolDescription(tools *mcp.ListToolsResult, name string) string {
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return tool.Description
		}
	}
	return ""
}

func stringField(t *testing.T, v any, keys ...string) string {
	t.Helper()
	current := v
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	out, _ := current.(string)
	return out
}

func containsStructuredID(v any, listKey, id string) bool {
	return containsStructuredField(v, listKey, "id", id)
}

func containsStructuredField(v any, listKey, field, value string) bool {
	root, ok := v.(map[string]any)
	if !ok {
		return false
	}
	items, ok := root[listKey].([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := m[field].(string); got == value {
			return true
		}
	}
	return false
}

func targetConnectionCommandFromStructured(v any, targetID string) string {
	root, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	items, ok := root["targets"].([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		target, ok := m["target"].(map[string]any)
		if !ok {
			continue
		}
		if got, _ := target["id"].(string); got == targetID {
			command, _ := m["connection_command"].(string)
			return command
		}
	}
	return ""
}

func testMCPSigner(t *testing.T) gossh.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
