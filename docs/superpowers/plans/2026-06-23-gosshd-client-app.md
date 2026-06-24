# GOSSHD Client App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a first working GOSSHD single-machine Wails client edition that starts an embedded local server and hides multi-tenant UI concepts.

**Architecture:** Add a backend `client_mode` runtime to reuse existing Go services with an automatic built-in `user`. Add frontend runtime branching so the React app becomes a single-user client surface. Add a Wails shell project under `client/wails` that starts the local server in-process and hosts it in a desktop window.

**Tech Stack:** Go, SQLite, React, TypeScript, Vite, Wails.

---

### Task 1: Backend Client Mode

**Files:**
- Modify: `cmd/gosshd-server/main.go`
- Modify: `internal/server/config.go`
- Modify: `internal/server/app.go`
- Modify: `internal/server/api.go`
- Test: `internal/server/client_mode_test.go`

- [ ] Add `Config.ClientMode bool` and a `--client-mode` flag.
- [ ] Add `ClientMode bool json:"client_mode"` to `apiRuntime`.
- [ ] In `ensureServices`, call a client bootstrap helper when `ClientMode` is enabled.
- [ ] Make `userForRequest` return the built-in client user without requiring a cookie in client mode.
- [ ] Add tests proving `/api/me` works without login only in client mode.
- [ ] Run `go test ./internal/server ./internal/store ./internal/auth`.

### Task 2: Frontend Client Mode

**Files:**
- Modify: `web/src/types.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/layout/Shell.tsx`
- Modify: `web/src/pages/AuditPage.tsx`

- [ ] Add `client_mode` to the frontend `Runtime` type.
- [ ] Skip login UI when `/api/me` succeeds in client mode.
- [ ] Render a client route set that excludes organizations, members, keys, system admin, and logout.
- [ ] Hide organization switcher, user profile block, and active organization text in the shell when `runtime.client_mode` is true.
- [ ] Hide audit user column and user-oriented search copy in client mode.
- [ ] Run `pnpm run check`.

### Task 3: Wails Windows Shell

**Files:**
- Create: `client/wails/main.go`
- Create: `client/wails/app.go`
- Create: `client/wails/frontend/dist/index.html`
- Create: `client/wails/wails.json`
- Create: `client/wails/README.md`
- Create: `build-client-windows.ps1`

- [ ] Add a Wails project shell.
- [ ] Start the existing Go server in-process with `ClientMode`, local HTTP, local SSH, and local data paths.
- [ ] Open the main window at the local `/targets` URL through the Wails shell.
- [ ] Add menu entries for resources, audit, settings, about, and a connection-window URL prompt.
- [ ] Add a package script that builds the Wails client into `package/gosshd-client-<runtime>`.
- [ ] Shut down the embedded server on application exit.
- [ ] Document that Wails build prerequisites are required for packaging.

### Task 4: Verification

**Files:**
- Modify only files needed to fix failures discovered by the commands below.

- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test ./internal/server ./internal/store ./internal/auth`.
- [ ] Run `pnpm run check`.
- [ ] Run `pnpm run build` if TypeScript check passes.
- [ ] Start `gosshd-server --client-mode` on loopback and verify `/targets`, `/audit`, and `/policies` render without organization, member, user, or user-group UI.
- [ ] Record the Wails package command result.

## Verification Notes

In the current workspace, `go test ./internal/server ./internal/store ./internal/auth`, `go test ./client/wails`, `tsc -b --pretty false`, and `vite build` pass. A local loopback `--client-mode` server was opened in the in-app browser; `/targets`, `/audit`, and the policy edit drawer showed only resource, audit, service, tag, and policy controls, with no user, organization, member, system-admin, or user-group controls visible. `build-client-windows.ps1 -SkipWebBuild` successfully builds `package/gosshd-client-win-x64/GOSSHD.exe` and `package/gosshd-client-win-x64-dev.zip` when Wails CLI is installable.

## Self-Review

The plan covers the approved spec: backend single-user client mode, frontend single-machine UI, Wails shell, and verification. It intentionally keeps the existing multi-tenant backend logic in place for compatibility and hides those concepts only in client mode.
