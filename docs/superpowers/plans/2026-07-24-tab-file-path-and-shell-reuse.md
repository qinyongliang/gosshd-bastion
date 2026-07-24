# Tab File Path and Shell Reuse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve file-manager paths per console tab and prevent command reuse while a terminal is running tmux or another foreground program.

**Architecture:** Store file paths in the existing `ConnectionTab` state and make `FileManager` path-controlled. Reuse the existing OSC-driven `shellBusy` state as the server-authoritative routing gate, and prevent the outer Bash prompt hook from leaking into child shells.

**Tech Stack:** React, TypeScript, TanStack Query, Go, Bash, existing Go and frontend checks.

---

### Task 1: Preserve File Paths Per Tab

**Files:**
- Modify: `web/src/pages/ConnectPage.tsx`
- Modify: `web/src/pages/FileManager.tsx`

- [ ] **Step 1: Add a failing path isolation check**

Add a small exported-free helper test in the existing frontend check surface that updates one tab and asserts another tab's `filePath` is unchanged.

- [ ] **Step 2: Run the frontend check and confirm failure**

Run: `pnpm run check`

Expected: TypeScript fails because the tab path field or controlled file-manager props do not exist.

- [ ] **Step 3: Implement controlled path state**

Add `filePath: string` to `ConnectionTab`, initialize it to `.`, pass it to `FileManager`, and update only the active tab when `FileManager` resolves or navigates to a path. Reset transient file UI when the tab or target changes, but do not replace the supplied path.

- [ ] **Step 4: Run the frontend check**

Run: `pnpm run check`

Expected: PASS.

### Task 2: Exclude Busy Terminals

**Files:**
- Modify: `internal/server/terminal_session_manager.go`
- Modify: `internal/server/terminal_session_manager_test.go`

- [ ] **Step 1: Write failing busy-session tests**

Assert that `earliestOnlineForUserTargetWithDiagnostics` returns reason `shell-busy` and does not select a busy session. Assert `listForUser` omits a busy session.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/server -run 'Busy|EarliestOnline' -count=1`

Expected: FAIL because busy sessions are currently candidates and listed.

- [ ] **Step 3: Implement the routing gate**

Add the existing `shellBusy` flag to both candidate filtering and MCP-visible session filtering. Keep `commandReadinessError` unchanged as defense for explicit IDs and races.

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/server -run 'Busy|EarliestOnline' -count=1`

Expected: PASS.

### Task 3: Stop Prompt Hook Leakage Into tmux

**Files:**
- Modify: `internal/server/terminal_session_manager.go`
- Modify: `internal/server/terminal_session_manager_test.go`
- Modify: `internal/agent/command_unix.go`
- Test: `internal/agent/command_unix_test.go`

- [ ] **Step 1: Add failing script assertions**

Assert that direct and agent Bash integration scripts contain `export -n PROMPT_COMMAND` after installing `__gosshd_precmd`.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/server ./internal/agent -run 'Bash.*Integration|AgentBash' -count=1`

Expected: FAIL because the prompt variable can retain an inherited export attribute.

- [ ] **Step 3: Implement the Bash fix**

Add `export -n PROMPT_COMMAND 2>/dev/null || true` immediately after assigning the prompt hook in both script generators.

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/server ./internal/agent -run 'Bash.*Integration|AgentBash' -count=1`

Expected: PASS.

### Task 4: Verify and Release

**Files:**
- Modify: release tag only

- [ ] **Step 1: Run full verification**

Run `go test ./...`, `pnpm run check`, and `pnpm run build`.

Expected: all commands pass.

- [ ] **Step 2: Commit intended files**

Review `git diff --check` and stage only the terminal reconnect, per-tab path, busy routing, tmux fix, tests, and these approved docs. Exclude unrelated deleted or untracked documents.

- [ ] **Step 3: Trigger release**

Push `main`, create and push `v0.1.108-bastion`, then wait for `.github/workflows/release.yml` to finish.

- [ ] **Step 4: Verify and deploy artifact**

Download the Linux amd64 archive and `checksums.txt`, verify SHA-256, back up the live binary on `root@118.24.118.205`, replace it while preserving `/app/data`, ports, host keys, cache, and restart policy, then verify container health, `/healthz`, and the reported version.

