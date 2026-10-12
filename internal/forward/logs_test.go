package forward

import (
	"context"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogsIsolationDedupRetention(t *testing.T) {
	dir := t.TempDir()
	aID, bID := uuid.NewString(), uuid.NewString()
	a, err := OpenLogs(dir, aID, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := OpenLogs(dir, bID, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := context.Background()
	e := Event{ID: uuid.NewString(), Time: time.Now().Add(-8 * 24 * time.Hour).UnixMilli(), SourceIP: "127.0.0.1", Path: "/api/x", RouteID: "api", Status: 502, Up: 10, Down: 20, Concurrent: 3}
	if err := a.Import(ctx, []Event{e, e}); err != nil {
		t.Fatal(err)
	}
	a.Record(Event{Path: "/new", SourceIP: "::1", Status: 200})
	if err := a.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	stats, err := a.Stats(ctx)
	if err != nil || stats.Requests != 2 || stats.Up != 10 || stats.Peak != 3 {
		t.Fatalf("%+v %v", stats, err)
	}
	pending, err := a.Pending(ctx, 256)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending %v %v", pending, err)
	}
	if err := a.Ack(ctx, pending[0].Sequence); err != nil {
		t.Fatal(err)
	}
	pending, _ = a.Pending(ctx, 256)
	if len(pending) != 1 {
		t.Fatal(pending)
	}
	filtered, _ := a.Query(ctx, LogFilter{SourceIP: "127.0.0.1", Path: "/api", Status: 502})
	if len(filtered) != 1 {
		t.Fatal(filtered)
	}
	if err := a.Prune(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	events, _ := a.Query(ctx, LogFilter{})
	if len(events) != 1 {
		t.Fatal(events)
	}
	stats, _ = a.Stats(ctx)
	if stats.Requests != 2 {
		t.Fatal("retention deleted accounting totals")
	}
	_, history, err := a.Traffic(ctx, 0, time.Now().Unix()+300, "")
	if err != nil || len(history) != 2 {
		t.Fatalf("access retention removed traffic/source history: %+v %v", history, err)
	}
	other, _ := b.Query(ctx, LogFilter{})
	if len(other) != 0 {
		t.Fatal("log leakage")
	}
	for _, id := range []string{aID, bID} {
		if _, err := os.Stat(filepath.Join(dir, id+".sqlite")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := OpenLogs(dir, "../escape", 7); err == nil {
		t.Fatal("unsafe path accepted")
	}
}
