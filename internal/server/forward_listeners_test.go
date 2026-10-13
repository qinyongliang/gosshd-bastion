package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestHTTPMultipleListenersServerAndAgent(t *testing.T) {
	for _, atAgent := range []bool{false, true} {
		t.Run(fmt.Sprint("agent=", atAgent), func(t *testing.T) {
			app, u, o := tunnelFixture(t)
			var edge *forwardTestAgent
			entry := ""
			if atAgent {
				edge = attachForwardTestAgent(t, app, "multiple-listener-agent")
				entry = tunnelAgentTarget(t, app, u, o, edge.id).ID
			}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !websocket.IsWebSocketUpgrade(r) {
					w.WriteHeader(201)
					fmt.Fprint(w, r.URL.RequestURI())
					return
				}
				c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer c.Close()
				kind, payload, err := c.ReadMessage()
				if err == nil {
					c.WriteMessage(kind, payload)
				}
			}))
			defer up.Close()
			p1, p2 := freeTunnelPort(t), freeTunnelPort(t)
			for p2 == p1 {
				p2 = freeTunnelPort(t)
			}
			config := store.TunnelConfig{Protocol: "http", Name: "Multiple listeners", EntryTargetID: entry, HTTP: &forward.HTTPConfig{Listeners: []forward.HTTPListener{{Port: p1}, {Host: "127.0.0.1", Port: p2}}, Servers: []forward.ServerBlock{{ID: "site", Hosts: []string{"_"}, Locations: []forward.Location{{ID: "backend", Path: "/", Scheme: "http", Host: "127.0.0.1", Port: up.Listener.Addr().(*net.TCPAddr).Port, ExitTargetID: entry}}}}}}
			view, err := app.saveManagedTunnel(context.Background(), u, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: config})
			if err != nil {
				t.Fatal(err)
			}
			if view.ListenHost != "0.0.0.0" || view.ListenPort != p1 || view.HTTP.Listeners[0].Host != "0.0.0.0" {
				t.Fatal(view.TunnelConfig)
			}
			persisted, err := app.store.Repository().GetTunnel(context.Background(), view.ID)
			if err != nil || len(persisted.HTTP.Listeners) != 2 {
				t.Fatalf("%+v %v", persisted, err)
			}
			enabled, err := app.setManagedTunnelEnabled(context.Background(), u, persisted, true)
			if err != nil {
				t.Fatal(err)
			}
			waitForward(t, app, enabled.Tunnel, func(s tunnelStatus) bool { return s.Status == "running" })
			if atAgent {
				edge.disconnect()
			} // Local edge rules continue after control loss.
			for _, port := range []int{p1, p2} {
				v := enabled.Tunnel
				v.ListenPort = port
				if got := requestForward(t, v, "/multi?q=1"); got != "/multi?q=1" {
					t.Fatal(got)
				}
				c, res, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/socket?q=1", port), nil)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != 101 {
					t.Fatal(res.StatusCode)
				}
				c.SetReadDeadline(time.Now().Add(5 * time.Second))
				c.WriteMessage(websocket.TextMessage, []byte("multiple listener socket"))
				_, got, err := c.ReadMessage()
				c.Close()
				if err != nil || string(got) != "multiple listener socket" {
					t.Fatalf("%q %v", got, err)
				}
			}
			if atAgent {
				edge.connect(t)
			}
			logs := waitForwardLogs(t, app, u, view.ID, 4)
			ws := 0
			for _, e := range logs.Events {
				if e.Status == 101 {
					ws++
					if e.Path != "/socket?q=1" || e.RouteID != "backend" {
						t.Fatal(e)
					}
				}
			}
			if ws != 2 {
				t.Fatal(logs.Events)
			}
			status := app.forwards.status(enabled.Tunnel)
			if !strings.Contains(status.ListenAddress, fmt.Sprint(p1)) || !strings.Contains(status.ListenAddress, fmt.Sprint(p2)) {
				t.Fatal(status)
			}
			if _, err = app.setManagedTunnelEnabled(context.Background(), u, enabled.Tunnel, false); err != nil {
				t.Fatal(err)
			}
			closed := map[int]bool{}
			deadline := time.Now().Add(5 * time.Second)
			for len(closed) < 2 && time.Now().Before(deadline) {
				for _, port := range []int{p1, p2} {
					conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
					if err != nil {
						closed[port] = true
					} else {
						conn.Close()
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			if len(closed) != 2 {
				t.Fatal("stopping configuration did not close every listener", closed)
			}
		})
	}
}
