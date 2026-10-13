package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPortTunnelMigrationPreservesConfigurationAndIsRepeatable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v5.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 5; version++ {
		names := []string{"0001_initial.sql", "0002_target_delete_indexes.sql", "0003_user_group_targets.sql", "0004_temporary_ssh_authorizations.sql", "0005_tunnels.sql"}
		content, e := schemaFiles.ReadFile("migrations/" + names[version-1])
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(content)); e != nil {
			t.Fatal(e)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version, names[version-1], "2026-01-01"); err != nil {
			t.Fatal(err)
		}
	}
	// Foreign keys are off on this seed connection; Open will re-enable them.
	for id, config := range map[string]string{
		"implicit": `{"name":"implicit","listen_host":"127.0.0.1","listen_port":8080,"destination_host":"db","destination_port":5432,"duration_seconds":3600}`,
		"empty":    `{"protocol":"","name":"empty"}`,
		"tcp":      `{"protocol":"tcp","name":"tcp"}`,
		"http":     `{"protocol":"http","name":"http","listen_host":"127.0.0.1","listen_port":8084,"http":{"servers":[]}}`,
	} {
		if _, e := db.Exec(`INSERT INTO tunnels(id,organization_id,created_by,config,enabled,expires_at,revision,created_at,updated_at) VALUES(?, 'org', 'user', ?, 1, 999999999999999999, 7, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, id, config); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	for i := 0; i < 2; i++ {
		st, e := Open(ctx, path)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { st.Close() })
		for _, id := range []string{"implicit", "empty", "tcp", "http"} {
			row, e := st.Repository().GetTunnel(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			if id == "http" {
				if row.Protocol != "http" || row.Revision != 9 || row.HTTP == nil || len(row.HTTP.Listeners) != 1 || row.HTTP.Listeners[0].Host != "127.0.0.1" || row.HTTP.Listeners[0].Port != 8084 {
					t.Fatalf("HTTP changed %+v", row)
				}
			} else {
				if row.Protocol != "port" || !reflect.DeepEqual(row.Transports, []string{"tcp"}) || row.Revision != 8 || !row.Enabled || row.ExpiresAt == nil {
					t.Fatalf("migration %+v", row)
				}
			}
			if id == "implicit" && (row.DestinationHost != "db" || row.DestinationPort != 5432 || row.DurationSeconds != 3600) {
				t.Fatalf("fields lost %+v", row)
			}
		}
		st.Close()
	}
}
