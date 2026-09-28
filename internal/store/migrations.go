package store

import (
	"context"
	"database/sql"
	"fmt"
)

type legacyColumn struct {
	table      string
	name       string
	definition string
}

var legacyColumns = []legacyColumn{
	{"users", "is_system_admin", "INTEGER NOT NULL DEFAULT 0"},
	{"users", "auth_provider", "TEXT NOT NULL DEFAULT 'local'"},
	{"users", "disabled_at", "TEXT"},
	{"mcp_tokens", "tool_groups", "TEXT NOT NULL DEFAULT 'session'"},
	{"mcp_tokens", "token_value", "TEXT NOT NULL DEFAULT ''"},
	{"organizations", "is_personal", "INTEGER NOT NULL DEFAULT 0"},
	{"ssh_targets", "name", "TEXT NOT NULL DEFAULT ''"},
	{"ssh_targets", "proxy_target_id", "TEXT"},
	{"ssh_targets", "credential_id", "TEXT"},
	{"ssh_targets", "folder_id", "TEXT"},
	{"target_tags", "color", "TEXT NOT NULL DEFAULT ''"},
	{"command_policies", "llm_prompt_id", "TEXT"},
	{"command_policies", "ip_allowlist", "TEXT NOT NULL DEFAULT ''"},
	{"command_policies", "allow_port_forward", "INTEGER NOT NULL DEFAULT 0"},
	{"command_policies", "allow_upload", "INTEGER NOT NULL DEFAULT 0"},
	{"command_policies", "allow_download", "INTEGER NOT NULL DEFAULT 0"},
	{"command_policies", "allow_ssh_interactive", "INTEGER NOT NULL DEFAULT 0"},
	{"command_policies", "allow_web_terminal", "INTEGER NOT NULL DEFAULT 0"},
	{"command_policies", "allow_manual_review", "INTEGER NOT NULL DEFAULT 0"},
	{"command_policies", "manual_review_timeout_seconds", "INTEGER NOT NULL DEFAULT 30"},
	{"command_audit_logs", "public_key_fingerprint", "TEXT NOT NULL DEFAULT ''"},
}

func upgradeLegacyMainSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range legacyColumns {
		exists, err := hasColumn(ctx, tx, column.table, column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("upgrade legacy column %s.%s: %w", column.table, column.name, err)
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE ssh_targets SET name = alias WHERE name = ''")
	return err
}

func hasColumn(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table, column string) (bool, error) {
	rows, err := queryer.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
