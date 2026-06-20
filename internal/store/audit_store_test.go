package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditRepositoryPaginationSearchAndRecordingMetadata(t *testing.T) {
	ctx := context.Background()
	audit, err := OpenAudit(ctx, filepath.Join(t.TempDir(), "gosshd-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	repo := audit.Repository()
	start := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	for i, item := range []struct {
		command string
		target  string
		key     string
	}{
		{"whoami", "test2", "workstation"},
		{"docker ps", "docker-host", "ops-laptop"},
		{"df -h", "db-host", "readonly-key"},
	} {
		if _, err := repo.CreateCommandAuditLog(ctx, CreateCommandAuditLogParams{
			UserID:               "user-1",
			UserEmail:            "ops@example.com",
			UserDisplayName:      "Ops",
			TargetID:             "target-" + item.target,
			TargetName:           item.target,
			TargetAlias:          item.target,
			TargetHost:           item.target + ".internal",
			TargetPort:           22,
			TargetUsername:       "root",
			OrganizationID:       "org-1",
			PublicKeyFingerprint: "SHA256:" + item.key,
			PublicKeyName:        item.key,
			SessionID:            "session-" + item.target,
			Command:              item.command,
			RequestType:          RequestExec,
			PolicyDecision:       DecisionAllow,
			PolicyReason:         "llm (1s)",
			ExitCode:             intPtr(0),
			StartedAt:            start.Add(time.Duration(i) * time.Minute),
			RemoteAddress:        "10.0.0.1:12345",
			RecordingPath:        item.target + ".jsonl.gz",
			RecordingSize:        int64(100 + i),
			RecordingSHA256:      "sha256-" + item.target,
			RecordingDurationMS:  int64(1000 + i),
			RecordingWidth:       120,
			RecordingHeight:      32,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := repo.ListCommandAuditLogs(ctx, AuditLogFilter{Query: "docker", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Logs) != 1 || page.Logs[0].Command != "docker ps" {
		t.Fatalf("query page mismatch: %+v", page)
	}
	if page.Logs[0].PublicKeyName != "ops-laptop" || page.Logs[0].RecordingPath != "docker-host.jsonl.gz" {
		t.Fatalf("audit denormalized metadata missing: %+v", page.Logs[0])
	}

	page, err = repo.ListCommandAuditLogs(ctx, AuditLogFilter{
		StartedFrom: start.Add(time.Minute),
		StartedTo:   start.Add(2 * time.Minute),
		Limit:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Logs) != 1 || page.Logs[0].Command != "df -h" {
		t.Fatalf("time range pagination mismatch: %+v", page)
	}
	got, err := repo.GetCommandAuditLog(ctx, page.Logs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecordingDurationMS == 0 || got.RecordingWidth != 120 || got.RecordingHeight != 32 {
		t.Fatalf("recording metadata mismatch: %+v", got)
	}
}
