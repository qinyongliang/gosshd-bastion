# DingTalk Admin Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add DingTalk login, system administration, organization member/owner management, and a modular embedded frontend to the existing bastion branch, with complete E2E coverage.

**Architecture:** Extend the current SQLite repository and HTTP API instead of replacing the existing bastion flow. Keep local login and current SSH/agent behavior intact, add DingTalk as an external identity service with mockable endpoints, add authorization middleware for system and organization admins, and refactor the frontend into plain ES modules served by Go embed.

**Tech Stack:** Go 1.26, `database/sql`, `modernc.org/sqlite`, existing cookie auth, plain ES modules, embedded static files, Go API/integration/E2E tests.

---

## File Structure

- Modify `internal/store/models.go`: add system admin, external identity, settings, OAuth state, organization member DTOs.
- Modify `internal/store/migrations.go`: append users/settings/identity/state migrations.
- Modify `internal/store/repository.go`: add bootstrap admin, settings, identity, OAuth state, member management, owner transfer, user/org admin list methods.
- Modify `internal/store/store_test.go`: cover bootstrap admin, settings, identity, member role, owner transfer.
- Modify `internal/auth/service.go`: expose local login compatibility and external login/session creation helpers.
- Create `internal/auth/dingtalk.go`: DingTalk provider client, auth URL/state, callback exchange.
- Modify `internal/auth/service_test.go`: cover DingTalk mock flow.
- Modify `internal/server/config.go`: add bootstrap admin password/env and public base URL fields if needed.
- Modify `cmd/gosshd-server/main.go`: add bootstrap admin password flag.
- Modify `internal/server/api.go`: add admin and provider routes plus authorization middleware.
- Modify `internal/server/api_auth.go`: expose provider list and DingTalk start/callback.
- Create `internal/server/api_admin.go`: system settings/users/org admin handlers.
- Modify `internal/server/api_orgs.go`: add member list/add/update/remove and owner transfer.
- Modify `internal/server/api_groups.go`: reuse org admin authorization.
- Modify `internal/server/api_test.go`: add DingTalk/admin/org-management API tests.
- Modify `internal/server/e2e_bastion_test.go`: add full DingTalk/admin/org transfer E2E assertions.
- Replace `web/app.js` with modular boot file or move to `web/main.js`.
- Modify `web/index.html`: load `main.js`.
- Modify `web/api.js`: group auth/admin/org APIs.
- Create `web/state.js`, `web/router.js`, `web/components/html.js`, `web/components/forms.js`, `web/components/layout.js`.
- Create `web/views/auth.js`, `web/views/dashboard.js`, `web/views/orgs.js`, `web/views/org-admin.js`, `web/views/keys.js`, `web/views/targets.js`, `web/views/agents.js`, `web/views/policies.js`, `web/views/audit.js`, `web/views/system-admin.js`.
- Modify `web/styles.css`: support new shell/components.
- Modify `internal/server/web_embed_test.go`: assert module assets and fallback.
- Modify `README.md` and `README.zh-CN.md`: document admin bootstrap, DingTalk config, LDAP config placeholder, and E2E command.

## Cross-Cutting Rules

- Keep migrations append-only.
- Do not break existing local login, SSH alias routing, MCP, agent enrollment, or static serving tests.
- Secrets in settings are stored encrypted where existing secret encryption helpers are available; if not available for settings yet, store only test-safe placeholder values until encryption is added in the same task.
- DingTalk E2E must use a local mock HTTP server, never the real DingTalk network.
- Run commands with:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
```

---

### Task 1: Store and Bootstrap Admin

**Files:**
- Modify `internal/store/models.go`
- Modify `internal/store/migrations.go`
- Modify `internal/store/repository.go`
- Modify `internal/store/store_test.go`

- [ ] **Step 1: Add failing store tests**

Add tests:

```go
func TestRepositoryEnsuresBootstrapAdmin(t *testing.T)
func TestRepositorySystemSettingsExternalIdentityAndOAuthState(t *testing.T)
func TestRepositoryOrganizationMembersRolesAndOwnerTransfer(t *testing.T)
```

Assertions:

- fresh DB creates admin user when `EnsureBootstrapAdmin(ctx, "admin-pass")` is called
- admin has `IsSystemAdmin == true`
- admin can login later using stored password hash
- settings round-trip JSON by key
- external identity unique `(provider, subject)` maps to user
- OAuth state can be created, consumed once, and expires
- organization members list includes role
- owner transfer makes target `owner` and previous owner `admin`
- personal organization owner transfer returns an error

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/store -run "TestRepository(EnsuresBootstrapAdmin|SystemSettings|OrganizationMembers)" -v
```

