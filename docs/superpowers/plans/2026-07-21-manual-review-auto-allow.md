# Manual Review Remembered Choice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep every manual-review popup interactive while using a recent remembered Allow or Deny choice only as its timeout default.

**Architecture:** Reuse the existing per-organization/session in-memory state, adding the remembered boolean decision. Snapshot that state when a review is created, keep the normal policy deadline, and resolve the request with its snapshotted default when the deadline expires.

**Tech Stack:** Go, `net/http`, React, TypeScript, existing manual review hub and popup.

---

### Task 1: Lock timeout semantics with tests

**Files:** `internal/server/manual_review_test.go`

- [ ] Add tests for baseline timeout Deny, remembered Allow, and remembered Deny.
- [ ] Run `go test ./internal/server -run 'TestManualReviewHub(RemembersChoice|RejectsWhenReviewTimesOutWithoutRememberedChoice)$' -count=1` and confirm failure before implementation.

### Task 2: Store and apply the remembered decision

**Files:** `internal/server/manual_review.go`, `internal/server/manual_review_ssh.go`, `internal/server/api_manual_reviews.go`

- [ ] Store Allow/Deny with minutes and expiry; snapshot it into new requests without changing their deadline.
- [ ] Remove immediate auto-approval and allow the decision endpoint to remember Deny as well as Allow.
- [ ] Expose `default_allow` in review responses and run focused server tests.

### Task 3: Correct popup behavior

**Files:** `web/src/types.ts`, `web/src/components/ManualReviewPoller.tsx`, `web/src/i18n.tsx`

- [ ] Rename the visible setting to "Remember my choice" / "记住我的选择".
- [ ] Always count down the review deadline and show whether timeout will Allow or Deny.
- [ ] Send the remember duration from both Allow and Deny actions; send zero when clearing an active memory.
- [ ] Run TypeScript checking and the Vite production build.

### Task 4: Verify and deploy

- [ ] Run focused Go packages, `git diff --check`, and inspect staged scope.
- [ ] Commit and push only remembered-choice files.
- [ ] Deploy the Linux amd64 binary to `ubuntu@1.14.10.105:/opt/gosshd-bastion` with a timestamped backup and verify `/healthz`.
