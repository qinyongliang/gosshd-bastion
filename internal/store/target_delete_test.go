package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetDeleteQueriesUseIndexes(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, query := range []string{
		`EXPLAIN QUERY PLAN UPDATE ssh_targets SET proxy_target_id = NULL WHERE proxy_target_id = 'target-id'`,
		`EXPLAIN QUERY PLAN DELETE FROM policy_targets WHERE target_id = 'target-id'`,
		`EXPLAIN QUERY PLAN DELETE FROM ssh_targets WHERE id = 'target-id'`,
	} {
		rows, err := st.DB().QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if strings.Contains(detail, "SCAN ssh_targets") || strings.Contains(detail, "SCAN policy_targets") || strings.Contains(detail, "SCAN command_audit_logs") {
				rows.Close()
				t.Fatalf("target deletion query performs a full table scan: %s", detail)
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
