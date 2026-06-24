# GOSSHD Client App Design

## Goal

Build a GOSSHD single-machine client edition of the bastion product. The client runs as a Windows desktop application using Wails, starts an embedded local GOSSHD server in client mode, opens local Web UI pages, and presents a single-user resource/connection/audit workflow without exposing users, organizations, members, invites, user groups, or backend administration pages.

## Product Shape

The existing server and web console remain available for the original managed deployment. The new client edition adds a `client_mode` runtime that reuses the existing bastion core: targets, policies, manual review, terminal sessions, file manager, audit logs, and terminal replay.

In client mode:

- The server starts with a built-in local user named `user`.
- The server ensures a personal owner scope for that user and returns an authenticated `/api/me` response without showing a login page.
- The web UI hides all multi-tenant concepts and all system administration surfaces.
- The audit UI hides user-related columns and filters.
- The desktop menu opens resource, audit, settings, and about windows.
- Clicking a resource connection opens a dedicated connection window. Each connection window may still contain terminal, file, host information, and workspace tabs for that target.

## Backend Design

Add `Config.ClientMode bool` and a `--client-mode` CLI flag.

When `client_mode` is enabled, `ensureServices` must call a client bootstrap path instead of the normal bootstrap-admin path. The client bootstrap creates or updates a local account:

- email: `user`
- display name: `user`
- auth provider: local
- system admin: false

The existing repository behavior that creates a personal organization for every user is kept. That internal owner scope is still used for target, policy, prompt, and audit APIs, but it is not exposed as a user-facing organization in the client UI.

`userForRequest` returns the built-in client user in client mode for loopback requests. Cookies remain supported for compatibility, but the Wails client and local UI do not need a login form.

`/api/me` includes `runtime.client_mode = true` so the frontend can select the client experience without guessing from URL or build flags.

## Frontend Design

Add `client_mode` to the `Runtime` type and use it to branch the shell and route set.

Client mode routes:

- `/` redirects to `/targets`.
- `/targets` shows the target/resource list.
- `/targets/:targetID/connect` keeps the existing connection workbench.
- `/policies` remains available for command review and capability settings.
- `/audit` shows audit records without user columns or user filters.
- `/settings` shows a compact local settings surface if needed.

Client mode navigation excludes:

- organizations
- members
- keys as a user-management concept
- system admin
- login/register/logout
- organization switcher
- user profile block

Existing pages can be reused where their visible fields match single-machine requirements. Where a page exposes multi-tenant fields, gate those fields behind `!runtime.client_mode`.

## Desktop Client Design

Create a new `client/wails` project for a Wails shell. The shell is responsible for:

- starting the existing Go server in process with `ClientMode`, loopback HTTP and SSH listeners, and a local data directory
- opening a Wails desktop window that hosts the local web UI
- adding menu entries for resources, audit, settings, and about
- opening connection pages in independent windows
- shutting down the embedded server process when the client exits

The shell does not reimplement bastion behavior. All domain logic stays in Go and React.

## Packaging

The client package should contain:

- Wails desktop executable
- built web assets embedded in the server binary
- optional agent binaries if local private-node registration remains available
- a local data directory template or first-run creation logic

The initial repository work may include source and project files even if the current development machine cannot compile the Windows client because Wails build dependencies may be unavailable.

The repository includes a Windows package script, `build-client-windows.ps1`, that builds web assets, builds the Wails shell, and creates a zip package when run on a machine with Go, pnpm, and Wails build prerequisites.

## Testing

Backend tests should verify:

- `--client-mode` initializes the built-in `user`.
- `/api/me` succeeds without a login cookie in client mode.
- `/api/me` includes `runtime.client_mode = true`.
- normal non-client authentication behavior remains unchanged.

Frontend checks should verify:

- TypeScript compiles.
- client mode does not render organization/member/system-admin navigation.
- audit user columns are hidden in client mode.

Desktop shell verification is source-level unless Wails build prerequisites are available. A follow-up Windows machine with Wails prerequisites should run `.\build-client-windows.ps1 -Runtime win-x64`.
