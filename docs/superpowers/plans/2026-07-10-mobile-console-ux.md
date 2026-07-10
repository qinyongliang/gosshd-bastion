# Mobile Console UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing `/connect` workspace usable on phones by tightening terminal character spacing, reserving space for terminal actions, adding a two-row terminal key bar, fixing both collapsible regions, and constraining the server switcher to the viewport.

**Architecture:** Keep the existing `ConnectWorkspace` and `TerminalPanel` ownership boundaries. Put terminal escape-sequence and one-shot modifier logic in one dependency-free TypeScript module, render it from `TerminalPanel`, and scope all layout changes to the existing 760px media query. Extend the current browser E2E instead of creating a second UI harness.

**Tech Stack:** React 19, TypeScript 5.9, xterm.js 5.5, CSS media queries, Node 24 built-in TypeScript stripping, Playwright through the existing Go UI E2E harness.

---

## File Map

- Create `web/src/terminalShortcuts.ts`: terminal shortcut rows, labels, escape sequences, and Ctrl/Alt transformation.
- Create `web/e2e/terminal_shortcuts.test.mjs`: dependency-free assertions for terminal control sequences.
- Modify `web/src/pages/ConnectPage.tsx`: mobile key bar, one-shot modifier state, mobile xterm letter spacing, and cell-width measurement.
- Modify `web/styles.css`: mobile-only static terminal toolbar, key bar, consistent collapsible bars, and fixed server switcher geometry.
- Modify `web/e2e/ui_e2e.mjs`: 390px integration assertions for the mobile layout.
- Modify `internal/server/ui_e2e_test.go`: named mobile-console browser E2E entrypoint.

### Task 1: Terminal Shortcut Protocol

**Files:**
- Create: `web/src/terminalShortcuts.ts`
- Create: `web/e2e/terminal_shortcuts.test.mjs`

- [ ] **Step 1: Write the failing protocol test**

Create `web/e2e/terminal_shortcuts.test.mjs`:

```js
import assert from "node:assert/strict";
import {
  MOBILE_TERMINAL_KEY_ROWS,
  applyTerminalModifier,
  terminalShortcutSequence,
} from "../src/terminalShortcuts.ts";

assert.deepEqual(MOBILE_TERMINAL_KEY_ROWS, [
  ["escape", "tab", "ctrl", "alt", "home", "end"],
  ["pageUp", "left", "up", "down", "right", "pageDown"],
]);
assert.equal(terminalShortcutSequence("escape"), "\x1b");
assert.equal(terminalShortcutSequence("tab"), "\t");
assert.equal(terminalShortcutSequence("home"), "\x1b[H");
assert.equal(terminalShortcutSequence("end"), "\x1b[F");
assert.equal(terminalShortcutSequence("pageUp"), "\x1b[5~");
assert.equal(terminalShortcutSequence("left"), "\x1b[D");
assert.equal(terminalShortcutSequence("up"), "\x1b[A");
assert.equal(terminalShortcutSequence("down"), "\x1b[B");
assert.equal(terminalShortcutSequence("right"), "\x1b[C");
assert.equal(terminalShortcutSequence("pageDown"), "\x1b[6~");
assert.equal(terminalShortcutSequence("ctrl"), null);
assert.equal(terminalShortcutSequence("alt"), null);
assert.equal(applyTerminalModifier("c", "ctrl"), "\x03");
assert.equal(applyTerminalModifier("[", "ctrl"), "\x1b");
assert.equal(applyTerminalModifier("x", "alt"), "\x1bx");
assert.equal(applyTerminalModifier("paste", "ctrl"), "paste");
assert.equal(applyTerminalModifier("x", null), "x");

console.log("terminal shortcut checks passed");
```

- [ ] **Step 2: Run the protocol test to verify it fails**

Run:

```powershell
node web/e2e/terminal_shortcuts.test.mjs
```

Expected: FAIL with `ERR_MODULE_NOT_FOUND` for `web/src/terminalShortcuts.ts`.

- [ ] **Step 3: Add the minimal shortcut module**

Create `web/src/terminalShortcuts.ts`:

