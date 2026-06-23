package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestManualReviewAPIApprovesDeniedCommand(t *testing.T) {
	srv, ownerClient, app := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, ownerClient, srv.URL, "manual-owner@example.com")
	var org apiOrganizationResponse
	postJSON(t, ownerClient, srv.URL+"/api/orgs", map[string]string{"name": "Manual Review", "slug": "manual-review"}, http.StatusCreated, &org)

	memberClient := apiClient(t)
	member := registerForAPI(t, memberClient, srv.URL, "manual-member@example.com")
	postJSON(t, ownerClient, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", map[string]string{
		"user_id": member.User.ID,
		"role":    "member",
	}, http.StatusOK, nil)

	var target apiTargetResponse
	postJSON(t, ownerClient, srv.URL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"name":            "Production",
		"alias":           "prod-1",
		"target_type":     "direct",
		"host":            "127.0.0.1",
		"port":            22,
		"remote_username": "root",
		"auth_type":       "password",
		"secret":          "secret",
	}, http.StatusCreated, &target)
	storeTarget, err := app.store.Repository().GetSSHTarget(context.Background(), target.Target.ID)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pollCh := startManualReviewPoll(ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&timeout_seconds=2")
	waitForManualReviewPoller(t, app, org.Organization.ID)
	resultCh := make(chan bastion.Decision, 1)
	go func() {
		resultCh <- app.reviewDeniedCommand(ctx, member.User.ID, storeTarget, "rm -rf /", bastion.Decision{
			Action:                     store.DecisionDeny,
			Reason:                     "llm: dangerous command",
			AllowManualReview:          true,
			ManualReviewTimeoutSeconds: 2,
		})
	}()

	pending := readManualReviewPoll(t, pollCh, http.StatusOK)
	if len(pending.Reviews) != 1 {
		t.Fatalf("pending review mismatch: %+v", pending)
	}
	review := pending.Reviews[0]
	if review.UserID != member.User.ID || review.TargetID != target.Target.ID || review.Command != "rm -rf /" || review.Reason != "llm: dangerous command" {
		t.Fatalf("review payload mismatch: %+v", review)
	}

	secondCh := make(chan bastion.Decision, 1)
	knownQuery := url.QueryEscape(review.ID)
	nextCh := startManualReviewPoll(ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&known_ids="+knownQuery+"&timeout_seconds=2")
	waitForManualReviewPoller(t, app, org.Organization.ID)
	go func() {
		secondCh <- app.reviewDeniedCommand(ctx, member.User.ID, storeTarget, "dd if=/dev/zero", bastion.Decision{
			Action:                     store.DecisionDeny,
			Reason:                     "llm: destructive write",
			AllowManualReview:          true,
			ManualReviewTimeoutSeconds: 2,
		})
	}()
	next := readManualReviewPoll(t, nextCh, http.StatusOK)
	if len(next.Reviews) != 1 || next.Reviews[0].ID == review.ID || next.Reviews[0].Command != "dd if=/dev/zero" {
		t.Fatalf("known_ids should return only new pending reviews: first=%+v next=%+v", review, next)
	}
	postJSON(t, ownerClient, srv.URL+"/api/manual-reviews/"+next.Reviews[0].ID+"/decision", map[string]bool{"allow": false}, http.StatusOK, nil)
	select {
	case decision := <-secondCh:
		if decision.Action != store.DecisionDeny || !strings.Contains(decision.Reason, "manual rejected by") {
			t.Fatalf("rejected decision mismatch: %+v", decision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual review rejection did not unblock second command")
	}

	getJSON(t, memberClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&timeout_seconds=0", http.StatusForbidden, nil)
	postJSON(t, memberClient, srv.URL+"/api/manual-reviews/"+review.ID+"/decision", map[string]bool{"allow": true}, http.StatusForbidden, nil)

	postJSON(t, ownerClient, srv.URL+"/api/manual-reviews/"+review.ID+"/decision", map[string]bool{"allow": true}, http.StatusOK, nil)

	select {
	case decision := <-resultCh:
		if decision.Action != store.DecisionAllow || decision.AllowManualReview || !strings.Contains(decision.Reason, "manual approved by") {
			t.Fatalf("approved decision mismatch: %+v", decision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual review decision did not unblock command")
	}

	skipped := app.reviewDeniedCommand(context.Background(), member.User.ID, storeTarget, "useradd blocked", bastion.Decision{
		Action:                     store.DecisionDeny,
		Reason:                     "llm: blocked user change",
		AllowManualReview:          true,
		ManualReviewTimeoutSeconds: 1,
	})
	if skipped.Action != store.DecisionDeny || skipped.AllowManualReview || !strings.Contains(skipped.Reason, "no active reviewer polling") {
		t.Fatalf("manual review should be skipped without active poller: %+v", skipped)
	}
	var empty apiManualReviewsResponse
	getJSON(t, ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&timeout_seconds=0", http.StatusOK, &empty)
	if len(empty.Reviews) != 0 {
		t.Fatalf("skipped manual review should not create pending reviews: %+v", empty)
	}

	sessionPollCh := startManualReviewPoll(ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&session_id=session-1&timeout_seconds=2")
	waitForManualReviewPoller(t, app, org.Organization.ID, "session-1")
	sessionResultCh := make(chan bastion.Decision, 1)
	go func() {
		sessionResultCh <- app.reviewDeniedCommandForSession(ctx, member.User.ID, storeTarget, "systemctl restart redis", bastion.Decision{
			Action:                     store.DecisionDeny,
			Reason:                     "llm: service restart",
			AllowManualReview:          true,
			ManualReviewTimeoutSeconds: 2,
		}, "session-1")
	}()
	var orgOnly apiManualReviewsResponse
	getJSON(t, ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&timeout_seconds=0", http.StatusOK, &orgOnly)
	if len(orgOnly.Reviews) != 0 {
		t.Fatalf("session-scoped review leaked into organization poll: %+v", orgOnly)
	}
	sessionPending := readManualReviewPoll(t, sessionPollCh, http.StatusOK)
	if len(sessionPending.Reviews) != 1 || sessionPending.Reviews[0].SessionID != "session-1" {
		t.Fatalf("session scoped pending review mismatch: %+v", sessionPending)
	}
	postJSON(t, ownerClient, srv.URL+"/api/manual-reviews/"+sessionPending.Reviews[0].ID+"/decision", map[string]bool{"allow": true}, http.StatusOK, nil)
	select {
	case decision := <-sessionResultCh:
		if decision.Action != store.DecisionAllow || !strings.Contains(decision.Reason, "manual approved by") {
			t.Fatalf("session review approval mismatch: %+v", decision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session review decision did not unblock command")
	}
}

type manualReviewPollResult struct {
	response apiManualReviewsResponse
	status   int
	err      error
}

func startManualReviewPoll(client *http.Client, url string) <-chan manualReviewPollResult {
	ch := make(chan manualReviewPollResult, 1)
	go func() {
		resp, err := client.Get(url)
		if err != nil {
			ch <- manualReviewPollResult{err: err}
			return
		}
		defer resp.Body.Close()
		result := manualReviewPollResult{status: resp.StatusCode}
		if resp.StatusCode == http.StatusOK {
			result.err = json.NewDecoder(resp.Body).Decode(&result.response)
		}
		ch <- result
	}()
	return ch
}

func readManualReviewPoll(t *testing.T, ch <-chan manualReviewPollResult, wantStatus int) apiManualReviewsResponse {
	t.Helper()
	select {
	case result := <-ch:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.status != wantStatus {
			t.Fatalf("manual review poll status mismatch: got %d want %d", result.status, wantStatus)
		}
		return result.response
	case <-time.After(3 * time.Second):
		t.Fatal("manual review poll timed out")
		return apiManualReviewsResponse{}
	}
}

func waitForManualReviewPoller(t *testing.T, app *App, organizationID string, sessionID ...string) {
	t.Helper()
	scope := ""
	if len(sessionID) > 0 {
		scope = sessionID[0]
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if app.manualReviews.HasActivePollers(organizationID, scope) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("manual review poller did not become active")
}
