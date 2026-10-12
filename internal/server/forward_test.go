package server

import (
	"context"
	"fmt"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func createHTTPForward(t *testing.T, app *App, u store.User, o store.Organization, entry, exit string, port int) store.Tunnel {
	t.Helper()
	config := store.TunnelConfig{Protocol: "http", Name: "HTTP test", EntryTargetID: entry, ListenHost: "127.0.0.1", ListenPort: freeTunnelPort(t), HTTP: &forward.HTTPConfig{Servers: []forward.ServerBlock{{ID: "site", Name: "API", Hosts: []string{"api.test", "alias.test"}, Locations: []forward.Location{{ID: "api", Match: "^~", Path: "/api/", Scheme: "http", Host: "127.0.0.1", Port: port, ExitTargetID: exit, StripPrefix: true}}}}}}
	view, err := app.saveManagedTunnel(context.Background(), u, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: config})
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := app.setManagedTunnelEnabled(context.Background(), u, view.Tunnel, true)
	if err != nil {
		t.Fatal(err)
	}
	return enabled.Tunnel
}
func waitForward(t *testing.T, app *App, v store.Tunnel, condition func(tunnelStatus) bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		s := app.forwards.status(v)
		if condition(s) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("forward did not converge: %+v", app.forwards.status(v))
}
func requestForward(t *testing.T, v store.Tunnel, path string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", v.ListenPort, path), nil)
	req.Host = "api.test"
	req.Close = true
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, body)
	}
	return string(body)
}
func httpForwardUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		w.WriteHeader(201)
		fmt.Fprint(w, r.URL.RequestURI())
	}))
	t.Cleanup(up.Close)
	return up
}
func waitForwardLogs(t *testing.T, app *App, u store.User, id string, n int) forwardLogsResponse {
	t.Helper()
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		app.forwards.notify()
		out, err := app.forwardLogs(context.Background(), u, id, forward.LogFilter{})
		if err == nil && len(out.Events) >= n {
			return out
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, err := app.forwardLogs(context.Background(), u, id, forward.LogFilter{})
	t.Fatalf("missing logs: %+v %v", out, err)
	return out
}
func TestHTTPServerForwardLogsAndStop(t *testing.T) {
	app, u, o := tunnelFixture(t)
	up := httpForwardUpstream(t)
	v := createHTTPForward(t, app, u, o, "", "", up.Listener.Addr().(*net.TCPAddr).Port)
	waitForward(t, app, v, func(s tunnelStatus) bool { return s.Status == "running" })
	if got := requestForward(t, v, "/api/item?q=1"); got != "/item?q=1" {
		t.Fatal(got)
	}
	logs := waitForwardLogs(t, app, u, v.ID, 1)
	e := logs.Events[0]
	if e.Path != "/api/item?q=1" || e.RouteID != "api" || e.ServerID != "site" || e.Status != 201 || e.SourceIP != "127.0.0.1" || e.Down != 9 {
		t.Fatalf("bad access event %+v", e)
	}
	var count int
	if err := app.store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='access_logs'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("access logs entered main database", err)
	}
	if err := app.audit.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='access_logs'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("access logs entered audit database", err)
	}
	if _, err := app.setManagedTunnelEnabled(context.Background(), u, v, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", v.ListenPort), 100*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("stopped forward still accepts")
}

type forwardTestAgent struct {
	app             *App
	id              string
	client          *agent.Client
	server, session *yamux.Session
	ctx             context.Context
	cancel          context.CancelFunc
}

