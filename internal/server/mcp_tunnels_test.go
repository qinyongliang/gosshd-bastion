package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestMCPTunnelLifecycleAndScopedToken(t *testing.T) {
	srv, admin, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, admin, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, 200, nil)
	actor, err := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	var token apiCreateMCPTokenResponse
	postJSON(t, admin, srv.URL+"/api/mcp-tokens", map[string]any{"name": "tunnels", "tool_groups": []string{"tunnel"}}, 201, &token)
	if len(token.Token.ToolGroups) != 1 || token.Token.ToolGroups[0] != "tunnel" {
		t.Fatalf("group not retained: %+v", token)
	}
	connect := func(client *http.Client) *mcp.ClientSession {
		t.Helper()
		s, err := mcp.NewClient(&mcp.Implementation{Name: "tunnel-test"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: client, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	session := connect(&http.Client{Transport: bearerRoundTripper{token: token.TokenValue}})
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 8 || mcpHasTool(tools, "auth_register") || mcpHasTool(tools, "session_send_command") {
		t.Fatalf("unexpected tunnel-only tools: %+v", tools.Tools)
	}
	call := func(name string, args map[string]any, wantError bool) *mcp.CallToolResult {
		t.Helper()
		r, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError != wantError {
			t.Fatalf("%s error=%v result=%+v", name, r.IsError, r)
		}
		return r
	}
	body := map[string]any{"organization_id": org.ID, "name": "MCP tunnel", "listen_host": "127.0.0.1", "listen_port": freeTunnelPort(t), "destination_host": "127.0.0.1", "destination_port": 80, "duration_seconds": 3600}
	created := call("tunnel_create", body, false)
	id := stringField(t, created.StructuredContent, "tunnel", "id")
	config, err := app.store.Repository().GetTunnel(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if config.Enabled || config.CreatedBy != actor.ID {
		t.Fatalf("wrong creator/default: %+v", config)
	}
	call("tunnel_update", map[string]any{"tunnel_id": id, "name": "Renamed", "destination_port": 8081}, false)
	updated, _ := app.store.Repository().GetTunnel(context.Background(), id)
	if updated.Name != "Renamed" || updated.DestinationPort != 8081 || updated.ListenPort != config.ListenPort || updated.DurationSeconds != 3600 {
		t.Fatalf("patch lost fields: %+v", updated)
	}
	call("tunnel_update", map[string]any{"tunnel_id": id, "listen_port": 70000}, true)
	after, _ := app.store.Repository().GetTunnel(context.Background(), id)
	if after.Revision != updated.Revision {
		t.Fatal("invalid update mutated configuration")
	}
	call("tunnel_list", map[string]any{"organization_id": org.ID}, false)
	call("tunnel_enable", map[string]any{"tunnel_id": id}, false)
	enabled, _ := app.store.Repository().GetTunnel(context.Background(), id)
	if !enabled.Enabled || enabled.EnabledBy != actor.ID || enabled.ExpiresAt == nil {
		t.Fatalf("enable attribution: %+v", enabled)
	}
	call("tunnel_update", map[string]any{"tunnel_id": id, "duration_seconds": 0}, false)
	after, _ = app.store.Repository().GetTunnel(context.Background(), id)
	if after.ExpiresAt == nil || !after.ExpiresAt.Equal(*enabled.ExpiresAt) {
		t.Fatal("patch reset expiry")
	}
	call("tunnel_get", map[string]any{"tunnel_id": id}, false)
	call("tunnel_traffic", map[string]any{"tunnel_id": id}, false)
	call("tunnel_traffic", map[string]any{"tunnel_id": id, "from": 0, "to": 9999999999999}, true)
	call("tunnel_stop", map[string]any{"tunnel_id": id}, false)
	after, _ = app.store.Repository().GetTunnel(context.Background(), id)
	if after.Enabled {
		t.Fatal("stop did not persist")
	}
	outsider := apiClient(t)
	postJSON(t, outsider, srv.URL+"/api/auth/register", map[string]string{"email": "mcp-outsider@example.com", "password": "secret-pass"}, 201, nil)
	foreign := connect(outsider)
	for _, name := range []string{"tunnel_get", "tunnel_update", "tunnel_enable", "tunnel_stop", "tunnel_delete", "tunnel_traffic"} {
		r, err := foreign.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"tunnel_id": id}})
		if err != nil {
			t.Fatal(err)
		}
		if !r.IsError {
			t.Fatalf("outsider allowed %s", name)
		}
	}
	temporary, finish := app.registerSSHForward(actor.ID, "SHA256:key", tunnelAgentTarget(t, app, actor, org, "mcp-temporary"), "192.0.2.3:22", "local", "localhost", 80)
	defer finish()
	call("tunnel_list", map[string]any{"organization_id": org.ID}, false)
	call("tunnel_get", map[string]any{"tunnel_id": temporary.config.ID}, false)
	call("tunnel_traffic", map[string]any{"tunnel_id": temporary.config.ID}, false)
	call("tunnel_enable", map[string]any{"tunnel_id": temporary.config.ID}, true)
	call("tunnel_update", map[string]any{"tunnel_id": temporary.config.ID, "name": "must not persist"}, true)
	call("tunnel_stop", map[string]any{"tunnel_id": temporary.config.ID}, false)
	finish()
	call("tunnel_delete", map[string]any{"tunnel_id": id}, false)
	if _, err := app.store.Repository().GetTunnel(context.Background(), id); err != store.ErrNotFound {
		t.Fatalf("delete failed: %v", err)
	}
	if err := app.store.Repository().UpdateUserDisabled(context.Background(), actor.ID, true); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token.TokenValue)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled bearer account allowed: %d", response.StatusCode)
	}
}
