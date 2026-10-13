package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPListenerMigrationCanonicalAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		"CREATE TABLE tunnels(id TEXT PRIMARY KEY,config TEXT NOT NULL,revision INTEGER NOT NULL,enabled INTEGER NOT NULL,expires_at INTEGER)",
		"CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)",
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	paths, _ := fs.Glob(schemaFiles, "migrations/*.sql")
	for _, file := range paths {
		base := filepath.Base(file)
		number, _, _ := strings.Cut(base, "_")
		version, _ := strconv.Atoi(number)
		if version != 8 {
			if _, err = db.Exec("INSERT INTO schema_migrations VALUES(?,?,?)", version, base, "2026-01-01"); err != nil {
				t.Fatal(err)
			}
		}
	}
	type fixture struct {
		id, raw, host string
		port          int
		migrated      bool
	}
	cases := []fixture{
		{"missing", "{\"protocol\":\"http\",\"listen_host\":\"127.0.0.1\",\"listen_port\":8080,\"http\":{\"servers\":[{\"id\":\"site\"}]}}", "127.0.0.1", 8080, true},
		{"empty", "{\"protocol\":\"http\",\"listen_host\":\"192.0.2.10\",\"listen_port\":8081,\"http\":{\"listeners\":[],\"servers\":[{\"id\":\"site\"}]}}", "192.0.2.10", 8081, true},
		{"null", "{\"protocol\":\"http\",\"listen_host\":\"\",\"listen_port\":8082,\"http\":{\"listeners\":null,\"servers\":[{\"id\":\"site\"}]}}", "0.0.0.0", 8082, true},
		{"canonical", "{\"protocol\":\"http\",\"http\":{\"listeners\":[{\"host\":\"192.0.2.20\",\"port\":9090},{\"host\":\"192.0.2.21\",\"port\":9091}],\"servers\":[{\"id\":\"site\"}]}}", "192.0.2.20", 9090, false},
		{"port", "{\"protocol\":\"port\",\"listen_host\":\"127.0.0.1\",\"listen_port\":8083,\"transports\":[\"tcp\"]}", "", 0, false},
	}
	for _, c := range cases {
		if _, err = db.Exec("INSERT INTO tunnels VALUES(?,?,9,1,?)", c.id, c.raw, int64(1900000000000000000)); err != nil {
			t.Fatal(err)
		}
	}
	for run := 0; run < 2; run++ {
		if err = applySchemaMigrations(ctx, db, "migrations", "ssh_targets", nil); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			var raw string
			var revision int
			var enabled bool
			var expiry int64
			if err = db.QueryRow("SELECT config,revision,enabled,expires_at FROM tunnels WHERE id=?", c.id).Scan(&raw, &revision, &enabled, &expiry); err != nil {
				t.Fatal(err)
			}
			wantRevision := 9
			if c.migrated {
				wantRevision++
			}
			if revision != wantRevision || !enabled || expiry != 1900000000000000000 {
				t.Fatalf("state changed: %s %d %v %d", c.id, revision, enabled, expiry)
			}
			if !c.migrated && raw != c.raw {
				t.Fatalf("existing canonical/port config changed: %s", c.id)
			}
			if c.id != "port" {
				var config TunnelConfig
				if err = json.Unmarshal([]byte(raw), &config); err != nil {
					t.Fatal(err)
				}
				if config.HTTP.Listeners[0].Host != c.host || config.HTTP.Listeners[0].Port != c.port || len(config.HTTP.Servers) != 1 || config.HTTP.Servers[0].ID != "site" {
					t.Fatalf("bad conversion: %s %+v", c.id, config)
				}
			}
		}
	}
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("%s %v", integrity, err)
	}
}
