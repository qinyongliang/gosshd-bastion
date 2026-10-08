package server

import (
	"context"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestRunningAuditKeepsIdentityWhenPersisted(t *testing.T) {
	srv, _, app := newAPITestServer(t)
	defer srv.Close()
	started := time.Now().UTC()
	params := store.CreateCommandAuditLogParams{UserID: "audit-user", TargetID: "audit-target", OrganizationID: "audit-org", Command: "echo audit-handoff", RequestType: store.RequestExec, PolicyDecision: store.DecisionAllow, StartedAt: started, PublicKeyFingerprint: temporaryAuthorizationAuditPrefix + "客户临时访问"}
	live := app.runningAudits.start(params)
	if snapshot := live.snapshot(); snapshot.PublicKeyName != "客户临时访问" || snapshot.ID == "" {
		t.Fatal("running authorization name or identity missing")
	}
	ended := started.Add(time.Second)
	live.finish(0, ended)
	if app.runningAudits.get(live.id) == nil {
		t.Fatal("finished audit disappeared before persistence")
	}
	params.ID, params.EndedAt = live.id, &ended
	entry, err := app.createAuditLog(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != live.id || entry.PublicKeyName != "客户临时访问" || entry.PublicKeyFingerprint != "" {
		t.Fatal("history did not preserve running ID and authorization name")
	}
	if app.runningAudits.get(live.id) != nil {
		t.Fatal("persisted audit still appears as running")
	}
	stored, err := app.audit.Repository().GetCommandAuditLog(context.Background(), live.id)
	if err != nil || stored.PublicKeyName != "客户临时访问" {
		t.Fatal("completed audit was not retained")
	}
}

func TestRunningAuditSystemAdminRespectsRequestedOrganization(t *testing.T) {
	live := newRunningAuditStore()
	live.start(store.CreateCommandAuditLogParams{OrganizationID: "org-a", UserID: "user-a"})
	live.start(store.CreateCommandAuditLogParams{OrganizationID: "org-b", UserID: "user-b"})
	if rows := live.list("", "org-a", true); len(rows) != 1 || rows[0].snapshot().OrganizationID != "org-a" {
		t.Fatal("running audits leak records from a different organization")
	}
	if rows := live.list("", "", true); len(rows) != 2 {
		t.Fatal("unscoped system-admin view was restricted")
	}
}
