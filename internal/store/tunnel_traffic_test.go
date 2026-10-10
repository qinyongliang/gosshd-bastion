package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestTunnelTrafficSourcesPersistAndRespectRange(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sources.db")
	st, err := OpenAudit(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []TunnelTraffic{
		{SourceIP: "192.0.2.1", BucketStart: 300, RelayUp: 10, ConnectionsOpened: 2, PeakConnections: 2},
		{SourceIP: "192.0.2.1", BucketStart: 300, DirectDown: 20, ConnectionsOpened: 1, PeakConnections: 1},
		{SourceIP: "192.0.2.1", BucketStart: 600, RelayUp: 5, ConnectionsOpened: 3, PeakConnections: 1},
		{SourceIP: "2001:db8::1", BucketStart: 300, DirectUp: 99, ConnectionsOpened: 4, PeakConnections: 4},
		{BucketStart: 300, RelayUp: 10, DirectDown: 20, DirectUp: 99, ConnectionsOpened: 7, PeakConnections: 4},
	} {
		if err := st.Repository().AddTunnelTraffic(ctx, "tunnel", "org", row); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	st, err = OpenAudit(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.Repository().TunnelTrafficSources(ctx, "tunnel", 300, 600, "")
	if err != nil || len(rows) != 2 || rows[0].RelayUp != 10 || rows[0].DirectDown != 20 || rows[0].ConnectionsOpened != 3 || rows[0].PeakConnections != 2 {
		t.Fatalf("range/source aggregation: %+v %v", rows, err)
	}
	rows, err = st.Repository().TunnelTrafficSources(ctx, "tunnel", 300, 900, "192.0.2.1")
	if err != nil || len(rows) != 1 || rows[0].RelayUp != 15 || rows[0].ConnectionsOpened != 6 || rows[0].PeakConnections != 2 {
		t.Fatalf("source filter: %+v %v", rows, err)
	}
	buckets, err := st.Repository().TunnelTrafficForSource(ctx, "tunnel", 300, 600, "2001:db8::1")
	if err != nil || len(buckets) != 1 || buckets[0].DirectUp != 99 {
		t.Fatalf("source buckets: %+v %v", buckets, err)
	}
	total, err := st.Repository().TunnelTrafficTotal(ctx, "tunnel")
	if err != nil || total.ConnectionsOpened != 7 || total.DirectUp != 99 {
		t.Fatalf("global counters counted sources twice: %+v %v", total, err)
	}
}

func TestTunnelSourceMigrationDropsOnlyOldTraffic(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-traffic.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_initial.sql", "0002_tunnel_traffic.sql"} {
		content, err := schemaFiles.ReadFile("audit_migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(content)); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations VALUES (1,'0001_initial.sql','2026-01-01'),(2,'0002_tunnel_traffic.sql','2026-01-01')`,
		`INSERT INTO tunnel_traffic (tunnel_id,organization_id,bucket_start,relay_up) VALUES ('t','o',300,123)`,
		`INSERT INTO tunnel_traffic_totals (tunnel_id,organization_id,relay_up) VALUES ('t','o',123)`,
		`INSERT INTO command_audit_logs (id,user_id,target_id,session_id,command,request_type,policy_decision,policy_reason,started_at) VALUES ('audit','u','t','s','pwd','exec','allow','','2026-01-01')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	st, err := OpenAudit(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, table := range []string{"tunnel_traffic", "tunnel_traffic_totals"} {
		var count int
		if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("old traffic retained in %s: %d %v", table, count, err)
		}
	}
	var command string
	if err := st.DB().QueryRowContext(ctx, "SELECT command FROM command_audit_logs WHERE id='audit'").Scan(&command); err != nil || command != "pwd" {
		t.Fatalf("audit log lost: %s %v", command, err)
	}
	if err := st.Repository().AddTunnelTraffic(ctx, "t", "o", TunnelTraffic{SourceIP: "192.0.2.1", BucketStart: 300, RelayUp: 10}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Repository().TunnelTrafficSources(ctx, "t", 300, 600, "")
	if err != nil || len(rows) != 1 || rows[0].RelayUp != 10 {
		t.Fatalf("migration ran twice: %+v %v", rows, err)
	}
}

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
