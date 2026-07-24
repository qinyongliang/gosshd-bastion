# MCP Busy Session Interrupt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make busy terminal sessions visible to MCP and interruptible with Ctrl+C without making them eligible for command reuse.

**Architecture:** Reuse the existing `shellBusy` state and `interrupt()` method. Pass the state through the existing session-list DTOs, remove only the list filter, and leave command readiness and automatic routing unchanged.

**Tech Stack:** Go 1.26, modelcontextprotocol/go-sdk, standard `testing` package.

---

### Task 1: Expose busy sessions and verify Ctrl+C

**Files:**
- Modify: `internal/server/terminal_session_manager.go`
- Test: `internal/server/terminal_session_manager_test.go`

- [x] **Step 1: Write failing manager tests**

Replace `TestListForUserOmitsShellBusySessions` with assertions that both sessions are listed and their `ShellBusy` values are correct. Add a test using `bytes.Buffer` as session input, set `shellBusy = true`, call `interrupt()`, and assert the input equals `"\x03"`.

- [x] **Step 2: Run tests and verify the list test fails**

Run: `go test ./internal/server -run 'TestListForUserIncludesShellBusySessions|TestTerminalSessionInterruptWritesCtrlC' -count=1`

Expected: the list test fails because the busy session is omitted; the existing interrupt implementation already passes its regression test.

- [x] **Step 3: Implement state propagation**

Remove `!session.shellBusy` from the `listForUser` predicate and add `ShellBusy bool` to `terminalSessionInfo`, populated from `session.shellBusy`.

- [x] **Step 4: Run focused manager tests**

Run: `go test ./internal/server -run 'TestListForUserIncludesShellBusySessions|TestTerminalSessionInterruptWritesCtrlC|TestEarliestOnlineForUserTargetDiagnosticsExplainMisses' -count=1`

Expected: PASS, including the existing proof that busy sessions remain excluded from automatic routing.

### Task 2: Expose the MCP status and usage contract

**Files:**
- Modify: `internal/server/mcp.go`
- Test: `internal/server/mcp_test.go`

- [x] **Step 1: Write a failing MCP response test**

Extend the existing authenticated MCP test: create a busy AI-enabled terminal session for the token owner, call `session_list`, and assert its structured response contains `shell_busy: true`. Keep the mapping in the existing `session_list` handler; do not add a new tool or mapping helper.

- [x] **Step 2: Run the MCP test and verify it fails**

Run: `go test ./internal/server -run 'TestMCPAcceptsUserToken' -count=1`

Expected: FAIL because `mcpSessionInfo` does not yet expose `shell_busy`.

- [x] **Step 3: Implement the MCP field and instructions**

Add `ShellBusy bool \`json:"shell_busy"\`` to `mcpSessionInfo`, map `item.ShellBusy`, and update `session_list` plus server instructions to say busy sessions may be inspected or interrupted but must not receive new commands.

- [x] **Step 4: Run focused and full verification**

Run: `go test ./internal/server -run 'Session|Interrupt|Busy|MCP' -count=1`

Expected: PASS.

Run: `go test ./internal/server -count=1`

Expected: PASS.

Run: `git diff --check`

Expected: no output.

### Task 3: Commit and release

**Files:**
- Modify: only the files named above and this plan.

- [ ] **Step 1: Commit the implementation**

Run: `git add internal/server/terminal_session_manager.go internal/server/terminal_session_manager_test.go internal/server/mcp.go internal/server/mcp_test.go docs/superpowers/plans/2026-07-24-mcp-busy-session-interrupt.md && git commit -m "fix: expose and interrupt busy MCP sessions"`

Expected: one commit that excludes unrelated working-tree files.

- [ ] **Step 2: Push, verify GitHub Actions, and deploy**

Push `main`, create the next bastion release tag using the repository's current release pattern, wait for its GitHub Actions run to succeed, verify the release checksum, replace the deployed binary while preserving the existing data and container configuration, and verify host-local plus public `/healthz` return `ok`.
