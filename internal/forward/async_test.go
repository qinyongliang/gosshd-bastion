package forward

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestAccessLoggingDoesNotBlockOnSQLiteWriter(t *testing.T) {
	l, err := OpenLogs(t.TempDir(), uuid.NewString(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	tx, err := l.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE totals SET peak=peak WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for i := 0; i < 6000; i++ {
		l.Record(Event{Path: "/nonblocking", Status: 200})
	}
	elapsed := time.Since(started)
	if elapsed > time.Second {
		t.Fatalf("data plane waited for SQLite: %s", elapsed)
	}
	if l.dropped.Load() == 0 {
		t.Fatal("bounded queue overflow was not reported")
	}
	tx.Rollback()
	if err := l.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := l.Stats(context.Background())
	if err != nil || stats.Requests != 6000 {
		t.Fatalf("log overload lost compact accounting: %+v %v", stats, err)
	}
}
