# DingTalk Login, Admin Console, and Modular Frontend Design

## Goal

Extend the current bastion branch with DingTalk login as the first real external identity provider, add system-level administration and stronger organization administration, and refactor the embedded frontend into an engineered module/component structure. Delivery must be proven by automated end-to-end coverage.

## Current State

The bastion branch already has SQLite-backed users, sessions, organizations, organization user groups, public keys, SSH targets, agent enrollment, command policies, audit logs, MCP tooling, and an embedded frontend under `web/`.

Authentication is currently local email/password only. User registration creates a personal organization. Organization membership has `owner`, `admin`, and `member` role constants, but the API and UI do not yet provide full member administration, owner transfer, or system-level admin management.

The frontend is a plain embedded ES module app, but most UI state and rendering live in `web/app.js`. It needs to be split into focused modules and view components before adding larger admin surfaces.

## Product Scope

This iteration delivers:

- DingTalk login as the real external login path.
- LDAP global configuration fields and admin UI entry, without real LDAP bind/login in this iteration.
- A default system admin account.
- System admin routes and UI for global settings, user management, and organization management.
- Organization owner/admin management of members, roles, user groups, and owner transfer.
- A modular embedded frontend with componentized views and API modules.
- Automated E2E tests that cover DingTalk account creation, organization assignment, admin authorization, organization role transfer, and frontend static module serving.

Local email/password login remains available.

## Identity Providers

### DingTalk

DingTalk is implemented as an OAuth-style external identity source using a configurable HTTP client. The backend stores the provider configuration in SQLite and performs code exchange against configured DingTalk-compatible endpoints.

The implementation must support real DingTalk configuration while keeping tests deterministic through a local mock DingTalk server. The provider settings include:

- enabled flag
- client id or app key
- client secret or app secret, stored encrypted where secret storage is already used
- authorization URL
- token URL
- userinfo URL
- redirect URL
- default organization id for auto-joining users
- default organization role, defaulting to `member`

Login flow:

1. Frontend opens the DingTalk authorization URL returned by `GET /api/auth/providers`.
2. DingTalk redirects to `/api/auth/dingtalk/callback?code=...&state=...`.
3. Backend validates state, exchanges code for access token, fetches userinfo, and resolves a stable external subject from `unionid`, `openid`, or provider user id.
4. If an external identity already exists, backend logs in that user.
5. If no identity exists but a user with the same verified email exists, backend binds DingTalk to that user and logs in.
6. If no user exists, backend creates one, creates the personal organization, binds the external identity, and optionally joins the configured default organization and its default all-members group.
7. Backend sets the normal session cookie and redirects to the frontend.

If DingTalk returns no email, the user email is generated as `<subject>@dingtalk.local` and the display name comes from DingTalk nickname/name fields. The UI should clearly show the resulting account identity.

### LDAP

LDAP is represented in global settings and the admin UI in this iteration. The stored fields include:

- enabled flag
- server URL
- bind DN
- encrypted bind password
- base DN
- user filter
- display name attribute
- email attribute
- default organization id

The login page may show LDAP as disabled or "configured by administrator" if enabled, but real LDAP bind/search login is not delivered in this slice. This avoids shipping two external identity protocols without full E2E coverage.

## System Administration

Add a system-level admin flag on users. A system admin can:

- view and edit global settings
- configure DingTalk and LDAP settings
- list users
- grant or revoke system admin for other users
- list organizations
- inspect and manage organization members
- transfer organization ownership if needed

First-run behavior:

- On database initialization, ensure a default admin user exists.
- Default email: `admin`.
- Default display name: `Administrator`.
- Password source priority:
  1. `--bootstrap-admin-password`
  2. `GOSSHD_BOOTSTRAP_ADMIN_PASSWORD`
  3. generated one-time password printed to startup logs
- The generated password is only printed when the admin user is created or when the existing admin has no usable local password hash.
- The admin user has a personal organization like every other user.