Expected: compile failure for missing model/repository methods.

- [ ] **Step 3: Implement schema and repository**

Add fields and tables from the spec:

- `users.is_system_admin`
- `users.auth_provider`
- `users.disabled_at`
- `external_identities`
- `system_settings`
- `oauth_states`

Add repository methods:

- `EnsureBootstrapAdmin(ctx, password string) (User, string, error)`
- `ListUsers(ctx) ([]User, error)`
- `UpdateUserSystemAdmin(ctx, userID string, isAdmin bool) error`
- `UpsertSystemSetting(ctx, key string, valueJSON []byte, updatedBy string) error`
- `GetSystemSetting(ctx, key string) ([]byte, error)`
- `CreateExternalIdentity(ctx, params CreateExternalIdentityParams) (ExternalIdentity, error)`
- `GetExternalIdentity(ctx, provider, subject string) (ExternalIdentity, error)`
- `CreateOAuthState(ctx, provider, rawState, redirectAfter string, expiresAt time.Time) error`
- `ConsumeOAuthState(ctx, provider, rawState string) (OAuthState, error)`
- `ListOrganizationMembers(ctx, orgID string) ([]OrganizationMemberWithUser, error)`
- `UpdateOrganizationMemberRole(ctx, orgID, userID, role string) error`
- `RemoveOrganizationMember(ctx, orgID, userID string) error`
- `TransferOrganizationOwner(ctx, orgID, newOwnerID, previousOwnerRole string) error`

- [ ] **Step 4: Run store tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/store -v
```

Expected: PASS.

---

### Task 2: DingTalk Auth Service

**Files:**
- Modify `internal/auth/service.go`
- Create `internal/auth/dingtalk.go`
- Modify `internal/auth/service_test.go`
- Modify `internal/store/repository.go`

- [ ] **Step 1: Add failing auth tests**

Add tests:

```go
func TestDingTalkLoginCreatesUserAndIdentity(t *testing.T)
func TestDingTalkLoginBindsExistingEmail(t *testing.T)
func TestDingTalkLoginRejectsInvalidState(t *testing.T)
```

Use `httptest.Server` for `/token` and `/userinfo`. The mock returns stable `unionid`, `openid`, `email`, and `name`.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/auth -run TestDingTalk -v
```

Expected: missing DingTalk service.

- [ ] **Step 3: Implement DingTalk service**

Add:

- `DingTalkConfig`
- `DingTalkUserInfo`
- `BuildDingTalkAuthURL(ctx, cfg, redirectAfter string) (string, error)`
- `CompleteDingTalkLogin(ctx, cfg, code, state string) (store.User, string, error)`

The complete flow validates state, exchanges code, fetches userinfo, finds/binds/creates local user, creates a session, and joins configured default organization when present.

- [ ] **Step 4: Run auth tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/auth ./internal/store -v
```

Expected: PASS.

---

### Task 3: Admin and Organization Management APIs

**Files:**
- Modify `internal/server/api.go`
- Modify `internal/server/api_auth.go`
- Create `internal/server/api_admin.go`
- Modify `internal/server/api_orgs.go`
- Modify `internal/server/api_groups.go`
- Modify `internal/server/config.go`
- Modify `cmd/gosshd-server/main.go`
- Modify `internal/server/api_test.go`

- [ ] **Step 1: Add failing API tests**

Add tests:

```go
func TestAPIBootstrapAdminAndAdminSettings(t *testing.T)
func TestAPIDingTalkMockLoginCreatesAndAssignsUser(t *testing.T)
func TestAPIOrganizationMemberRoleAndOwnerTransfer(t *testing.T)
func TestAPIOrganizationMemberManagementForbiddenForMember(t *testing.T)
```

Assertions:

- default admin can login
- non-admin cannot read `/api/admin/settings`
- admin can configure DingTalk and LDAP settings
- mock DingTalk callback creates a user and adds to default org
- owner promotes member to admin, demotes to member, transfers owner
- previous owner becomes admin
- member cannot manage members or transfer owner

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run "TestAPI(Bootstrap|DingTalk|OrganizationMember)" -v
```

Expected: missing routes.

- [ ] **Step 3: Wire bootstrap admin**

On `ensureServices`, call `EnsureBootstrapAdmin` using config/env password. Add config field and CLI flag:

- `BootstrapAdminPassword string`
- flag `--bootstrap-admin-password`
- env fallback `GOSSHD_BOOTSTRAP_ADMIN_PASSWORD`

- [ ] **Step 4: Implement auth provider/admin/org routes**

