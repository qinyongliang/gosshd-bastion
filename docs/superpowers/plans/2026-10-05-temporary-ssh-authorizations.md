# Temporary SSH Authorizations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create, list, renew, and delete per-service UUID authorizations that allow SSH without a client key, with a configurable lifetime defaulting to 24 hours.

**Architecture:** Persist UUID bearer credentials encrypted with the existing secret box, index their SHA-256 hashes, and scope each authorization to one SSH target and its creator. The SSH `none` authentication callback accepts only a valid grant; ordinary aliases still require public-key authentication. Commands use the creator's existing policies and audit identity. Revalidate live temporary connections every second and before opening a channel so expiration, deletion, disabled creators, or removed access close those connections.

**Tech Stack:** Go, SQLite migrations, x/crypto/ssh, React, TanStack Query, Ant Design, Playwright.

---

### Task 1: Persistence

**Files:** Create `internal/store/migrations/0004_temporary_ssh_authorizations.sql`, `internal/store/temporary_ssh_authorizations.go`, and `internal/store/temporary_ssh_authorizations_test.go`; update migration version assertions in `internal/store/migration_runner_test.go`.

- [ ] Add store tests for UUID generation, encrypted persistence, scoped listing, renewal, deletion, target/user cascade, and persistence after reopen. Run `go test ./internal/store -run 'TemporarySSH|MainSchemaMigrations' -count=1` before and after implementation.
- [ ] Implement these contracts with parameterized SQL and existing `formatTime`, `parseTime`, `sealSecret`, `openSecret`, and `requireRowsAffected` helpers:

```go
type TemporarySSHAuthorization struct {
    ID, TargetID, Name, Token, CreatedBy string
    ExpiresAt, CreatedAt, UpdatedAt time.Time
}
type CreateTemporarySSHAuthorizationParams struct {
    TargetID, Name, CreatedBy string
    ExpiresAt time.Time
}
// Repository methods:
CreateTemporarySSHAuthorization(context.Context, CreateTemporarySSHAuthorizationParams) (TemporarySSHAuthorization, error)
ListTemporarySSHAuthorizations(context.Context, string) ([]TemporarySSHAuthorization, error)
GetTemporarySSHAuthorization(context.Context, string) (TemporarySSHAuthorization, error)
GetTemporarySSHAuthorizationByToken(context.Context, string) (TemporarySSHAuthorization, error)
RenewTemporarySSHAuthorization(context.Context, string, string, time.Duration, time.Time) (TemporarySSHAuthorization, error)
DeleteTemporarySSHAuthorization(context.Context, string, string) error
```

Renewal adds the selected duration to `max(expires_at, now)` and retains the credential. The two string arguments in renewal/deletion are authorization ID and target ID. Foreign keys cascade when the target or creator is deleted; index `target_id` and give `token_hash` a unique index. Use an SQL transaction for read/update renewal.

### Task 2: Management API and SSH routing

**Files:** Create `internal/server/api_temporary_ssh_authorizations.go`, `internal/server/temporary_ssh_authorizations.go`, and focused API/SSH test files; update `internal/server/api.go`, `internal/server/ssh.go`, and `internal/server/ssh_bastion.go`.

- [ ] Implement authenticated routes, verifying `targetForUser` before all operations and validating authorization target IDs on renewal/deletion:

```text
GET  /api/targets/{id}/temporary-authorizations -> {authorizations: []}
POST /api/targets/{id}/temporary-authorizations -> 201 {authorization: object}
POST /api/targets/{id}/temporary-authorizations/{authorization_id}/renew -> {authorization: object}
DELETE /api/targets/{id}/temporary-authorizations/{authorization_id} -> 204
```

Requests accept `name` for creation and optional integer `duration_seconds`; missing duration defaults to 86400. Reject zero, negative, fractional, and overflow durations. Objects expose `id`, `target_id`, `name`, `token`, `created_by`, `expires_at`, `created_at`, and `updated_at`.

Ordinary members may list, renew, and delete only grants they created. Organization owners/admins and system admins may manage every grant for accessible services. This prevents revealing another creator's UUID and inheriting a more privileged policy identity. Add a regression covering ownership and current target visibility.

- [ ] Test default and custom lifetime, unchanged UUID on renewal, expired renewal, validation, unauthenticated requests, cross-target UUID IDs, users outside the owning organization, inaccessible services, and deletion.
- [ ] Set `NoClientAuth: true` with a conditional `NoClientAuthCallback`; return permissions containing `user_id` and `temporary_authorization_id` only after expiry, enabled creator, and target access checks. Keep the normal `PublicKeyCallback`.
- [ ] Resolve temporary connections by the grant's target ID rather than aliases, monitor revocation, and do not log the bearer UUID. A temporary grant must never permit access to another target or bypass command policy.
- [ ] Run real SSH tests using `ssh.ClientConfig{User: grant.Token, Auth: nil}`: successful exec and audit, rejected random/expired/deleted credentials, ordinary aliases requiring keys, policy denial, creator disable/access removal, and existing connections closing after expiry/deletion. Run `go test ./internal/server ./internal/store ./internal/auth ./internal/bastion -count=1`.

### Task 3: Console UI

**Files:** Create `web/src/components/TemporarySSHAuthorizationsModal.tsx`; update `web/src/pages/TargetsPage.tsx`, `web/src/types.ts`, `web/src/api.ts`, `web/src/i18n.tsx`, and scoped styles in `web/styles.css`.

- [ ] Add a per-service **Temporary authorizations / 临时授权** action opening a manager for direct services and private nodes. Creation has an optional name and an hours input defaulting to 24; use `Math.round(hours * 3600)` for the API.
- [ ] List grants with UUID, expiration, active/expired status, and a copyable `ssh -p PORT UUID@HOST` command. Each grant supports duration-configurable renewal and deletion with confirmation. Refresh the list after mutations; surface query/mutation errors and pending state.
- [ ] Use the site's advertised SSH host/port, bilingual labels, and single-line horizontally scrollable commands. Keep product copy focused on connection behavior and validity; this is not the existing command manual-review authorization feature.
- [ ] Validate `pnpm check` and `pnpm build`, then add a meaningful Playwright regression for default duration, create, copied UUID command, renew retaining UUID, and delete through the manager. Check desktop and narrow layouts.

### Task 4: Review and publish

The user added mobile click failures to this release. Fix shared dropdown visibility/positioning, let the component library manage nested modal layering, supply a stable accessible label on the More trigger, and keep the desktop notification-permission invitation from covering narrow-screen controls. Keep actual pending-review cards available. `web/e2e/mobile_interactions.test.mjs` verifies navigation, folder creation, service edit/drawer close, and credentials dialogs at 390, 768, 1024, 1440, and 1800 px. Register it in `internal/server/ui_e2e_test.go`.

- [ ] Review specification compliance and then code quality; fix actionable findings and re-run affected checks.
- [ ] Commit the verified feature, fast-forward local `main`, push main plus the next unused `v0.1.N-bastion` tag, and wait for all release builds to succeed.
- [ ] Deploy SHA-256-verified release packages to `ssh.jsydf.cn` on `118.24.118.205` (amd64) and `qyl.my.to:8880` (arm64), preserving configuration and backing up binaries and both databases before upgrade.
- [ ] Verify running version, public/local health, initialized APIs, and published frontend; retain backups, remove obsolete stopped containers, and report the entry point and connection form.
