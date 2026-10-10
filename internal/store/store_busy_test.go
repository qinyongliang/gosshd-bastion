package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMainStorePooledConnectionsWaitForWriters(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "main.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// Holding the first connection forces the pool to create a second one.
	first, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var timeout int
	if err := second.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
		t.Fatalf("pooled writer timeout = %d: %v", timeout, err)
	}
}
