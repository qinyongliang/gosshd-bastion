# Mobile Lists And Flat Background Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make shared application lists readable as cards on phones and remove decorative grid textures from application backgrounds on mobile and desktop.

**Architecture:** Add cell labels once in the shared `SimpleTable`, then use mobile CSS to preserve semantic tables while presenting rows as cards. Keep specialized file and tree lists intact, adjust only their mobile spacing, and remove repeating grid layers from existing background declarations without adding components or dependencies.

**Tech Stack:** React 19, TypeScript, CSS, Playwright browser E2E, Go test wrapper

---

### Task 1: Add Failing Mobile List And Background Checks

**Files:**
- Modify: `web/e2e/ui_e2e.mjs:43-216`

- [ ] **Step 1: Assert shared tables expose mobile card labels**

In the mobile-only branch, set the viewport before navigation, open the organizations page, and inspect the first shared table row.

```js
await page.setViewportSize({ width: mobileViewportWidth, height: 844 });
await page.goto(`${baseURL}/organizations`, { waitUntil: "domcontentloaded" });
await page.locator(".simple-table tbody tr").first().waitFor();
const mobileTable = await page.locator(".simple-table").evaluate((element) => {
  const table = element.querySelector("table");
  const header = element.querySelector("thead");
  const cell = element.querySelector("tbody td[data-label]");
  if (!(table instanceof HTMLElement) || !(header instanceof HTMLElement) || !(cell instanceof HTMLElement)) throw new Error("mobile shared table is missing");
  return {
    tableDisplay: getComputedStyle(table).display,
    headerDisplay: getComputedStyle(header).display,
    cellLabel: cell.dataset.label || "",
    bodyOverflow: document.documentElement.scrollWidth - innerWidth,
  };
});
if (mobileTable.tableDisplay !== "block" || mobileTable.headerDisplay !== "none" || !mobileTable.cellLabel || mobileTable.bodyOverflow > 1) {
  throw new Error(`mobile shared table is not a card list: ${JSON.stringify(mobileTable)}`);
}
```

- [ ] **Step 2: Assert representative backgrounds contain no grid sizing**

After opening the connect page, inspect the workspace background and terminal pseudo-element.

```js
const backgrounds = await page.evaluate(() => {
  const workspace = document.querySelector(".connect-workspace");
  const terminal = document.querySelector(".terminal-panel");
  if (!(workspace instanceof HTMLElement) || !(terminal instanceof HTMLElement)) throw new Error("background surfaces are missing");
  return {
    workspaceSize: getComputedStyle(workspace).backgroundSize,
    terminalTexture: getComputedStyle(terminal, "::before").backgroundImage,
  };
});
if (/\d+px\s+\d+px/.test(backgrounds.workspaceSize) || backgrounds.terminalTexture !== "none") {
  throw new Error(`decorative grid background remains: ${JSON.stringify(backgrounds)}`);
}
```

- [ ] **Step 3: Run the 390px browser test and verify failure**

Run in PowerShell with the existing browser E2E environment variables:

```powershell
$env:GOSSHD_UI_E2E_VIEWPORT_WIDTH='390'
go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1 -v
```

Expected: FAIL because `.simple-table`, `data-label`, and flat connect backgrounds do not exist yet.

### Task 2: Implement Shared Mobile Cards And Flat Backgrounds

**Files:**
- Modify: `web/src/components/ui.tsx:157-159`
- Modify: `web/styles.css:649-658`
- Modify: `web/styles.css:1597-1633`
- Modify: `web/styles.css:4654-4698`
- Modify: `web/styles.css:5995-6225`
- Modify: `web/styles.css:7012-7259`
- Modify: `web/styles.css:7273-7300`
- Modify: `web/styles.css:7878-7882`

- [ ] **Step 1: Label shared table cells once**

Add a stable class to the shared wrapper and copy each header into the matching cell.

```tsx
export function SimpleTable({ headers, rows }: { headers: string[]; rows: ReactNode[][] }) {
  return <div className="table-wrap simple-table"><table><thead><tr>{headers.map((item) => <th key={item}>{item}</th>)}</tr></thead><tbody>{rows.map((row, index) => <tr key={index}>{row.map((cell, cellIndex) => <td key={cellIndex} data-label={headers[cellIndex] || ""}>{cell}</td>)}</tr>)}</tbody></table></div>;
}
```

- [ ] **Step 2: Present shared tables as cards below 760px**

Inside the existing `@media (max-width: 760px)` block, add only shared-table rules so file-manager tables remain unchanged.

```css
.simple-table {
  border: 0;
  overflow: visible;
}
.simple-table table,
.simple-table tbody,
.simple-table tr,
.simple-table td {
  display: block;
  width: 100%;
  min-width: 0;
}
.simple-table thead {
  display: none;
}
.simple-table tbody {
  display: grid;
  gap: 10px;
}
.simple-table tr {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 6px 10px;
  background: var(--panel);
}
.simple-table td {
  display: grid;
  grid-template-columns: minmax(72px, 92px) minmax(0, 1fr);
  gap: 10px;
  padding: 8px 0;
  overflow-wrap: anywhere;
}
.simple-table td::before {
  content: attr(data-label);
  color: var(--muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
}
.simple-table td[data-label=""] {
  display: block;
}
.simple-table td[data-label=""]::before {
  display: none;
}
```

