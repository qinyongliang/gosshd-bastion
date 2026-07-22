# Manual Review Background Timer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep remembered manual-review decisions and deadline resolution active when no browser page is polling.

**Architecture:** Reuse the existing in-memory `manualReviewHub`, which already owns pending reviews, absolute deadlines, and timers. Remove only the SSH review entrypoint's active-poller gate so every eligible denied command reaches that hub.

**Tech Stack:** Go, existing `internal/server` integration-test helpers

---

### Task 1: Allow server-owned reviews without a browser poller

**Files:**
- Modify: `internal/server/manual_review_test.go`
- Modify: `internal/server/manual_review_ssh.go`

- [ ] **Step 1: Replace the skip assertion with a failing background-timer assertion**

In `TestManualReviewAPIApprovesDeniedCommand`, store a remembered Allow choice, assert there is no active poller, call `reviewDeniedCommand` with a one-second timeout, and expect `DecisionAllow` with `remembered choice` in the reason:

```go
app.manualReviews.mu.Lock()
app.manualReviews.updateAutoAllowLocked(org.Organization.ID, "", 10, true, time.Now().UTC())
app.manualReviews.mu.Unlock()
if app.manualReviews.HasActivePollers(org.Organization.ID, "") {
	t.Fatal("background review test unexpectedly has an active poller")
}
background := app.reviewDeniedCommand(context.Background(), member.User.ID, storeTarget, "useradd blocked", bastion.Decision{
	Action:                     store.DecisionDeny,
	Reason:                     "llm: blocked user change",
	AllowManualReview:          true,
	ManualReviewTimeoutSeconds: 1,
})
if background.Action != store.DecisionAllow || background.AllowManualReview || !strings.Contains(background.Reason, "remembered choice") {
	t.Fatalf("background manual review should apply remembered allow at deadline: %+v", background)
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `go test ./internal/server -run TestManualReviewAPIApprovesDeniedCommand -count=1`

Expected: FAIL because the current active-poller guard returns `DecisionDeny` immediately.

- [ ] **Step 3: Remove the active-poller gate**

Delete this block from `reviewDeniedCommandForSession` in `internal/server/manual_review_ssh.go`:

```go
if !a.manualReviews.HasActivePollers(organizationID, sessionID) {
	decision.AllowManualReview = false
	decision.Reason = "manual review skipped: no active reviewer polling: " + decision.Reason
	return decision
}
```

Leave the existing `manualReviewHub.Create` and timer behavior unchanged.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./internal/server -run TestManualReviewAPIApprovesDeniedCommand -count=1`

Expected: PASS.

Run: `go test ./internal/server -count=1`

Expected: PASS.

- [ ] **Step 5: Commit only the implementation files**

```bash
git add internal/server/manual_review_test.go internal/server/manual_review_ssh.go
git commit -m "fix: keep manual reviews active without browser"
```
