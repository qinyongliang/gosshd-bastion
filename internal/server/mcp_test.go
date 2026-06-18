package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	if !mcpHasTool(tools, "target_create") || !mcpHasTool(tools, "policy_create") || !mcpHasTool(tools, "llm_prompt_create") {
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

	target, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "target_create",
		Arguments: map[string]any{
			"user_id":         userID,
			"owner_type":      "organization",
			"owner_id":        orgID,
			"alias":           "test2",
			"target_type":     "direct",
			"host":            "127.0.0.1",
			"port":            22,
			"remote_username": "root",
			"auth_type":       "password",
			"secret":          "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if alias := stringField(t, target.StructuredContent, "target", "alias"); alias != "test2" {
		t.Fatalf("target alias mismatch: %q in %#v", alias, target.StructuredContent)
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