- [ ] **Step 3: Tighten existing non-table lists on mobile**

Use the existing selectors without changing JSX: reduce menu/card padding, keep actions visible, wrap metadata, and preserve 36px minimum tap targets for `.server-switcher-item`, `.target-tree-row`, `.target-folder-row`, `.member-card`, `.resource-row`, `.policy-rule-card`, and `.row-actions`.

```css
.server-switcher-item,
.target-tree-row,
.target-folder-row,
.member-card,
.resource-row,
.policy-rule-card {
  min-width: 0;
}
.row-actions,
.inline-actions {
  flex-wrap: wrap;
}
.row-actions button,
.inline-actions button {
  min-height: 36px;
}
```

- [ ] **Step 4: Remove decorative grid layers**

Replace only repeated grid layers. Keep theme colors, radial accents, and functional trend chart guides.

```css
.client-desktop-content {
  background: var(--bg);
}
.terminal-panel::before {
  display: none;
}
.empty-state {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.82), rgba(229, 249, 252, 0.62));
}
html[data-theme="dark"] .empty-state {
  background: linear-gradient(135deg, rgba(8, 17, 30, 0.58), rgba(10, 47, 59, 0.28));
}
```

Also remove the first two one-pixel grid gradients and fixed square `background-size` values from `.connect-workspace`, `html[data-theme="dark"] .connect-workspace`, `.local-terminal-workspace`, `.terminal-viewport`, and `.manual-review-command`, retaining their existing flat color/non-repeating gradient layers.

Use these complete replacements for the affected declarations:

```css
.connect-workspace {
  background:
    linear-gradient(116deg, transparent 0 16%, var(--signal-line-soft) 16.05% 16.18%, transparent 16.28% 62%, var(--signal-line) 62.05% 62.18%, transparent 62.28%),
    linear-gradient(132deg, transparent 0 18%, color-mix(in oklch, var(--accent) 5%, transparent) 18.2% 18.45%, transparent 18.7% 51%, color-mix(in oklch, var(--accent-2) 4%, transparent) 51.2% 51.45%, transparent 51.7%),
    linear-gradient(180deg, color-mix(in oklch, var(--bg) 94%, black) 0%, var(--bg) 42%, var(--bg-end) 100%),
    var(--bg);
  background-size: 100% 100%, 100% 100%, auto, auto;
}
html[data-theme="dark"] .connect-workspace {
  background:
    linear-gradient(116deg, transparent 0 16%, var(--signal-line-soft) 16.05% 16.18%, transparent 16.28% 62%, var(--signal-line) 62.05% 62.18%, transparent 62.28%),
    linear-gradient(132deg, transparent 0 18%, color-mix(in oklch, var(--accent) 7%, transparent) 18.2% 18.45%, transparent 18.7% 51%, color-mix(in oklch, var(--accent-2) 4%, transparent) 51.2% 51.45%, transparent 51.7%),
    linear-gradient(180deg, color-mix(in oklch, var(--bg) 92%, white) 0%, var(--bg) 38%, var(--bg-end) 100%),
    var(--bg);
  background-size: 100% 100%, 100% 100%, auto, auto;
}
.local-terminal-workspace {
  background: radial-gradient(circle at 12% 0%, rgba(14, 165, 183, 0.18), transparent 28%), #08111e;
  background-size: auto, auto;
}
.terminal-viewport {
  background:
    radial-gradient(circle at 18% 0%, rgba(14, 165, 183, 0.18), transparent 30%),
    linear-gradient(180deg, rgba(7, 16, 30, 0.96), rgba(4, 10, 20, 0.98)),
    #08111e;
  background-size: auto, auto, auto;
}
.manual-review-command {
  background: rgba(2, 6, 12, 0.5);
}
```

- [ ] **Step 5: Run focused checks**

Run: `pnpm check`

Expected: PASS.

Run the 390px browser test again.

Expected: PASS.

### Task 3: Regression Validation And Commit

**Files:**
- Verify only; no additional product files

- [ ] **Step 1: Run the 315px mobile browser test**

```powershell
$env:GOSSHD_UI_E2E_VIEWPORT_WIDTH='315'
go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1 -v
```

Expected: PASS with no horizontal page overflow.

- [ ] **Step 2: Run desktop browser and production build checks**

Run: `go test ./internal/server -run TestUIE2EWithBrowser -count=1 -v`

Expected: PASS and desktop shared tables retain `display: table`.

Run: `pnpm build`

Expected: PASS.

- [ ] **Step 3: Check and commit the final diff**

```powershell
git diff --check
git add web/src/components/ui.tsx web/styles.css web/e2e/ui_e2e.mjs docs/superpowers/plans/2026-07-14-mobile-lists-flat-background.md
git commit -m "feat: improve mobile list layouts"
```

Expected: clean diff check and one implementation commit.
