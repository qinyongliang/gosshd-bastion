package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
	if _, err := first.ExecContext(ctx, "CREATE TABLE writer_test(value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer first.ExecContext(ctx, "ROLLBACK")
	written := make(chan error, 1)
	go func() { _, err := second.ExecContext(ctx, "INSERT INTO writer_test VALUES (1)"); written <- err }()
	select {
	case err := <-written:
		t.Fatalf("pooled writer did not wait: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := first.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pooled writer did not resume")
	}
}