The `/api/me` response includes `is_system_admin`, provider summary, and organization memberships with role information so the frontend can render the right navigation.

## Organization Administration

Organizations continue to have exactly one `owner` as their manager. Organization roles:

- `owner`: full organization control, including transfer of ownership and deleting/removing members except where safety rules prevent it.
- `admin`: can manage members, user groups, invites, targets, policies, and audit visibility for the organization; cannot transfer owner.
- `member`: can use resources allowed by organization policy and manage their own keys.

Organization owner/admin APIs:

- list members with user id, email, display name, role, and groups
- add member by user id or email
- remove member
- update member role between `admin` and `member`
- transfer owner to another existing member
- create/list/delete user groups
- add/remove group members

Owner transfer rules:

- Only current organization owner or system admin can transfer ownership.
- Target user must already be an organization member.
- Transfer is transactional.
- New owner becomes `owner`.
- Previous owner becomes `admin` by default.
- The default all-members user group is preserved for both users.
- Personal organizations cannot transfer ownership.

Safety rules:

- The last organization owner cannot be removed.
- A user cannot remove themselves as owner; they must transfer ownership first.
- System admins can repair organization ownership through the system admin organization management endpoint.

## Storage Changes

Add or migrate the following:

- `users.is_system_admin INTEGER NOT NULL DEFAULT 0`
- `users.auth_provider TEXT NOT NULL DEFAULT 'local'` for display and legacy compatibility
- `users.disabled_at TEXT` reserved for future account disable support
- `external_identities` table:
  - `id`
  - `user_id`
  - `provider`
  - `subject`
  - `email`
  - `display_name`
  - `raw_profile_json`
  - `created_at`
  - `updated_at`
  - unique `(provider, subject)`
- `system_settings` table:
  - `key TEXT PRIMARY KEY`
  - `value_json TEXT NOT NULL`
  - `updated_at TEXT NOT NULL`
  - `updated_by TEXT`
- `oauth_states` table:
  - `state_hash`
  - `provider`
  - `redirect_after`
  - `expires_at`
  - `created_at`

Existing migrations stay append-only. Existing users remain local users with `is_system_admin = 0` unless they are the bootstrap admin.

## Backend API

Authentication:

- `GET /api/auth/providers`: returns enabled login providers and DingTalk authorization URL metadata.
- `GET /api/auth/dingtalk/start`: creates state and redirects to DingTalk authorization endpoint.
- `GET /api/auth/dingtalk/callback`: validates state, completes DingTalk login, creates/binds user, sets session cookie, redirects to `/`.
- Existing local `register`, `login`, `logout`, and `me` remain.

System admin:

- `GET /api/admin/settings`
- `PUT /api/admin/settings/dingtalk`
- `PUT /api/admin/settings/ldap`
- `GET /api/admin/users`
- `PATCH /api/admin/users/{id}`
- `GET /api/admin/orgs`
- `GET /api/admin/orgs/{id}/members`
- `PATCH /api/admin/orgs/{id}/members/{user_id}`
- `POST /api/admin/orgs/{id}/transfer-owner`

Organization administration:

- `GET /api/orgs/{id}/members`
- `POST /api/orgs/{id}/members`
- `PATCH /api/orgs/{id}/members/{user_id}`
- `DELETE /api/orgs/{id}/members/{user_id}`
- `POST /api/orgs/{id}/transfer-owner`

Authorization middleware:

- `requireUser`: any authenticated user.
- `requireSystemAdmin`: authenticated user with `is_system_admin`.
- `requireOrganizationAdmin`: organization `owner` or `admin`, or system admin.
- `requireOrganizationOwner`: organization `owner`, or system admin where explicitly allowed.

All APIs keep JSON error shape:

```json
{"error":"message"}
```

## Frontend Architecture

Keep embedded, no Node build requirement for this iteration. Refactor from single large `web/app.js` to plain ES module source files:

- `web/main.js`: bootstraps app and event delegation.
- `web/state.js`: central state object and update helpers.
- `web/api.js`: typed fetch methods grouped by domain.
- `web/router.js`: view selection and route helpers.
- `web/components/html.js`: escaping, raw HTML wrapper, template helper.
- `web/components/forms.js`: fields, selects, inline forms.
- `web/components/layout.js`: shell, sidebar, topbar, panels, tables, badges, empty states.
- `web/views/auth.js`: local login/register and DingTalk login entry.
- `web/views/dashboard.js`: metrics and core bastion overview.
- `web/views/orgs.js`: organization switch/create/join.
- `web/views/org-admin.js`: members, groups, roles, owner transfer.
- `web/views/keys.js`: public key management.
- `web/views/targets.js`: SSH target list, tag filters, create/edit forms.
- `web/views/agents.js`: agent enrollment commands.
- `web/views/policies.js`: command security group editor.
- `web/views/audit.js`: command audit table.
- `web/views/system-admin.js`: global settings, DingTalk/LDAP settings, users, organizations.

The frontend shell has:

- ordinary user navigation: Dashboard, SSH Services, Agents, Policies, Audit, Keys, Organization
- organization admin navigation when user is owner/admin: Members, User Groups, Owner Transfer
- system admin navigation when `is_system_admin`: System Settings, Users, Organizations, Identity Providers

The UI remains operational and dense enough for repeated work, while feeling polished: clear hierarchy, restrained palette, responsive tables, stable controls, subtle hover/focus states, and no marketing hero screen inside the app.

## E2E Requirements

Delivery is not complete until `go test ./...` passes and includes an E2E test that proves:

1. Fresh database bootstraps default `admin`.
2. Admin can log in locally.
3. Admin can configure DingTalk using a local mock DingTalk server.
4. DingTalk callback creates a new user automatically.
5. DingTalk-created user is added to the configured default organization and its default all-members group.
6. Admin can list users and organizations.
7. Organization owner can list members, promote a member to admin, demote back to member, and transfer owner.
8. After transfer, previous owner is `admin`, new owner is `owner`, and both remain in default group.
9. Non-admin member cannot manage members or transfer owner.
10. System admin can perform organization repair/transfer through admin APIs.
11. Frontend root and ES module assets are served by the embedded server.
12. Frontend includes visible DingTalk login and system admin navigation when API state indicates it.

The DingTalk E2E uses a local `httptest.Server` that implements token and userinfo endpoints. It must not depend on external DingTalk network availability.

## Testing Strategy

Unit tests:

- admin bootstrap creation and password source behavior
- DingTalk provider URL/state generation
- DingTalk callback account creation and identity binding
- organization authorization helpers
- owner transfer transaction rules
- settings repository JSON persistence

API integration tests:

- local admin login
- DingTalk mock login
- admin settings update
- admin user/org listing
- organization member role changes
- organization owner transfer
- forbidden responses for non-admin users

Frontend/static tests:

- `/`, `/main.js`, selected component modules, and SPA fallback return expected content
- `web/main.js` imports view modules without missing files
- auth view contains DingTalk login action
- system admin view module is embedded

Full verification:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
```

## Non-Goals

- Real LDAP bind/search login.
- SAML or generic OIDC providers.
- User disable enforcement beyond reserved storage fields.
- Multi-owner organizations.
- Node-based frontend build tooling.

## Completion Checklist

- DingTalk real provider flow exists and is covered by mock E2E.
- LDAP settings can be configured by system admin but cannot be selected as a working login path.
- Default admin account exists on fresh DB and can access admin APIs.
- `/api/me` exposes enough role/admin metadata for frontend routing.
- Organization owner/admin member management works.
- Organization ownership transfer works transactionally.
- Frontend is split into focused modules/components.
- Embedded frontend serves all modules and keeps existing API/install routes working.
- `go test ./...` passes.
