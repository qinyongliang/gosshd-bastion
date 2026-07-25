# Manual Review Target Scope Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scope remembered review choices by SSH user and selected targets, expose that server state in every approval popup, and allow proactive temporary authorization from the audit list.

**Architecture:** Extend the existing in-memory `manualReviewHub`; key remembered choices by organization and intercepted SSH user, and store a target-ID snapshot. Reuse one React target picker in both the pending-review card and a new audit-page modal. Add one GET/PUT API pair for proactive authorization and keep pending-review decisions on the existing endpoint.

**Tech Stack:** Go `net/http`, existing SQLite repositories for validation only, React 19, TypeScript, TanStack Query, existing CSS and i18n

---

### Task 1: Server-owned user and target scope

**Files:**
- Modify: `internal/server/manual_review.go`
- Modify: `internal/server/manual_review_ssh.go`
- Modify: `internal/server/api_manual_reviews.go`
- Modify: `internal/server/api.go`
- Test: `internal/server/manual_review_test.go`

- [ ] **Step 1: Add failing hub tests for SSH-user and target isolation**

Extend `TestManualReviewHubRemembersChoice` with remembered target IDs and create reviews for the same user on selected and unselected targets plus another user:

```go
first, firstDecision := hub.Create(manualReviewRequest{OrganizationID: "org-1", UserID: "user-1", TargetID: "target-1"}, time.Second)
minutes := 10
targetIDs := []string{"target-1"}
if err := hub.DecideWithAutoAllow(first.ID, manualReviewDecision{Allow: true, Reviewer: "owner"}, &minutes, targetIDs); err != nil {
	t.Fatal(err)
}
selected, selectedDecision := hub.Create(manualReviewRequest{OrganizationID: "org-1", UserID: "user-1", TargetID: "target-1"}, 25*time.Millisecond)
if !selected.DefaultAllow || len(selected.AutoAllowTargetIDs) != 1 {
	t.Fatalf("selected target did not load remembered choice: %+v", selected)
}
unselected, _ := hub.Create(manualReviewRequest{OrganizationID: "org-1", UserID: "user-1", TargetID: "target-2"}, time.Second)
if unselected.DefaultAllow {
	t.Fatalf("unselected target loaded remembered choice: %+v", unselected)
}
otherUser, _ := hub.Create(manualReviewRequest{OrganizationID: "org-1", UserID: "user-2", TargetID: "target-1"}, time.Second)
if otherUser.DefaultAllow {
	t.Fatalf("other SSH user loaded remembered choice: %+v", otherUser)
}
if result := <-selectedDecision; !result.Allow {
	t.Fatalf("selected target timeout mismatch: %+v", result)
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `go test ./internal/server -run TestManualReviewHubRemembersChoice -count=1`

Expected: FAIL because `DecideWithAutoAllow` has no target scope and the hub key still uses the poller session.

- [ ] **Step 3: Extend the in-memory state and command snapshots**

Use these fields and signatures in `manual_review.go`:

```go
type manualReviewAutoAllow struct {
	Allow     bool
	Minutes   int
	ExpiresAt time.Time
	TargetIDs []string
}

func manualReviewChoiceKey(organizationID, userID string) string {
	return organizationID + "\x00" + userID
}