```ts
export const MOBILE_TERMINAL_KEY_ROWS = [
  ["escape", "tab", "ctrl", "alt", "home", "end"],
  ["pageUp", "left", "up", "down", "right", "pageDown"],
] as const;

export type TerminalShortcutKey = (typeof MOBILE_TERMINAL_KEY_ROWS)[number][number];
export type TerminalModifier = Extract<TerminalShortcutKey, "ctrl" | "alt">;

export const TERMINAL_SHORTCUT_LABELS: Record<TerminalShortcutKey, string> = {
  escape: "Esc",
  tab: "Tab",
  ctrl: "Ctrl",
  alt: "Alt",
  home: "Home",
  end: "End",
  pageUp: "PgUp",
  left: "←",
  up: "↑",
  down: "↓",
  right: "→",
  pageDown: "PgDn",
};

const TERMINAL_SHORTCUT_SEQUENCES: Partial<Record<TerminalShortcutKey, string>> = {
  escape: "\x1b",
  tab: "\t",
  home: "\x1b[H",
  end: "\x1b[F",
  pageUp: "\x1b[5~",
  left: "\x1b[D",
  up: "\x1b[A",
  down: "\x1b[B",
  right: "\x1b[C",
  pageDown: "\x1b[6~",
};

export function terminalShortcutSequence(key: TerminalShortcutKey) {
  return TERMINAL_SHORTCUT_SEQUENCES[key] ?? null;
}

export function applyTerminalModifier(value: string, modifier: TerminalModifier | null) {
  if (modifier === "alt") return `\x1b${value}`;
  if (modifier !== "ctrl" || value.length !== 1) return value;
  const code = value.toUpperCase().charCodeAt(0);
  return code >= 64 && code <= 95 ? String.fromCharCode(code - 64) : value;
}
```

- [ ] **Step 4: Run the protocol and TypeScript checks**

Run:

```powershell
node web/e2e/terminal_shortcuts.test.mjs
pnpm check
```

Expected: `terminal shortcut checks passed`; TypeScript exits 0.

- [ ] **Step 5: Commit the protocol module**

```powershell
git add web/src/terminalShortcuts.ts web/e2e/terminal_shortcuts.test.mjs
git commit -m "feat: define mobile terminal shortcuts"
```

### Task 2: Mobile Terminal And Collapsible Layout

**Files:**
- Modify: `web/e2e/ui_e2e.mjs:175-191`
- Modify: `web/src/pages/ConnectPage.tsx:41-55,1308-1676,2160-2182`
- Modify: `web/styles.css:4797-4813,5388-5492,5838-5840,6147-6251,7008-7102`
- Modify: `internal/server/ui_e2e_test.go`

- [ ] **Step 1: Add failing mobile integration assertions**

In `web/e2e/ui_e2e.mjs`, set the phone viewport before opening `/connect` and assert the new layout before reconnecting:

```js
const mobileViewportWidth = Number(process.env.GOSSHD_UI_E2E_VIEWPORT_WIDTH || 390);

// Inside the main scenario:
  await page.setViewportSize({ width: mobileViewportWidth, height: 844 });
  await page.goto(`${baseURL}/targets/${externalTargetID}/connect`, { waitUntil: "domcontentloaded" });
  await expectCount(page.locator(".mobile-terminal-keys button"), 12);
  await expectCount(page.locator(".connect-host-panel.collapsed .collapsed-zone-button"), 1);
  await expectCount(page.locator(".files-zone.collapsed .collapsed-zone-button"), 1);
  const mobileTerminalLayout = await page.evaluate(() => {
    const toolbar = document.querySelector(".terminal-pane-toolbar");
    const viewport = document.querySelector(".terminal-viewport");
    if (!(toolbar instanceof HTMLElement) || !(viewport instanceof HTMLElement)) return null;
    const toolbarRect = toolbar.getBoundingClientRect();
    const viewportRect = viewport.getBoundingClientRect();
    return {
      toolbarPosition: getComputedStyle(toolbar).position,
      toolbarBottom: toolbarRect.bottom,
      viewportTop: viewportRect.top,
    };
  });
  if (!mobileTerminalLayout) throw new Error("mobile terminal layout not found");
  if (mobileTerminalLayout.toolbarPosition !== "static") throw new Error("mobile terminal toolbar should be in flow");
  if (mobileTerminalLayout.toolbarBottom > mobileTerminalLayout.viewportTop + 1) throw new Error("mobile terminal toolbar overlaps output");
```

After `switcherSearch.waitFor()`, assert viewport bounds:

```js
  const switcherBounds = await page.locator(".server-switcher-menu").evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return { left: rect.left, right: rect.right, width: innerWidth };
  });
  if (switcherBounds.left < 8 || switcherBounds.right > switcherBounds.width - 8) {
    throw new Error(`mobile server switcher escaped viewport: ${JSON.stringify(switcherBounds)}`);
  }
```

- [ ] **Step 2: Build and run UI E2E to verify the new assertions fail**

Run:

```powershell
pnpm build
$env:GOSSHD_UI_E2E_NODE='C:\Users\qyl\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\Users\qyl\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\Program Files\Google\Chrome\Application\chrome.exe'
go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1
```

