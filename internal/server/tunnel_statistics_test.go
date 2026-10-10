package server

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestTunnelSourceAddressNormalization(t *testing.T) {
	for address, want := range map[string]string{
		"192.0.2.1:1234": "192.0.2.1", "[2001:db8::1]:1234": "2001:db8::1",
		"::ffff:192.0.2.1": "192.0.2.1", "[fe80::1%eth0]:1234": "fe80::1",
		"": "unknown", "agent:old": "unknown",
	} {
		if got := tunnelSourceIP(address); got != want {
			t.Errorf("%q: got %q want %q", address, got, want)
		}
	}
}

func TestTunnelSourceStatisticsLiveFlushFilterAndConcurrency(t *testing.T) {
	srv, admin, app := newAPITestServer(t)
	defer srv.Close()
	ctx := context.Background()
	postJSON(t, admin, srv.URL+"/api/auth/login", map[string]string{"email": "admin", "password": "admin-pass"}, 200, nil)
	user, err := app.store.Repository().GetUserByEmail(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := app.store.Repository().SaveTunnel(ctx, store.Tunnel{OrganizationID: org.ID, CreatedBy: user.ID, TunnelConfig: store.TunnelConfig{Name: "sources", ListenHost: "127.0.0.1", ListenPort: 8080, DestinationHost: "localhost", DestinationPort: 80}})
	if err != nil {
		t.Fatal(err)
	}
	metrics := newTunnelMetrics(config.ID, org.ID)
	app.tunnels.mu.Lock()
	app.tunnels.metrics[config.ID] = metrics
	app.tunnels.mu.Unlock()
	bucket := time.Now().Truncate(store.TunnelTrafficInterval).Unix()
	for range 2 {
		metrics.opened("192.0.2.1")
	}
	metrics.bytes("192.0.2.1", true, false, 100)
	metrics.bytes("192.0.2.1", false, true, 200)
	for range 2 {
		metrics.closed("192.0.2.1")
	}
	for range 3 {
		metrics.opened("2001:db8::1")
	}
	metrics.bytes("2001:db8::1", true, true, 500)
	// An excluded historical bucket must not enter the selected source totals.
	if err := app.audit.Repository().AddTunnelTraffic(ctx, config.ID, org.ID, store.TunnelTraffic{SourceIP: "192.0.2.1", BucketStart: bucket - 300, RelayUp: 9999}); err != nil {
		t.Fatal(err)
	}
	endpoint := fmt.Sprintf("%s/api/tunnels/%s/traffic?from=%d&to=%d", srv.URL, config.ID, bucket, bucket+300)
	var before tunnelTrafficStatistics
	getJSON(t, admin, endpoint, 200, &before)
	if len(before.Buckets) != 1 || len(before.Sources) != 2 || before.ActiveConnections != 3 || before.Buckets[0].ConnectionsOpened != 5 || before.Buckets[0].PeakConnections != 3 || before.Buckets[0].RelayUp != 100 || before.Buckets[0].DirectUp != 500 || before.Buckets[0].DirectDown != 200 {
		t.Fatalf("invalid global/live statistics: %+v", before)
	}
	if before.Sources[0].ConnectionsOpened != 2 || before.Sources[0].PeakConnections != 2 || before.Sources[0].ActiveConnections != 0 || before.Sources[1].ActiveConnections != 3 {
		t.Fatalf("invalid source counters: %+v", before.Sources)
	}
	metrics.flush(app.audit.Repository(), true)
	var after tunnelTrafficStatistics
	getJSON(t, admin, endpoint, 200, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("flush lost or duplicated statistics: before=%+v after=%+v", before, after)
	}
	metrics.bytes("192.0.2.1", false, false, 50)
	var filtered tunnelTrafficStatistics
	getJSON(t, admin, endpoint+"&source_ip=192.0.2.1", 200, &filtered)
	if len(filtered.Sources) != 1 || filtered.ActiveConnections != 0 || filtered.Buckets[0].RelayUp != 100 || filtered.Buckets[0].RelayDown != 50 || filtered.Buckets[0].DirectUp != 0 || filtered.Buckets[0].PeakConnections != 2 {
		t.Fatalf("source filter did not merge persisted/live: %+v", filtered)
	}
	getJSON(t, admin, endpoint+"&source_ip=2001:0db8::1", 200, &filtered)
	if len(filtered.Sources) != 1 || filtered.Sources[0].SourceIP != "2001:db8::1" || filtered.ActiveConnections != 3 {
		t.Fatalf("IPv6 filter: %+v", filtered)
	}
	getJSON(t, admin, endpoint+"&source_ip=192.0.2.99", 200, &filtered)
	if len(filtered.Sources) != 0 || filtered.ActiveConnections != 0 || !filtered.Buckets[0].Empty() {
		t.Fatalf("missing source leaked other IPs: %+v", filtered)
	}
	getJSON(t, admin, endpoint+"&source_ip=not-an-ip", 400, nil)
	getJSON(t, admin, endpoint+"&source_ip=192.0.2.1:1234", 400, nil)
	getJSON(t, admin, endpoint+"&source_ip=unknown", 200, nil)
	for range 3 {
		metrics.closed("2001:db8::1")
	}
}