Routes:

- `GET /api/auth/providers`
- `GET /api/auth/dingtalk/start`
- `GET /api/auth/dingtalk/callback`
- `GET /api/admin/settings`
- `PUT /api/admin/settings/dingtalk`
- `PUT /api/admin/settings/ldap`
- `GET /api/admin/users`
- `PATCH /api/admin/users/{id}`
- `GET /api/admin/orgs`
- `GET /api/admin/orgs/{id}/members`
- `PATCH /api/admin/orgs/{id}/members/{user_id}`
- `POST /api/admin/orgs/{id}/transfer-owner`
- `GET /api/orgs/{id}/members`
- `POST /api/orgs/{id}/members`
- `PATCH /api/orgs/{id}/members/{user_id}`
- `DELETE /api/orgs/{id}/members/{user_id}`
- `POST /api/orgs/{id}/transfer-owner`

- [ ] **Step 5: Run API tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run "TestAPI" -v
```

Expected: PASS.

---

### Task 4: Modular Frontend Refactor

**Files:**
- Modify `web/index.html`
- Modify `web/api.js`
- Replace `web/app.js` or keep as compatibility loader
- Create `web/main.js`
- Create `web/state.js`
- Create `web/router.js`
- Create `web/components/html.js`
- Create `web/components/forms.js`
- Create `web/components/layout.js`
- Create `web/views/auth.js`
- Create `web/views/dashboard.js`
- Create `web/views/orgs.js`
- Create `web/views/org-admin.js`
- Create `web/views/keys.js`
- Create `web/views/targets.js`
- Create `web/views/agents.js`
- Create `web/views/policies.js`
- Create `web/views/audit.js`
- Create `web/views/system-admin.js`
- Modify `web/styles.css`
- Modify `internal/server/web_embed_test.go`

- [ ] **Step 1: Add failing static/module tests**

Extend `TestWebAppServesIndexAndStaticAssets` or add:

```go
func TestWebAppServesModularFrontendAssets(t *testing.T)
```

Assert:

- `/` includes `main.js`
- `/main.js`, `/state.js`, `/router.js`, `/views/auth.js`, `/views/system-admin.js`, `/components/layout.js` return JavaScript
- auth view contains DingTalk login action text or route
- system admin view contains settings/users/org management text
- `/targets` still serves SPA index

- [ ] **Step 2: Run static tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestWebApp -v
```

Expected: missing modular assets.

- [ ] **Step 3: Refactor frontend modules**

Move rendering logic into views and components. Keep event delegation in `main.js`. Keep no-build embedded delivery. Include:

- DingTalk login button on auth screen
- system admin nav when `state.user.is_system_admin`
- organization admin/member management views
- owner transfer form
- existing target, agent, policy, audit, keys functionality

- [ ] **Step 4: Run static tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestWebApp -v
```

Expected: PASS.

---

### Task 5: Full E2E and Documentation

**Files:**
- Modify `internal/server/e2e_bastion_test.go`
- Modify `README.md`
- Modify `README.zh-CN.md`

- [ ] **Step 1: Extend E2E test**

Extend `TestBastionE2E` or add `TestDingTalkAdminOrganizationE2E` to prove:

1. Fresh DB bootstraps admin.
2. Admin logs in.
3. Admin configures DingTalk with mock server and LDAP settings.
4. DingTalk callback creates a user.
5. User joins configured default organization and all-members group.
6. Admin lists users and organizations.
7. Owner promotes/demotes member and transfers owner.
8. Previous owner becomes admin.
9. Member cannot manage members.
10. System admin can repair/transfer ownership.
11. Frontend root and modules serve.

- [ ] **Step 2: Run E2E**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run "TestBastionE2E|TestDingTalkAdminOrganizationE2E" -v
```

Expected: PASS.

- [ ] **Step 3: Update docs**

Document:

- admin bootstrap password flag/env
- DingTalk settings fields
- LDAP settings are configurable but LDAP login is not active in this slice
- organization owner/admin/member behavior
- owner transfer behavior
- E2E command

- [ ] **Step 4: Full verification**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
go build ./cmd/gosshd-server
go build ./cmd/gosshd-agent
git status --short --branch
```

Expected: all tests/builds pass and only intended changes remain.

---

## Self-Review Checklist

- The plan covers DingTalk real flow, LDAP config-only scope, admin bootstrap, system admin UI/API, organization member management, owner transfer, frontend modules, and E2E.
- No task relies on external DingTalk network availability.
- No task introduces Node build tooling.
- Local login and existing bastion behavior remain covered by existing tests.
- Completion requires `go test ./...`.