func (h *manualReviewHub) DecideWithAutoAllow(id string, decision manualReviewDecision, minutes *int, targetIDs []string) error
func (h *manualReviewHub) AutoAllowState(organizationID, userID string) (manualReviewAutoAllow, bool)
func (h *manualReviewHub) SetAutoAllow(organizationID, userID string, minutes int, allow bool, targetIDs []string) manualReviewAutoAllow
```

Add `AutoAllowTargetIDs []string` to requests and snapshots. `Create` loads a state by organization and SSH user only when `req.TargetID` occurs in `state.TargetIDs`; copy slices when storing or returning them. Keep `manualReviewPollerKey` only for pending-review polling.

- [ ] **Step 4: Add failing API tests for proactive authorization and validation**

In `TestManualReviewAPIApprovesDeniedCommand`, call the new PUT endpoint with the existing member and target, GET it back, then reject an unknown/cross-organization target and a missing target list:

```go
var choice apiManualReviewChoiceResponse
postJSON(t, ownerClient, srv.URL+"/api/manual-review-choice", map[string]any{
	"organization_id": org.Organization.ID,
	"user_id": member.User.ID,
	"auto_allow_minutes": 10,
	"auto_allow_target_ids": []string{target.Target.ID},
}, http.StatusOK, &choice)
getJSON(t, ownerClient, srv.URL+"/api/manual-review-choice?organization_id="+org.Organization.ID+"&user_id="+member.User.ID, http.StatusOK, &choice)
if !choice.Active || choice.AutoAllowMinutes != 10 || len(choice.AutoAllowTargetIDs) != 1 {
	t.Fatalf("proactive choice mismatch: %+v", choice)
}
```

- [ ] **Step 5: Implement decision validation and proactive APIs**

Add these routes in `api.go`:

```go
mux.HandleFunc("GET /api/manual-review-choice", a.requireUser(a.handleGetManualReviewChoice))
mux.HandleFunc("PUT /api/manual-review-choice", a.requireUser(a.handlePutManualReviewChoice))
```

In `api_manual_reviews.go`, add `auto_allow_target_ids` to decision requests/responses and review payloads. Validate with `GetOrganizationMember`, `GetSSHTarget`, and `organizationIDForTarget`; require admin access, 1..1440 minutes, and at least one unique target. The PUT endpoint always calls `SetAutoAllow(..., true, targetIDs)`. The GET endpoint returns `{active:false}` for an absent or expired choice.

Use one response type for both proactive endpoints:

```go
type apiManualReviewChoiceResponse struct {
	Active             bool     `json:"active"`
	Allow              bool     `json:"allow"`
	AutoAllowMinutes   int      `json:"auto_allow_minutes,omitempty"`
	AutoAllowExpiresAt string   `json:"auto_allow_expires_at,omitempty"`
	AutoAllowTargetIDs []string `json:"auto_allow_target_ids,omitempty"`
}
```

- [ ] **Step 6: Run server tests and commit**

Run: `go test ./internal/server -count=1`

Expected: PASS.

Commit:

```bash
git add internal/server/manual_review.go internal/server/manual_review_ssh.go internal/server/api_manual_reviews.go internal/server/api.go internal/server/manual_review_test.go
git commit -m "feat: scope remembered reviews by user and targets"
```

### Task 2: Shared target-scope picker

**Files:**
- Create: `web/src/components/ManualReviewScopePicker.tsx`
- Modify: `web/src/types.ts`
- Modify: `web/styles.css`

- [ ] **Step 1: Add API-facing types**

Add `auto_allow_target_ids?: string[]` to `ManualReview`. Add `user_id`, `target_id`, and `organization_id` to `AuditLog`, matching the fields already returned by the server.

- [ ] **Step 2: Implement the shared picker without new dependencies**

Create a controlled component:

```tsx
export function ManualReviewScopePicker({
  data,
  selectedTargetIDs,
  onChange,
}: {
  data: ConsoleData;
  selectedTargetIDs: string[];
  onChange: (targetIDs: string[]) => void;
})
```

Build folder groups from direct `folder_id` membership, tag groups from `target.tags`, and safety groups from `policy.target_ids` plus targets matching `policy.target_tags`. Keep enabled selector keys in component state. Checking a selector adds all member IDs. Clearing it removes IDs not covered by another enabled selector in the same section. Individual target checkboxes directly update the selected-ID set. Render only target names and use native checkbox `indeterminate` state for partial groups.

- [ ] **Step 3: Add compact responsive styles**

Add `.manual-review-scope-*` rules beside existing manual-review styles. Use an unframed section layout, fixed checkbox dimensions, scroll only inside the expanded options, and one-column layout on narrow screens.

- [ ] **Step 4: Run frontend type checking**

Run: `pnpm check`

Expected: PASS.

- [ ] **Step 5: Commit the shared picker**

```bash
git add web/src/types.ts web/src/components/ManualReviewScopePicker.tsx web/styles.css
git commit -m "feat: add temporary authorization target picker"
```

### Task 3: Pending-review and audit-list integration

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/components/ManualReviewPoller.tsx`
- Modify: `web/src/pages/AuditPage.tsx`
- Modify: `web/src/i18n.tsx`
- Modify: `web/styles.css`

