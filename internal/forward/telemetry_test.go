package forward

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestTrafficTelemetrySurvivesExpiredUnreportedAccessLogs(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	node, err := OpenLogs(t.TempDir(), id, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	central, err := OpenLogs(t.TempDir(), id, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer central.Close()
	e := Event{ID: uuid.NewString(), Time: time.Now().Add(-8 * 24 * time.Hour).UnixMilli(), SourceIP: "192.0.2.1", Up: 10, Down: 20, DirectUp: 10, DirectDown: 20, Concurrent: 2, SourceConcurrent: 1}
	if err := node.Import(ctx, []Event{e}); err != nil {
		t.Fatal(err)
	}
	if err := node.Prune(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	events, _ := node.Pending(ctx, 256)
	if len(events) != 0 {
		t.Fatal("expired access records survived")
	}
	buckets, err := node.PendingTraffic(ctx, 512)
	if err != nil || len(buckets) != 2 {
		t.Fatal(buckets, err)
	}
	totals, _ := node.Stats(ctx)
	for i := 0; i < 2; i++ {
		if err := central.ImportTelemetry(ctx, "agent", totals, buckets); err != nil {
			t.Fatal(err)
		}
	}
	stats, _ := central.Stats(ctx)
	if stats.Requests != 1 || stats.Up != 10 || stats.DirectDown != 20 {
		t.Fatal("lost or duplicate accounting", stats)
	}
	history, sources, err := central.Traffic(ctx, 0, time.Now().Unix()+300, "")
	if err != nil || len(history) != 1 || len(sources) != 1 || sources[0].Requests != 1 {
		t.Fatal(history, sources, err)
	}
	if err := node.AckTraffic(ctx, buckets[len(buckets)-1].Sequence); err != nil {
		t.Fatal(err)
	}
	pending, _ := node.PendingTraffic(ctx, 512)
	if len(pending) != 0 {
		t.Fatal("telemetry ack not durable")
	}
	if err := central.ImportAccess(ctx, []Event{e}); err != nil {
		t.Fatal(err)
	}
	after, _ := central.Stats(ctx)
	if after.Requests != 1 {
		t.Fatal("access replay double counted accounting")
	}
}
