package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMainSchemaMigrationsFreshAndRepeated(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "gosshd.db")
	for i := 0; i < 2; i++ {
		st, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		assertMigrationVersions(t, st.DB(), 1, 2, 3, 4, 5)
		assertSchemaObject(t, st.DB(), "table", "temporary_ssh_authorizations")
		assertSchemaObject(t, st.DB(), "table", "tunnels")
		for _, index := range []string{"idx_ssh_targets_proxy_target", "idx_policy_targets_target", "idx_command_audit_target_started", "idx_temporary_ssh_authorizations_target"} {
			assertSchemaObject(t, st.DB(), "index", index)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMainSchemaMigrationsUpgradeLegacyData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT NOT NULL, display_name TEXT NOT NULL, password_hash BLOB NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE ssh_targets (id TEXT PRIMARY KEY, owner_type TEXT NOT NULL, owner_id TEXT NOT NULL, alias TEXT NOT NULL, target_type TEXT NOT NULL, host TEXT NOT NULL, port INTEGER NOT NULL, remote_username TEXT NOT NULL, auth_type TEXT NOT NULL, encrypted_secret BLOB, agent_id TEXT, created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO users (id, email, display_name, password_hash, created_at) VALUES ('user-1', 'legacy@example.com', 'Legacy', x'01', '2026-01-01')`,
		`INSERT INTO ssh_targets (id, owner_type, owner_id, alias, target_type, host, port, remote_username, auth_type, created_by, created_at, updated_at) VALUES ('target-1', 'personal', 'user-1', 'legacy-target', 'direct', 'localhost', 22, 'root', 'password', 'user-1', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertMigrationVersions(t, st.DB(), 1, 2, 3, 4, 5)
	assertSchemaObject(t, st.DB(), "table", "temporary_ssh_authorizations")
	assertSchemaObject(t, st.DB(), "table", "tunnels")
	var name, alias string
	if err := st.DB().QueryRowContext(ctx, "SELECT name, alias FROM ssh_targets WHERE id = 'target-1'").Scan(&name, &alias); err != nil {
		t.Fatal(err)
	}
	if name != "legacy-target" || alias != "legacy-target" {
		t.Fatalf("legacy target changed: name=%q alias=%q", name, alias)
	}
	for _, column := range []string{"proxy_target_id", "credential_id", "folder_id"} {
		exists, err := hasColumn(ctx, st.DB(), "ssh_targets", column)
		if err != nil || !exists {
			t.Fatalf("missing upgraded column %s: %v", column, err)
		}
	}
}

func TestAuditSchemaMigrationsBaselineExistingData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := schemaFiles.ReadFile("audit_migrations/0001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO command_audit_logs (id, user_id, target_id, session_id, command, request_type, policy_decision, policy_reason, started_at) VALUES ('audit-1', 'user-1', 'target-1', 'session-1', 'pwd', 'exec', 'allow', '', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenAudit(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertMigrationVersions(t, st.DB(), 1, 2)
	var command string
	if err := st.DB().QueryRowContext(ctx, "SELECT command FROM command_audit_logs WHERE id = 'audit-1'").Scan(&command); err != nil || command != "pwd" {
		t.Fatalf("legacy audit changed: command=%q err=%v", command, err)
	}
}

func assertMigrationVersions(t *testing.T, db *sql.DB, want ...int) {
	t.Helper()
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		got = append(got, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("migration versions: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("migration versions: got %v want %v", got, want)
		}
	}
}

func assertSchemaObject(t *testing.T, db *sql.DB, kind, name string) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", kind, name).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing %s %s: count=%d err=%v", kind, name, count, err)
	}
}
