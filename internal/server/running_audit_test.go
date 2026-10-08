package server

import (
	"context"
	"net/http"
	"sync"
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

func TestRunningAuditStopIsIdempotentAndCannotStopFinishedCommand(t *testing.T) {
	live := newRunningAuditStore().start(store.CreateCommandAuditLogParams{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count := 0
	live.setCancel(func() { count++; cancel() })
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { live.stop() })
	}
	wg.Wait()
	if count != 1 || ctx.Err() == nil {
		t.Fatalf("stop count = %d, context error = %v", count, ctx.Err())
	}
	live.finish(137, time.Now())
	if live.stop() {
		t.Fatal("finished command accepted a stop request")
	}
}

func TestAPIStopRunningAuditChecksAccessAndCompletion(t *testing.T) {
	srv, ownerClient, app := newAPITestServer(t)
	defer srv.Close()
	owner := registerForAPI(t, ownerClient, srv.URL, "stop-owner@example.com")
	otherClient := apiClient(t)
	registerForAPI(t, otherClient, srv.URL, "stop-other@example.com")
	live := app.runningAudits.start(store.CreateCommandAuditLogParams{UserID: owner.User.ID, Command: "sleep 60", RequestType: store.RequestExec})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	live.setCancel(cancel)
	path := srv.URL + "/api/audit-live/" + live.id + "/stop"
	postJSON(t, apiClient(t), path, nil, http.StatusUnauthorized, nil)
	postJSON(t, otherClient, path, nil, http.StatusForbidden, nil)
	if ctx.Err() != nil {
		t.Fatal("unauthorized request stopped command")
	}
	postJSON(t, ownerClient, path, nil, http.StatusAccepted, nil)
	if ctx.Err() == nil {
		t.Fatal("accepted stop did not cancel execution")
	}
	postJSON(t, ownerClient, path, nil, http.StatusAccepted, nil)
	live.finish(137, time.Now())
	postJSON(t, ownerClient, path, nil, http.StatusConflict, nil)
	postJSON(t, ownerClient, srv.URL+"/api/audit-live/missing/stop", nil, http.StatusNotFound, nil)
}

func TestAPILiveOutputReturnsCompletedAuditAfterPersistence(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	user := registerForAPI(t, client, srv.URL, "stop-history@example.com")
	params := store.CreateCommandAuditLogParams{UserID: user.User.ID, Command: "sleep 60", RequestType: store.RequestExec, StartedAt: time.Now().UTC()}
	live := app.runningAudits.start(params)
	live.append("output before stopping")
	var response struct {
		Log    apiAuditLog `json:"log"`
		Output *string     `json:"output"`
	}
	path := srv.URL + "/api/audit-live/" + live.id
	getJSON(t, client, path, http.StatusOK, &response)
	if response.Output == nil || *response.Output != "output before stopping" || !response.Log.Running {
		t.Fatalf("live output missing: %+v", response)
	}
	ended := time.Now().UTC()
	code := 137
	live.finish(code, ended)
	params.ID, params.ExitCode, params.EndedAt = live.id, &code, &ended
	if _, err := app.createAuditLog(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	response.Log = apiAuditLog{}
	response.Output = nil
	getJSON(t, client, path, http.StatusOK, &response)
	if response.Log.Running || response.Log.EndedAt == "" || response.Output != nil {
		t.Fatalf("completed audit missing or replaced captured output: %+v", response)
	}
	other := apiClient(t)
	registerForAPI(t, other, srv.URL, "stop-history-other@example.com")
	getJSON(t, other, path, http.StatusForbidden, nil)
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
