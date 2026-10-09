package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTunnelTrafficAuditBucketsTotalsAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audit.db")
	st, err := OpenAudit(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	bucket := time.Now().UTC().Truncate(TunnelTrafficInterval).Unix()
	a := TunnelTraffic{BucketStart: bucket, RelayUp: 50, RelayDown: 100, ConnectionsOpened: 2, PeakConnections: 2}
	b := TunnelTraffic{BucketStart: bucket, DirectUp: 75, DirectDown: 125, ConnectionsOpened: 1, PeakConnections: 3}
	for _, v := range []TunnelTraffic{a, b, {BucketStart: bucket + 300, RelayUp: 20, PeakConnections: 1}} {
		if err = st.Repository().AddTunnelTraffic(ctx, "tunnel", "org", v); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	st, err = OpenAudit(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	total, err := st.Repository().TunnelTrafficTotal(ctx, "tunnel")
	if err != nil {
		t.Fatal(err)
	}
	if total.RelayUp != 70 || total.DirectDown != 125 || total.ConnectionsOpened != 3 || total.PeakConnections != 3 {
		t.Fatalf("invalid totals: %+v", total)
	}
	rows, err := st.Repository().TunnelTraffic(ctx, "tunnel", bucket, bucket+300)
	if err != nil || len(rows) != 1 || rows[0].DirectUp != 75 || rows[0].RelayUp != 50 {
		t.Fatalf("invalid time buckets: %+v %v", rows, err)
	}
}
func TestTunnelConfigurationPersistsDeadlineAndExpiry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "main.db")
	st, err := Open(ctx, path, []byte("tunnel-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := temporarySSHFixture(t, st)
	o, err := st.Repository().GetPersonalOrganizationForUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.Repository().SaveTunnel(ctx, Tunnel{OrganizationID: o.ID, CreatedBy: u.ID, TunnelConfig: TunnelConfig{Name: "persist", ListenHost: "127.0.0.1", ListenPort: 8080, DestinationHost: "db", DestinationPort: 5432, DurationSeconds: 3600}})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := st.Repository().CreateUser(ctx, CreateUserParams{Email: "operator@example.com", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	v, err = st.Repository().EnableTunnelForUser(ctx, v.ID, v.DurationSeconds, operator.ID)
	if err != nil {
		t.Fatal(err)
	}
	expiry := *v.ExpiresAt
	st.Close()
	st, err = Open(ctx, path, []byte("tunnel-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	loaded, err := st.Repository().GetTunnel(ctx, v.ID)
	if err != nil || !loaded.Enabled || loaded.ExpiresAt == nil || !loaded.ExpiresAt.Equal(expiry) || loaded.OperatorID() != operator.ID || loaded.CreatedBy != u.ID {
		t.Fatalf("deadline changed on reopen: %+v %v", loaded, err)
	}
	if err = st.Repository().ExpireTunnels(ctx, expiry.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	loaded, err = st.Repository().GetTunnel(ctx, v.ID)
	if err != nil || loaded.Enabled || loaded.ExpiresAt == nil {
		t.Fatal("expired tunnel remained enabled")
	}
}
