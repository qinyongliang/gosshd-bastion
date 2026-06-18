package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestMCPToolsControlBastionObjects(t *testing.T) {
	app := NewApp(Config{
		DatabasePath: filepath.Join(t.TempDir(), "gosshd.db"),
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.store.Close()
		}
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "gosshd-test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/mcp",
		HTTPClient:           srv.Client(),
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
	if !mcpHasTool(tools, "target_create") || !mcpHasTool(tools, "policy_create") || !mcpHasTool(tools, "policy_bind_target_tag") || !mcpHasTool(tools, "llm_prompt_create") {
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
	if _, err := app.store.Repository().CreateCommandAuditLog(context.Background(), store.CreateCommandAuditLogParams{
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

func mcpHasTool(tools *mcp.ListToolsResult, name string) bool {
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
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