- [ ] **Step 1: Add client API methods**

Change `decideManualReview` to accept target IDs and add:

```ts
manualReviewChoice: (organizationID: string, userID: string) =>
  request<ManualReviewChoice>(`/api/manual-review-choice?${queryString({ organization_id: organizationID, user_id: userID })}`),
putManualReviewChoice: (body: { organization_id: string; user_id: string; auto_allow_minutes: number; auto_allow_target_ids: string[] }) =>
  request<ManualReviewChoice>("/api/manual-review-choice", put(body)),
```

- [ ] **Step 2: Reuse the picker in pending review cards**

Pass `data` into `ReviewCard`. Initialize selected IDs from `review.auto_allow_target_ids` when active, otherwise from every `data.targets` ID. Show a collapsed "More" button only while remembering is enabled, render `ManualReviewScopePicker` when expanded, and submit target IDs with the remembered minutes. Disable Allow and Deny when remembering is enabled with no selected targets.

- [ ] **Step 3: Add proactive temporary authorization to the audit list**

For organization owners, admins, and system admins, add a `ShieldCheck` button in the audit-page header. Open a modal that requires one `data.members` SSH user, loads its state through `manualReviewChoice`, defaults to 10 minutes and all targets when inactive, shows remaining server time when active, reuses `ManualReviewScopePicker`, and saves through `putManualReviewChoice`. Do not create or replay an audit command.

- [ ] **Step 4: Add English and Chinese text**

Add translations for More, folders, tags, safety groups, targets, partial selection, select-user prompt, temporary authorization, remaining time, and save success/failure. Keep button text command-oriented and avoid instructional paragraphs.

- [ ] **Step 5: Run release checks**

Run: `pnpm check`

Expected: PASS.

Run: `pnpm build`

Expected: PASS.

Run: `go test ./internal/server -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the frontend integration**

```bash
git add web/src/api.ts web/src/types.ts web/src/components/ManualReviewScopePicker.tsx web/src/components/ManualReviewPoller.tsx web/src/pages/AuditPage.tsx web/src/i18n.tsx web/styles.css
git commit -m "feat: configure temporary review authorization"
```

### Task 4: Publish and deploy

**Files:**
- No source changes

- [ ] **Step 1: Verify the committed release snapshot**

Use a clean detached worktree so unrelated local changes cannot enter validation. Run `pnpm install --frozen-lockfile`, `pnpm check`, `pnpm build`, and `go test ./internal/server -count=1`.

- [ ] **Step 2: Push main and the next bastion tag**

Push only committed changes. Select the next tag after the current highest `v0.1.109-bastion`, push it, and wait for `.github/workflows/release.yml` to finish successfully.

- [ ] **Step 3: Verify and deploy the Release asset**

Download `gosshd-<tag>-linux-amd64.tar.gz` and `checksums.txt` on `118.24.118.205`, verify SHA-256, back up `/opt/gosshd-bastion/gosshd-server`, replace it atomically, and recreate `gosshd-bastion` with its current ports, mounts, public host, proxy, and the new `--version`.

- [ ] **Step 4: Verify production**

Confirm the container is running with the new version, the deployed binary hash matches the extracted Release binary, and both `http://127.0.0.1:18080/healthz` and server-side `https://ssh.jsydf.cn/healthz` return `ok`.
