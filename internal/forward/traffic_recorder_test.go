package forward

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTrafficRecorderPersistsAggregatesWithoutAccessRows(t *testing.T) {
	dir := t.TempDir()
	id := uuid.NewString()
	l, err := OpenLogs(dir, id, 7)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	bucket := now.Truncate(5 * time.Minute).Unix()
	recorder := l.TrafficRecorder()
	recorder.Record(Event{ID: uuid.NewString(), Time: now.UnixMilli(), SourceIP: "192.0.2.1", Method: "TCP", Path: "/not-an-access-row", RouteID: "tcp", Up: 120, Down: 240, DirectUp: 20, DirectDown: 40, Concurrent: 3, SourceConcurrent: 2})
	recorder.Record(Event{Time: now.UnixMilli(), SourceIP: "192.0.2.1", Method: "UDP", Up: 10, Down: 20, Concurrent: 1, SourceConcurrent: 1, Error: "not-persisted-detail"})
	if err := l.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	verify := func(l *Logs) {
		t.Helper()
		var count int
		if err := l.db.QueryRow("SELECT COUNT(*) FROM access_logs").Scan(&count); err != nil || count != 0 {
			t.Fatalf("access rows %d: %v", count, err)
		}
		events, err := l.Query(ctx, LogFilter{})
		if err != nil || len(events) != 0 {
			t.Fatalf("events %+v %v", events, err)
		}
		pending, err := l.Pending(ctx, 256)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending %+v %v", pending, err)
		}
		retained, err := l.Sources(ctx, LogFilter{})
		if err != nil || len(retained) != 0 {
			t.Fatalf("retained access sources %+v %v", retained, err)
		}
		stats, err := l.Stats(ctx)
		if err != nil || stats.Requests != 2 || stats.Up != 130 || stats.Down != 260 || stats.Peak != 3 || stats.Errors != 1 || stats.DirectUp != 20 || stats.DirectDown != 40 || stats.Dropped != 0 {
			t.Fatalf("stats %+v %v", stats, err)
		}
		buckets, sources, err := l.Traffic(ctx, bucket, bucket+300, "")
		if err != nil || len(buckets) != 1 || len(sources) != 1 || sources[0].SourceIP != "192.0.2.1" || sources[0].Up != 130 || sources[0].Down != 260 || sources[0].Peak != 2 {
			t.Fatalf("traffic %+v %+v %v", buckets, sources, err)
		}
	}
	verify(l)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = OpenLogs(filepath.Clean(dir), id, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.Prune(ctx, now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	verify(l)
}
