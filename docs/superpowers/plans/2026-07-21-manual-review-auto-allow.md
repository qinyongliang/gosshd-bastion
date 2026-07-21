# Manual Review Auto-Allow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a reviewer arm a shared countdown so pending commands still appear for review but automatically allow at the deadline.

**Architecture:** Extend the in-memory `manualReviewHub` to own expiry timers and one auto-allow state per existing organization/session poller key. Expose that state on review snapshots and accept an optional setting update with an Allow decision; keep frontend state local to each review card and send updates only when enabling, disabling, or changing the configured minutes.

**Tech Stack:** Go 1.26, `net/http`, React 19, TypeScript, TanStack Query, existing CSS/i18n.

---

### Task 1: Hub-owned expiry and auto-allow state

**Files:**
- Modify: `internal/server/manual_review.go`
- Test: `internal/server/manual_review_test.go`

- [ ] **Step 1: Write failing hub tests**

Add tests that create a request, arm a short auto-allow deadline through a hub helper, assert the snapshot contains the deadline/minutes, and receive an allowed automatic decision. Add a renewal test with two pending requests and assert both snapshots move to the new deadline. Add a disable test and assert requests return to normal denial expiry.

```go
if result, ok := <-decided; !ok || !result.Allow || result.Reviewer != "automatic deadline" {
	t.Fatalf("automatic decision mismatch: %+v %v", result, ok)
}
```

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/server -run 'TestManualReviewHubAutoAllow' -count=1`

Expected: FAIL because the hub has no automatic state or reschedulable expiry.

- [ ] **Step 3: Implement the minimal hub state**

Add a per-stream state, retain each request's normal expiry, and schedule expiry inside the hub.

```go
type manualReviewAutoAllow struct {
	Minutes   int
	ExpiresAt time.Time
}

type manualReviewRequest struct {
	// existing fields
	NormalExpiresAt time.Time
	AutoAllow       bool
	timer           *time.Timer
}
```

Add a locked auto-allow update where `0` disables and `1..1440` sets `now + minutes`. Reschedule every matching pending request. The timer callback removes the request once under the hub lock, sends `{Allow: true, Reviewer: "automatic deadline"}` for auto-allow expiry, otherwise closes the channel for normal denial.

- [ ] **Step 4: Make SSH handlers trust the hub decision channel**

Modify `internal/server/manual_review_ssh.go` to remove the independent `time.After(wait)` branch. Wait on the hub-owned decision channel or context cancellation; preserve the current manual approved/rejected/expired reason handling.

- [ ] **Step 5: Run focused tests**

Run: `go test ./internal/server -run 'TestManualReview' -count=1`

Expected: PASS.

### Task 2: API contract and validation

**Files:**
- Modify: `internal/server/api_manual_reviews.go`
- Test: `internal/server/manual_review_test.go`

- [ ] **Step 1: Write failing API assertions**

Extend the approval flow to submit `{"allow":true,"auto_allow_minutes":10}`, then create another review and assert:

```go
if review.AutoAllowMinutes != 10 || review.AutoAllowExpiresAt == "" {
	t.Fatalf("auto allow state mismatch: %+v", review)
}
```

Also submit `-1` and `1441` and expect HTTP 400 without resolving the review.

- [ ] **Step 2: Run the API test and verify failure**

Run: `go test ./internal/server -run TestManualReviewAPIApprovesDeniedCommand -count=1`

Expected: FAIL because response fields and request parsing do not exist.

- [ ] **Step 3: Add API fields and optional update**

Use a pointer so omitted differs from zero:

```go
var req struct {
	Allow            bool `json:"allow"`
	AutoAllowMinutes *int `json:"auto_allow_minutes"`
}
```

Reject values outside `0..1440` and reject setting updates on Deny. Add `DecideWithAutoAllow` on the hub so one lock removes and resolves the current review before updating and rescheduling the remaining reviews in its stream; keep the existing `Decide` as a wrapper for callers without a setting update. Add `auto_allow_minutes` and `auto_allow_expires_at` to snapshots and JSON.

- [ ] **Step 4: Run focused server tests**

Run: `go test ./internal/server -run 'TestManualReview' -count=1`

Expected: PASS.

### Task 3: Popup control and remaining-time display

**Files:**
- Modify: `web/src/types.ts`
- Modify: `web/src/api.ts`
- Modify: `web/src/components/ManualReviewPoller.tsx`
- Modify: `web/src/i18n.tsx`
- Modify: `web/styles.css`

- [ ] **Step 1: Extend the typed contract**

Add optional `auto_allow_minutes` and `auto_allow_expires_at` to `ManualReview`. Change `decideManualReview` to accept an optional minutes value and omit the JSON property when unchanged.

```ts
decideManualReview: (id: string, allow: boolean, autoAllowMinutes?: number) =>
  request<{ ok: true }>(`/api/manual-reviews/${id}/decision`, post({
    allow,
    ...(autoAllowMinutes === undefined ? {} : { auto_allow_minutes: autoAllowMinutes }),
  })),
```

- [ ] **Step 2: Add per-card controls**

Initialize the checkbox from `review.auto_allow_expires_at`, initialize minutes from `review.auto_allow_minutes || 10`, and compute the outgoing value on Allow:

```ts
const update = !enabled ? (active ? 0 : undefined) : (!active || minutes !== configured ? minutes : undefined);
onAllow(update);
```

Deny sends no setting update. Show the automatic deadline countdown in the header when active; otherwise keep the existing manual timeout countdown. Format it with seconds as `MM:SS` or `HH:MM:SS`. Remove the card on expiry only for normal review timeout; let the automatic decision response remove active auto-allow reviews through polling/decision completion.

- [ ] **Step 3: Add concise bilingual labels and CSS**

Add English and Chinese keys for "Allow automatically when time expires", "minutes", and "auto allow in" without any helper paragraph. Style one compact row above the actions and keep it wrapping at the existing mobile breakpoint.

- [ ] **Step 4: Run frontend checks**

Run: `pnpm check`

Expected: PASS.

Run: `pnpm build`

Expected: PASS.

### Task 4: Regression verification

**Files:**
- Verify only; no new files expected.

- [ ] **Step 1: Run server regression tests**

Run: `go test ./internal/server ./internal/bastion`

Expected: PASS.

- [ ] **Step 2: Inspect the final diff**

Run: `git diff --check`

Expected: no whitespace errors.

Run: `git status --short`

Expected: only the auto-allow implementation plus the user's pre-existing file-manager and unrelated documentation changes.
