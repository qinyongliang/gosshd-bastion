package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestTunnelAPIPermissionsValidationAndTrafficRanges(t *testing.T) {
	srv, admin, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, admin, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, 200, nil)
	u, err := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"organization_id": org.ID, "name": "API tunnel", "listen_host": "127.0.0.1", "listen_port": freeTunnelPort(t), "destination_host": "127.0.0.1", "destination_port": 80, "duration_seconds": 3600}
	var result struct {
		Tunnel apiTunnel `json:"tunnel"`
	}
	postJSON(t, admin, srv.URL+"/api/tunnels", body, 200, &result)
	id := result.Tunnel.ID
	regular := apiClient(t)
	postJSON(t, regular, srv.URL+"/api/auth/register", map[string]string{"email": "outsider@example.com", "password": "secret-pass"}, 201, nil)
	getJSON(t, regular, srv.URL+"/api/tunnels?organization_id="+org.ID, 403, nil)
	postJSON(t, regular, srv.URL+"/api/tunnels/"+id+"/enable", nil, 403, nil)
	getJSON(t, regular, srv.URL+"/api/tunnels/"+id+"/traffic", 403, nil)
	body["listen_port"] = 70000
	postJSON(t, admin, srv.URL+"/api/tunnels", body, 400, nil)
	body["listen_port"] = 8080
	body["duration_seconds"] = -1
	postJSON(t, admin, srv.URL+"/api/tunnels", body, 400, nil)
	other, err := app.store.Repository().GetUserByEmail(context.Background(), "outsider@example.com")
	if err != nil {
		t.Fatal(err)
	}
	otherOrg, err := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := tunnelAgentTarget(t, app, other, otherOrg, "other-agent")
	body["duration_seconds"] = 0
	body["exit_target_id"] = target.ID
	postJSON(t, admin, srv.URL+"/api/tunnels", body, 400, nil)
	getJSON(t, admin, srv.URL+"/api/tunnels/"+id+"/traffic?from=0&to=9999999999999", 400, nil)
	var history struct {
		Interval int                   `json:"interval_seconds"`
		Buckets  []store.TunnelTraffic `json:"buckets"`
	}
	getJSON(t, admin, srv.URL+"/api/tunnels/"+id+"/traffic", 200, &history)
	if history.Interval != 300 || len(history.Buckets) < 288 {
		t.Fatalf("unexpected traffic range: %+v", history)
	}
	unauth := apiClient(t)
	getJSON(t, unauth, srv.URL+"/api/tunnels?organization_id="+org.ID, http.StatusUnauthorized, nil)
}