Expected: FAIL because `.mobile-terminal-keys` does not exist.

- [ ] **Step 3: Render shortcut controls and apply one-shot modifiers**

Import the shortcut module in `web/src/pages/ConnectPage.tsx`:

```ts
import {
  MOBILE_TERMINAL_KEY_ROWS,
  TERMINAL_SHORTCUT_LABELS,
  applyTerminalModifier,
  terminalShortcutSequence,
  type TerminalModifier,
  type TerminalShortcutKey,
} from "../terminalShortcuts";
```

In `TerminalPanel`, add modifier state next to the existing state:

```ts
  const [mobileModifier, setMobileModifier] = useState<TerminalModifier | null>(null);
  const mobileModifierRef = useRef<TerminalModifier | null>(null);
  const connected = status === "connected";
```

Add the shortcut handler after `sendTerminalInput`:

```ts
  const setModifier = (modifier: TerminalModifier | null) => {
    mobileModifierRef.current = modifier;
    setMobileModifier(modifier);
  };

  const pressMobileShortcut = (key: TerminalShortcutKey) => {
    const sequence = terminalShortcutSequence(key);
    if (sequence === null) {
      const modifier = key as TerminalModifier;
      setModifier(mobileModifierRef.current === modifier ? null : modifier);
    } else if (sendTerminalInput(sequence)) {
      setModifier(null);
    }
    runtime.terminal?.focus();
  };
```

Replace the connected `terminal.onData` send with one-shot modifier application:

```ts
        const normalized = normalizeTerminalInputText(value);
        const modifier = mobileModifierRef.current;
        if (modifier) setModifier(null);
        sendTerminalInput(applyTerminalModifier(normalized, modifier));
```

Add the key bar after `.terminal-viewport`:

```tsx
      <div className="mobile-terminal-keys" aria-label="Terminal shortcut keys">
        {MOBILE_TERMINAL_KEY_ROWS.flat().map((key) => (
          <button
            key={key}
            type="button"
            data-terminal-key={key}
            className={mobileModifier === key ? "active" : ""}
            disabled={!connected}
            aria-pressed={key === "ctrl" || key === "alt" ? mobileModifier === key : undefined}
            onPointerDown={(event) => event.preventDefault()}
            onClick={() => pressMobileShortcut(key)}
          >
            {TERMINAL_SHORTCUT_LABELS[key]}
          </button>
        ))}
      </div>
```

- [ ] **Step 4: Tighten mobile xterm cell spacing at the renderer and measurement source**

Add initial xterm spacing in the `new Terminal` options:

```ts
        letterSpacing: window.matchMedia("(max-width: 760px)").matches ? -1 : 0,
```

In the terminal effect, register and clean up a media-query listener:

```ts
    const mobileTerminal = window.matchMedia("(max-width: 760px)");
    const updateLetterSpacing = () => {
      terminal.options.letterSpacing = mobileTerminal.matches ? -1 : 0;
      scheduleTerminalFit(terminal);
    };
    updateLetterSpacing();
    mobileTerminal.addEventListener("change", updateLetterSpacing);
```

Add this cleanup beside the existing listeners:

```ts
      mobileTerminal.removeEventListener("change", updateLetterSpacing);
```

Make `measureTerminalCell` use the same spacing value:

```ts
  probe.style.letterSpacing = `${terminal.options.letterSpacing || 0}px`;
```

- [ ] **Step 5: Add the mobile-only layout styles**

Add the desktop-hidden key bar near the terminal styles:

```css
.mobile-terminal-keys {
  display: none;
}
```

Inside `@media (max-width: 760px)`, add:

```css
  .server-switcher-menu {
    position: fixed;
    inset: 46px 8px 8px;
    width: auto;
    max-height: none;
  }
  .connect-body {
    grid-template-columns: 1fr;
    grid-template-rows: auto minmax(0, 1fr);
    padding: 0;
    gap: 0;
  }
  .connect-body.host-collapsed {
    grid-template-rows: 34px minmax(0, 1fr);
  }
  .connect-host-panel {
    max-height: min(38dvh, 300px);
    padding: 0;
    border-right: 0;
    border-bottom: 1px solid var(--line);
    overflow: auto;
  }
  .connect-host-panel.collapsed {
    display: block;
    width: 100%;
    height: 34px;
  }
  .connect-panel.compact {
    padding: 0;
    border: 0;
    border-radius: 0;
    box-shadow: none;
  }
  .connect-panel-title,
  .connect-zone-head {
    min-height: 34px;
    margin: 0;
    padding: 0 8px;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
  }
  .connect-host-list {
    padding: 8px;
  }
  .collapsed-zone-button {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-height: 34px;
    border-radius: 0;
    padding: 0 10px;
  }
  .collapsed-zone-button span {
    order: -1;
    writing-mode: horizontal-tb;
    font-size: 11px;
    letter-spacing: 0;
  }
  .connect-host-panel.collapsed .collapsed-zone-button svg {
    transform: rotate(90deg);
  }
  .files-zone.collapsed .collapsed-zone-button svg {
    transform: rotate(-90deg);
  }
  .connect-main {
    grid-template-columns: 1fr;
    grid-template-rows: minmax(280px, 1fr) minmax(220px, 0.65fr);
  }
  .connect-main.files-collapsed {
    grid-template-columns: 1fr;
    grid-template-rows: minmax(0, 1fr) 34px;
  }
  .connect-zone {
    padding: 0;
  }
  .terminal-panel {
    grid-template-rows: 34px minmax(0, 1fr) auto;
    min-height: 280px;
  }
  .terminal-pane-toolbar {
    position: static;
    justify-content: flex-end;
    gap: 4px;
    min-height: 34px;
    padding: 3px 6px;
    border-bottom: 1px solid rgba(103, 232, 249, 0.16);
    background: #101d2c;
    opacity: 1;
    pointer-events: auto;
    transform: none;
  }
  .terminal-pane-toolbar .icon-button {
    width: 28px;
    min-width: 28px;
    min-height: 28px;
  }
  .terminal-reconnect-button {
    top: 2px;
    right: auto;
    left: 6px;
    min-height: 30px;
  }
  .mobile-terminal-keys {
    display: grid;
    grid-template-columns: repeat(6, minmax(0, 1fr));
    gap: 4px;
    padding: 5px 6px;
    border-top: 1px solid rgba(103, 232, 249, 0.2);
    background: #101d2c;
  }
  .mobile-terminal-keys button {
    min-width: 0;
    min-height: 34px;
    border: 1px solid rgba(148, 163, 184, 0.24);
    border-radius: 5px;
    padding: 0 2px;
    color: #e2e8f0;
    background: #203247;
    font-size: 10px;
    font-weight: 800;
  }
  .mobile-terminal-keys button.active {
    color: #031017;
    border-color: #67e8f9;
    background: #67e8f9;
  }
  .mobile-terminal-keys button:disabled {
    opacity: 0.45;
  }
```

- [ ] **Step 6: Run focused checks and UI E2E**

Run:

```powershell
node web/e2e/terminal_shortcuts.test.mjs
pnpm check
pnpm build
$env:GOSSHD_UI_E2E_NODE='C:\Users\qyl\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\Users\qyl\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\Program Files\Google\Chrome\Application\chrome.exe'
go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1
```

Expected: all commands exit 0; the Node check prints `terminal shortcut checks passed`; Go prints `ok`.

- [ ] **Step 7: Commit the mobile UI**

```powershell
git add web/src/pages/ConnectPage.tsx web/styles.css web/e2e/ui_e2e.mjs internal/server/ui_e2e_test.go
git commit -m "feat: improve mobile web terminal controls"
```

### Task 3: Regression Verification

**Files:**
- Verify only; no planned source changes.

- [ ] **Step 1: Run repository regression checks**

Run:

```powershell
git diff --check HEAD~2
go test ./internal/server
pnpm check
pnpm build
```

Expected: all commands exit 0.

- [ ] **Step 2: Verify mobile geometry at both required widths**

Run the existing UI E2E through the checked-in assertions at both viewport widths:

```powershell
$env:GOSSHD_UI_E2E_NODE='C:\Users\qyl\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\Users\qyl\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\Program Files\Google\Chrome\Application\chrome.exe'
$env:GOSSHD_UI_E2E_VIEWPORT_WIDTH='390'
go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1
$env:GOSSHD_UI_E2E_VIEWPORT_WIDTH='315'
go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1
Remove-Item Env:GOSSHD_UI_E2E_VIEWPORT_WIDTH
```

Expected at both widths:

```text
12 mobile terminal keys
toolbar position = static
toolbar bottom <= terminal viewport top
one collapsed host bar
one collapsed file-manager bar
server switcher left >= 8px
server switcher right <= viewport width - 8px
```

- [ ] **Step 3: Confirm the final diff is scoped**

Run:

```powershell
git status --short
git diff --stat HEAD~2
```

Expected: only the six files in the File Map plus this plan/spec history are changed by the feature; no generated `.superpowers` content is tracked.
