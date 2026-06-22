package server

import (
	"context"
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
	resultCh := make(chan bastion.Decision, 1)
	go func() {
		resultCh <- app.reviewDeniedCommand(ctx, member.User.ID, storeTarget, "rm -rf /", bastion.Decision{
			Action:            store.DecisionDeny,
			Reason:            "llm: dangerous command",
			AllowManualReview: true,
		})
	}()

	var pending apiManualReviewsResponse
	getJSON(t, ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&timeout_seconds=2", http.StatusOK, &pending)
	if len(pending.Reviews) != 1 {
		t.Fatalf("pending review mismatch: %+v", pending)
	}
	review := pending.Reviews[0]
	if review.UserID != member.User.ID || review.TargetID != target.Target.ID || review.Command != "rm -rf /" || review.Reason != "llm: dangerous command" {
		t.Fatalf("review payload mismatch: %+v", review)
	}

	secondCh := make(chan bastion.Decision, 1)
	go func() {
		secondCh <- app.reviewDeniedCommand(ctx, member.User.ID, storeTarget, "dd if=/dev/zero", bastion.Decision{
			Action:            store.DecisionDeny,
			Reason:            "llm: destructive write",
			AllowManualReview: true,
		})
	}()
	var next apiManualReviewsResponse
	knownQuery := url.QueryEscape(review.ID)
	getJSON(t, ownerClient, srv.URL+"/api/manual-reviews?organization_id="+org.Organization.ID+"&known_ids="+knownQuery+"&timeout_seconds=2", http.StatusOK, &next)
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
}
