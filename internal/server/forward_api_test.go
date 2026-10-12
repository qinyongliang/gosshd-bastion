package server

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"net"
	"net/http"
	"testing"
)

func TestForwardLogsAndGlobalRetentionAuthorization(t *testing.T) {
	srv, admin, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, admin, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, 200, nil)
	u, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	o, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), u.ID)
	up := httpForwardUpstream(t)
	v := createHTTPForward(t, app, u, o, "", "", up.Listener.Addr().(*net.TCPAddr).Port)
	waitForward(t, app, v, func(s tunnelStatus) bool { return s.Status == "running" })
	requestForward(t, v, "/api/authorization")
	waitForwardLogs(t, app, u, v.ID, 1)
	getJSON(t, admin, srv.URL+"/api/tunnels/"+v.ID+"/logs?path=authorization&status=201", 200, nil)
	getJSON(t, &http.Client{}, srv.URL+"/api/tunnels/"+v.ID+"/logs", 401, nil)
	outsider := apiClient(t)
	postJSON(t, outsider, srv.URL+"/api/auth/register", map[string]string{"email": "forward-outsider@example.com", "password": "secret-pass"}, 201, nil)
	getJSON(t, outsider, srv.URL+"/api/tunnels/"+v.ID+"/logs", 403, nil)
	getJSON(t, outsider, srv.URL+"/api/admin/settings/forward", 403, nil)
	var settings forwardSettings
	getJSON(t, admin, srv.URL+"/api/admin/settings/forward", 200, &settings)
	if settings.RetentionDays != 7 {
		t.Fatal(settings)
	}
	putJSON(t, admin, srv.URL+"/api/admin/settings/forward", map[string]int{"retention_days": 3}, 200, nil)
	getJSON(t, admin, srv.URL+"/api/admin/settings/forward", 200, &settings)
	if settings.RetentionDays != 3 {
		t.Fatal(settings)
	}
	putJSON(t, admin, srv.URL+"/api/admin/settings/forward", map[string]int{"retention_days": 0}, 400, nil)
	getJSON(t, admin, srv.URL+"/api/tunnels/"+v.ID+"/logs?limit=501", 400, nil)
}
func TestMCPHTTPConfigurationAndForwardLogs(t *testing.T) {
	srv, admin, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, admin, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, 200, nil)
	u, _ := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	o, _ := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), u.ID)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "forward-test"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: admin, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"forward_logs", "forward_log_settings_get", "forward_log_settings_update"} {
		if !mcpHasTool(tools, name) {
			t.Fatal("missing MCP tool", name)
		}
	}
	config := forward.HTTPConfig{Servers: []forward.ServerBlock{{ID: "site", Name: "MCP site", Hosts: []string{"api.test"}, Locations: []forward.Location{{ID: "route", Name: "MCP route", Path: "/", Match: "prefix", Scheme: "http", Host: "127.0.0.1", Port: 80}}}}}
	created, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "tunnel_create", Arguments: map[string]any{"organization_id": o.ID, "name": "MCP HTTP", "protocol": "http", "listen_host": "127.0.0.1", "listen_port": freeTunnelPort(t), "http": config}})
	if err != nil || created.IsError {
		t.Fatalf("HTTP MCP create: %+v %v", created, err)
	}
	id := stringField(t, created.StructuredContent, "tunnel", "id")
	saved, _ := app.store.Repository().GetTunnel(context.Background(), id)
	if saved.Protocol != "http" || len(saved.HTTP.Servers) != 1 {
		t.Fatal(saved)
	}
	for _, params := range []*mcp.CallToolParams{{Name: "forward_logs", Arguments: map[string]any{"tunnel_id": id, "path": "/", "limit": 10}}, {Name: "forward_log_settings_get", Arguments: map[string]any{}}, {Name: "forward_log_settings_update", Arguments: map[string]any{"retention_days": 5}}} {
		out, err := session.CallTool(context.Background(), params)
		if err != nil || out.IsError {
			t.Fatalf("%s: %+v %v", params.Name, out, err)
		}
	}
}