func attachForwardTestAgent(t *testing.T, app *App, id string) *forwardTestAgent {
	t.Helper()
	dir := t.TempDir()
	client, err := agent.NewEmbedded(agent.Config{Root: dir, IDFile: filepath.Join(dir, "agent.json")}, id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &forwardTestAgent{app: app, id: id, client: client, ctx: ctx, cancel: cancel}
	a.connect(t)
	t.Cleanup(func() { a.disconnect(); cancel(); client.Close() })
	return a
}
func (a *forwardTestAgent) connect(t *testing.T) {
	t.Helper()
	left, right := net.Pipe()
	var err error
	a.server, err = yamux.Server(left, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.session, err = yamux.Client(right, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.app.registry.Register(a.id, a.server)
	go a.app.serveAgentTunnelConnections(a.id, a.server)
	go a.client.ServeSession(a.ctx, a.session)
	a.app.forwards.notify()
}
func (a *forwardTestAgent) disconnect() {
	if a.server != nil {
		a.app.registry.Unregister(a.id, a.server)
		a.server.Close()
		a.session.Close()
	}
}
func TestHTTPAgentDirectSurvivesServerLossAndReportsLogs(t *testing.T) {
	app, u, o := tunnelFixture(t)
	up := httpForwardUpstream(t)
	entryAgent := attachForwardTestAgent(t, app, "http-entry")
	exitAgent := attachForwardTestAgent(t, app, "http-exit")
	entry := tunnelAgentTarget(t, app, u, o, entryAgent.id)
	exit := tunnelAgentTarget(t, app, u, o, exitAgent.id)
	v := createHTTPForward(t, app, u, o, entry.ID, exit.ID, up.Listener.Addr().(*net.TCPAddr).Port)
	waitForward(t, app, v, func(s tunnelStatus) bool { return s.Status == "running" && s.Transport == "direct" })
	if got := requestForward(t, v, "/api/before"); got != "/before" {
		t.Fatal(got)
	}
	entryAgent.disconnect()
	exitAgent.disconnect()
	time.Sleep(100 * time.Millisecond)
	// The upstream closes each connection. These requests require NEW mux streams,
	// rather than merely continuing one socket that was open before disconnection.
	for _, path := range []string{"/api/offline-one", "/api/offline-two"} {
		if got := requestForward(t, v, path); got != path[4:] {
			t.Fatal(got)
		}
	}
	entryAgent.connect(t)
	exitAgent.connect(t)
	logs := waitForwardLogs(t, app, u, v.ID, 3)
	paths := map[string]bool{}
	for _, e := range logs.Events {
		paths[e.Path] = true
	}
	if !paths["/api/offline-one"] || !paths["/api/offline-two"] || logs.Stats.Requests != 3 {
		t.Fatalf("lost/duplicated outage logs: %+v", logs)
	}
	if _, err := app.setManagedTunnelEnabled(context.Background(), u, v, false); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", v.ListenPort), 100*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		app.forwards.notify()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Agent did not stop after reconnect")
}

func TestHTTPDelegatedSSHBehindAgentsSurvivesServerLoss(t *testing.T) {
	app, u, o := tunnelFixture(t)
	up := httpForwardUpstream(t)
	entryAgent := attachForwardTestAgent(t, app, "http-ssh-entry")
	exitAgent := attachForwardTestAgent(t, app, "http-ssh-exit")
	entryRoot := tunnelAgentTarget(t, app, u, o, entryAgent.id)
	exitRoot := tunnelAgentTarget(t, app, u, o, exitAgent.id)
	entry := tunnelSSHTarget(t, app, u, o, "http-entry-ssh", tunnelTestSSH(t), entryRoot.ID)
	exit := tunnelSSHTarget(t, app, u, o, "http-exit-ssh", tunnelTestSSH(t), exitRoot.ID)
	v := createHTTPForward(t, app, u, o, entry.ID, exit.ID, up.Listener.Addr().(*net.TCPAddr).Port)
	waitForward(t, app, v, func(s tunnelStatus) bool { return s.Status == "running" && s.Transport == "direct" })
	if got := requestForward(t, v, "/api/ssh-before"); got != "/ssh-before" {
		t.Fatal(got)
	}
	entryAgent.disconnect()
	exitAgent.disconnect()
	time.Sleep(100 * time.Millisecond)
	if got := requestForward(t, v, "/api/ssh-offline"); got != "/ssh-offline" {
		t.Fatal(got)
	}
}
func TestTCPForwardEdgeMultiplexerSurvivesServerLoss(t *testing.T) {
	app, u, o := tunnelFixture(t)
	echo := tunnelEcho(t)
	entryAgent := attachForwardTestAgent(t, app, "tcp-edge-entry")
	exitAgent := attachForwardTestAgent(t, app, "tcp-edge-exit")
	entry := tunnelAgentTarget(t, app, u, o, entryAgent.id)
	exit := tunnelAgentTarget(t, app, u, o, exitAgent.id)
	view, err := app.saveManagedTunnel(context.Background(), u, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: store.TunnelConfig{Protocol: "port", Transports: []string{"tcp"}, Name: "Edge TCP", EntryTargetID: entry.ID, ExitTargetID: exit.ID, ListenHost: "127.0.0.1", ListenPort: freeTunnelPort(t), DestinationHost: "127.0.0.1", DestinationPort: echo.Addr().(*net.TCPAddr).Port}})
	if err != nil {
		t.Fatal(err)
	}
	view, err = app.setManagedTunnelEnabled(context.Background(), u, view.Tunnel, true)
	if err != nil {
		t.Fatal(err)
	}
	v := view.Tunnel
	waitForward(t, app, v, func(s tunnelStatus) bool { return s.Status == "running" && s.Transport == "direct" })
	entryAgent.disconnect()
	exitAgent.disconnect()
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", v.ListenPort))
		if err != nil {
			t.Fatal(err)
		}
		roundtripTunnel(t, conn)
		conn.Close()
	}
}
