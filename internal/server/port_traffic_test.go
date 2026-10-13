package server

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestForwardSyncPortDropsLegacyAccessDetailsButKeepsTelemetry(t *testing.T) {
	for _, kind := range []string{"port", "http"} {
		t.Run(kind, func(t *testing.T) {
			app, u, o := tunnelFixture(t)
			ctx := context.Background()
			config, err := app.store.Repository().SaveTunnel(ctx, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: store.TunnelConfig{Protocol: kind, Transports: []string{"tcp"}, Name: "sync policy", ListenHost: "127.0.0.1", ListenPort: 1234, DestinationHost: "localhost", DestinationPort: 80}})
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			cfg := yamux.DefaultConfig()
			cfg.LogOutput = io.Discard
			server, err := yamux.Server(a, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			client, err := yamux.Client(b, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			agentID := "legacy-details"
			app.registry.Register(agentID, server)
			defer app.registry.Unregister(agentID, server)
			now := time.Now()
			stats := forward.Stats{Requests: 1, Up: 70, Down: 80, Peak: 1}
			done := make(chan error, 1)
			go func() {
				s, e := client.AcceptStream()
				if e != nil {
					done <- e
					return
				}
				defer s.Close()
				_, e = protocol.ReadJSONLine[protocol.StreamRequest](bufio.NewReader(s))
				if e != nil {
					done <- e
					return
				}
				e = protocol.WriteJSONLine(s, protocol.ForwardSyncResponse{OK: true, Reports: []protocol.ForwardReport{{ID: config.ID, Revision: config.Revision, Status: "stopped", Stats: stats, Traffic: []forward.TrafficBucket{{Sequence: 1, SourceIP: "192.0.2.1", Bucket: now.Truncate(5 * time.Minute).Unix(), Stats: stats}}, Events: []forward.Event{{ID: uuid.NewString(), Sequence: 42, Time: now.UnixMilli(), SourceIP: "192.0.2.1", Method: "TCP", Path: "/legacy-detail", Up: 70, Down: 80, Concurrent: 1}}}}})
				done <- e
			}()
			app.forwards.syncAgentBatch(agentID, &protocol.ForwardSync{KnownIDs: []string{config.ID}, ReportOnly: true}, map[string]store.Tunnel{config.ID: config})
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			l, err := app.forwards.logStore(config.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := l.Query(ctx, forward.LogFilter{})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if kind == "http" {
				want = 1
			}
			if len(events) != want {
				t.Fatalf("%s stored access rows: %d", kind, len(events))
			}
			total, err := l.Stats(ctx)
			if err != nil || total.Requests != 1 || total.Up != 70 || total.Down != 80 {
				t.Fatalf("telemetry %+v %v", total, err)
			}
			app.forwards.mu.Lock()
			ack := app.forwards.acks[agentID][config.ID]
			trafficAck := app.forwards.trafficAcks[agentID][config.ID]
			app.forwards.mu.Unlock()
			if ack != 42 || trafficAck != 1 {
				t.Fatalf("outbox acknowledgements %d/%d", ack, trafficAck)
			}
		})
	}
}

func TestPortStatisticsAPIAndStorageContainNoAccessDetails(t *testing.T) {
	app, u, o := tunnelFixture(t)
	echo := tunnelEcho(t)
	view, err := app.saveManagedTunnel(context.Background(), u, store.Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: store.TunnelConfig{Protocol: "port", Transports: []string{"tcp"}, Name: "traffic only", ListenHost: "127.0.0.1", ListenPort: freeTunnelPort(t), DestinationHost: "127.0.0.1", DestinationPort: echo.Addr().(*net.TCPAddr).Port}})
	if err != nil {
		t.Fatal(err)
	}
	view, err = app.setManagedTunnelEnabled(context.Background(), u, view.Tunnel, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForward(t, app, view.Tunnel, func(s tunnelStatus) bool { return s.Status == "running" })
	conn, err := net.Dial("tcp", net.JoinHostPort(view.ListenHost, strconv.Itoa(view.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	roundtripTunnel(t, conn)
	conn.Close()
	l, err := app.forwards.logStore(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		l.Flush(context.Background())
		stats, _ := l.Stats(context.Background())
		if stats.Requests == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, err := app.forwardLogs(context.Background(), u, view.ID, forward.LogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Events) != 0 || len(response.Sources) != 0 || response.RetentionDays != 0 || response.Stats.Up != 56000 || response.Stats.Down != 56000 {
		t.Fatalf("port overview %+v", response)
	}
	rows, err := l.Query(context.Background(), forward.LogFilter{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("database details %+v %v", rows, err)
	}
	history, err := app.tunnelTrafficStatistics(context.Background(), view.Tunnel, time.Now().Truncate(store.TunnelTrafficInterval).Unix()-300, time.Now().Unix()+300, "")
	if err != nil || len(history.Sources) != 1 || history.Sources[0].SourceIP != "127.0.0.1" || history.Sources[0].RelayUp != 56000 || history.Sources[0].RelayDown != 56000 {
		t.Fatalf("historical aggregates %+v %v", history, err)
	}
}
