# Console UX and UI Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rebuild every console page and shared component around a clean, predictable CRUD interaction model without removing existing capabilities.

**Architecture:** Keep the existing React Router, React Query, API client, and page routes. Replace the shared shell and UI primitives first, then migrate each page to consistent page headers, toolbars, data panels, drawers/modals, inline validation, mutation pending states, and non-blocking feedback. Preserve API calls and add missing update/delete affordances where the API already supports them.

**Tech Stack:** React 19, TypeScript, React Query, React Router, Lucide icons, CSS design tokens.

---

### Task 1: Shared interaction primitives

**Files:**
- Modify: `web/src/components/ui.tsx`
- Modify: `web/styles.css`

- [x] Add clean `Panel`, `Modal`, `Drawer`, `Toolbar`, `SimpleTable`, `Metric`, `Empty`, and `NavButton` structures.
- [ ] Add reusable `ConfirmDialog`, `ErrorMessage`, `InlineNotice`, and `SaveBar` components.
- [ ] Ensure all modal and drawer bodies have consistent focus, spacing, close, and submit affordances.
- [ ] Add styles for loading, disabled, error, success, selected, and destructive states.
- [ ] Run `pnpm run check`.

### Task 2: Application shell

**Files:**
- Modify: `web/src/layout/Shell.tsx`
- Modify: `web/src/App.tsx`

- [ ] Group navigation into workspace and administration sections.
- [ ] Add consistent connection status, active organization context, responsive navigation, and account actions.
- [ ] Keep all existing routes and keyboard shortcut behavior.
- [ ] Run `pnpm run check`.

### Task 3: Resource management pages

**Files:**
- Modify: `web/src/pages/TargetsPage.tsx`
- Modify: `web/src/pages/PoliciesPage.tsx`
- Modify: `web/src/pages/KeysPage.tsx`

- [ ] Standardize page headers and primary actions.
- [ ] Replace browser prompts/confirms with in-app dialogs.
- [ ] Keep create/edit/delete/copy/move/bind flows and show pending/error/success states.
- [ ] Ensure SSH targets, credentials, folders, policies, keys, and MCP tokens expose complete CRUD affordances.
- [ ] Run `pnpm run check` and `pnpm run build`.

### Task 4: Organization and administration pages

**Files:**
- Modify: `web/src/pages/OrganizationsPage.tsx`
- Modify: `web/src/pages/MembersPage.tsx`
- Modify: `web/src/pages/SystemAdminPage.tsx`

- [ ] Convert organization, member, role, group, account, and provider actions to explicit modal/drawer workflows.
- [ ] Add confirmation dialogs for destructive operations and visible mutation errors.
- [ ] Keep permission-sensitive behavior unchanged.
- [ ] Run `pnpm run check`.

### Task 5: Dashboard, audit, authentication, and terminal surfaces

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/pages/AuditPage.tsx`
- Modify: `web/src/pages/AuthPage.tsx`
- Modify: `web/src/pages/ConnectPage.tsx`
- Modify: `web/src/pages/FileManager.tsx`
- Modify: `web/src/pages/LocalTerminalPage.tsx`

- [ ] Standardize filters, pagination, empty/loading/error states, and action feedback.
- [ ] Preserve terminal tabs, split panes, file operations, uploads, editor save behavior, and dirty-file protection.
- [ ] Replace remaining browser alerts/confirms where practical.
- [ ] Run `pnpm run check`, `pnpm run build`, and existing E2E tests.

### Task 6: Final visual and interaction verification

**Files:**
- Modify: `web/styles.css`
- Test: `web/e2e/*.test.mjs`

- [ ] Verify every route at desktop and mobile widths.
- [ ] Verify keyboard focus, modal escape behavior, disabled pending states, and mutation error recovery.
- [ ] Run `pnpm run check`, `pnpm run build`, and all available E2E tests.
