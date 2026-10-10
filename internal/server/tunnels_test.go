package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func tunnelFixture(t *testing.T) (*App, store.User, store.Organization) {
	t.Helper()
	_, _, app := newAPITestServer(t)
	user, err := app.store.Repository().GetUserByEmail(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return app, user, org
}
func freeTunnelPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}
func tunnelEcho(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return l
}
func waitTunnel(t *testing.T, app *App, id string, status string) store.Tunnel {
	t.Helper()
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		v, err := app.store.Repository().GetTunnel(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		s := app.tunnels.status(v)
		if s.Status == status {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	v, _ := app.store.Repository().GetTunnel(context.Background(), id)
	t.Fatalf("tunnel did not become %s: %+v", status, app.tunnels.status(v))
	return v
}
func createTestTunnel(t *testing.T, app *App, user store.User, org store.Organization, entry, exit string, dest int) store.Tunnel {
	t.Helper()
	c := store.TunnelConfig{Name: "test", EntryTargetID: entry, ExitTargetID: exit, ListenHost: "127.0.0.1", ListenPort: freeTunnelPort(t), DestinationHost: "127.0.0.1", DestinationPort: dest}
	v, err := app.store.Repository().SaveTunnel(context.Background(), store.Tunnel{TunnelConfig: c, OrganizationID: org.ID, CreatedBy: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	v, err = app.store.Repository().SetTunnelEnabled(context.Background(), v.ID, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	app.tunnels.notify()
	return waitTunnel(t, app, v.ID, "running")
}
func roundtripTunnel(t *testing.T, c net.Conn) {
	t.Helper()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	payload := bytes.Repeat([]byte("tunnel\x00\xff"), 7000)
	go c.Write(payload)
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, got) {
		t.Fatal("corrupted tunnel payload")
	}
	c.SetDeadline(time.Time{})
}
func TestTunnelRelayStopAndAuditStatistics(t *testing.T) {
	app, user, org := tunnelFixture(t)
	echo := tunnelEcho(t)
	v := createTestTunnel(t, app, user, org, "", "", echo.Addr().(*net.TCPAddr).Port)
	c, err := net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundtripTunnel(t, c)
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		s := app.tunnelAPIStatus(v)
		if s.Traffic.RelayDown > 0 && s.Connections == 1 {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	_, err = app.store.Repository().SetTunnelEnabled(context.Background(), v.ID, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	app.tunnels.stop(v.ID)
	c.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err = c.Read(b[:]); err == nil {
		t.Fatal("stop did not close established TCP")
	}
	total, err := app.audit.Repository().TunnelTrafficTotal(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total.RelayUp != 56000 || total.RelayDown != 56000 || total.ConnectionsOpened != 1 || total.PeakConnections != 1 {
		t.Fatalf("unexpected persisted statistics: %+v", total)
	}
	sources, err := app.audit.Repository().TunnelTrafficSources(context.Background(), v.ID, 0, time.Now().Unix()+300, "")
	if err != nil || len(sources) != 1 || sources[0].SourceIP != "127.0.0.1" || sources[0].RelayUp != 56000 || sources[0].RelayDown != 56000 || sources[0].ConnectionsOpened != 1 {
		t.Fatalf("relay source attribution: %+v %v", sources, err)
	}
	var count int
	err = app.store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='tunnel_traffic'`).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("traffic entered main database")
	}
}
func attachTunnelAgent(t *testing.T, app *App, id string) func() {
	t.Helper()
	a, b := net.Pipe()
	server, err := yamux.Server(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := yamux.Client(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := agent.NewEmbedded(agent.Config{Root: t.TempDir()}, id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.registry.Register(id, server)
	go app.serveAgentTunnelConnections(id, server)
	go embedded.ServeSession(ctx, client)
	app.tunnels.notify()
	stop := func() { cancel(); server.Close(); client.Close(); app.registry.Unregister(id, server) }
	t.Cleanup(stop)
	return stop
}
func tunnelAgentTarget(t *testing.T, app *App, u store.User, o store.Organization, id string) store.SSHTarget {
	t.Helper()
	target, err := app.store.Repository().CreateSSHTarget(context.Background(), store.CreateSSHTargetParams{OwnerType: store.OwnerOrganization, OwnerID: o.ID, TargetType: store.TargetAgent, AgentID: id, Alias: id, Name: id, Host: "127.0.0.1", CreatedBy: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	return target
}
func TestTunnelAgentsP2PAndReconnect(t *testing.T) {
	app, u, o := tunnelFixture(t)
	echo := tunnelEcho(t)
	stop := attachTunnelAgent(t, app, "entry")
	attachTunnelAgent(t, app, "exit")
	entry := tunnelAgentTarget(t, app, u, o, "entry")
	exit := tunnelAgentTarget(t, app, u, o, "exit")
	v := createTestTunnel(t, app, u, o, entry.ID, exit.ID, echo.Addr().(*net.TCPAddr).Port)
	c, err := net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	roundtripTunnel(t, c)
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		state := app.tunnelAPIStatus(v)
		if state.Transport == "direct" && len(state.Paths) > 0 && state.Paths[0].Entry != nil && state.Paths[0].Exit != nil && state.Paths[0].Entry.Local != nil && state.Paths[0].Exit.Local != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if app.tunnelAPIStatus(v).Transport != "direct" {
		t.Fatal("Agent tunnel did not switch to P2P")
	}
	route := app.tunnelAPIStatus(v).Paths
	if len(route) == 0 || route[0].Entry == nil || route[0].Exit == nil || route[0].Entry.Local == nil || route[0].Exit.Local == nil || route[0].Entry.Local.Interface == "" || route[0].Exit.Local.Interface == "" {
		t.Fatalf("both Agents must report the selected interfaces: %+v", route)
	}
	roundtripTunnel(t, c)
	time.Sleep(1200 * time.Millisecond)
	s := app.tunnelAPIStatus(v)
	if s.Traffic.DirectUp == 0 || s.Traffic.DirectDown == 0 {
		t.Fatalf("Agent did not report direct traffic: %+v", s)
	}
	c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for app.tunnelAPIStatus(v).Connections != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	final := app.tunnelAPIStatus(v).Traffic
	if final.RelayUp+final.DirectUp != 112000 || final.RelayDown+final.DirectDown != 112000 {
		t.Fatalf("traffic was lost or duplicated across relay/P2P: %+v", final)
	}
	stats, err := app.tunnelTrafficStatistics(context.Background(), v, time.Now().Truncate(store.TunnelTrafficInterval).Unix()-300, time.Now().Unix()+300, "127.0.0.1")
	if err != nil || len(stats.Sources) != 1 || stats.Sources[0].RelayUp+stats.Sources[0].DirectUp != 112000 || stats.Sources[0].RelayDown+stats.Sources[0].DirectDown != 112000 || stats.Sources[0].ConnectionsOpened != 1 {
		t.Fatalf("Agent relay/P2P source attribution: %+v %v", stats, err)
	}
	stop()
	attachTunnelAgent(t, app, "entry")
	time.Sleep(4 * time.Second)
	waitTunnel(t, app, v.ID, "running")
	c, err = net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundtripTunnel(t, c)
}
func TestTunnelRestartRestoresWithoutLogin(t *testing.T) {
	app, u, o := tunnelFixture(t)
	echo := tunnelEcho(t)
	v := createTestTunnel(t, app, u, o, "", "", echo.Addr().(*net.TCPAddr).Port)
	cfg := app.cfg
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewApp(cfg)
	t.Cleanup(func() { restarted.Close() })
	if err := restarted.ensureServices(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitTunnel(t, restarted, v.ID, "running")
	c, err := net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundtripTunnel(t, c)
}
func TestTunnelExpiryAndCreatorRevocationCloseConnections(t *testing.T) {
	for _, expire := range []bool{true, false} {
		t.Run(strconv.FormatBool(expire), func(t *testing.T) {
			app, u, o := tunnelFixture(t)
			echo := tunnelEcho(t)
			v := createTestTunnel(t, app, u, o, "", "", echo.Addr().(*net.TCPAddr).Port)
			if expire {
				var err error
				v, err = app.store.Repository().SetTunnelEnabled(context.Background(), v.ID, true, 1)
				if err != nil {
					t.Fatal(err)
				}
				app.tunnels.stop(v.ID)
				waitTunnel(t, app, v.ID, "running")
			}
			c, err := net.Dial("tcp", net.JoinHostPort(v.ListenHost, strconv.Itoa(v.ListenPort)))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roundtripTunnel(t, c)
			if !expire {
				if err = app.store.Repository().UpdateUserDisabled(context.Background(), u.ID, true); err != nil {
					t.Fatal(err)
				}
				app.tunnels.notify()
			}
			c.SetReadDeadline(time.Now().Add(4 * time.Second))
			var b [1]byte
			if _, err = c.Read(b[:]); err == nil {
				t.Fatal("expiry/revocation kept connection open")
			}
			status := "expired"
			if !expire {
				status = "error"
			}
			waitTunnel(t, app, v.ID, status)
		})
	}
}
func TestTunnelAgentHalfCloseDeliversCompleteResponse(t *testing.T) {
	for _, kind := range []string{"agent-to-agent", "agent-to-bastion", "bastion-to-agent"} {
		t.Run(kind, func(t *testing.T) {
			app, u, o := tunnelFixture(t)
			attachTunnelAgent(t, app, "half-entry")
			attachTunnelAgent(t, app, "half-exit")
			entry := tunnelAgentTarget(t, app, u, o, "half-entry")
			exit := tunnelAgentTarget(t, app, u, o, "half-exit")
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			go func() {
				c, e := l.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				data, e := io.ReadAll(c)
				if e == nil {
					c.Write(data)
				}
			}()
			entryID, exitID := entry.ID, exit.ID
			if kind == "agent-to-bastion" {
				exitID = ""
			}
			if kind == "bastion-to-agent" {
				entryID = ""
			}
			v := createTestTunnel(t, app, u, o, entryID, exitID, l.Addr().(*net.TCPAddr).Port)
			c, err := net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.ParseIP(v.ListenHost), Port: v.ListenPort})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(15 * time.Second))
			payload := bytes.Repeat([]byte("half-close response\x00"), 60000)
			go func() { c.Write(payload); c.CloseWrite() }()
			response, err := io.ReadAll(c)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, response) {
				t.Fatalf("response truncated: got %d want %d", len(response), len(payload))
			}
		})
	}
}
