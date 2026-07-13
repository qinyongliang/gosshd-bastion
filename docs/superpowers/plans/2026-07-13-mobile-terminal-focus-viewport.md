# Mobile Terminal Focus And Viewport Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hide mobile terminal shortcuts unless xterm input is focused and keep the latest terminal line above the software keyboard.

**Architecture:** Use xterm's existing hidden textarea as the only focus source. Mirror `visualViewport.height` into the connect workspace, then reuse the existing resize observer and terminal fit path; a mobile visual viewport resize finishes by scrolling xterm to the bottom.

**Tech Stack:** React 19, TypeScript, xterm.js, CSS, Playwright E2E, Go test wrapper

---

### Task 1: Reproduce Mobile Focus And Viewport Regressions

**Files:**
- Modify: `web/e2e/ui_e2e.mjs:70-182`

- [ ] **Step 1: Add failing focus assertions**

After focusing `.xterm-helper-textarea`, assert the key bar is visible; blur it and assert the bar is hidden. Toggle fullscreen and assert mobile tab activation did not focus the textarea again.

```js
const terminalInput = page.locator(".xterm-helper-textarea");
await terminalInput.focus();
await expectVisible(page.locator(".mobile-terminal-keys"));
await terminalInput.blur();
await expectHidden(page.locator(".mobile-terminal-keys"));
```

- [ ] **Step 2: Add failing visual viewport assertion**

Focus the terminal input, reduce the viewport height, wait for `--connect-viewport-height`, and assert the terminal panel and its viewport remain above the visible bottom.

```js
await terminalInput.focus();
await page.setViewportSize({ width: mobileViewportWidth, height: 520 });
await page.waitForFunction(() => {
  const height = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--connect-viewport-height"));
  return Math.abs(height - (window.visualViewport?.height || innerHeight)) < 1;
});
```

- [ ] **Step 3: Run the mobile E2E and verify failure**

Run: `GOSSHD_UI_E2E_VIEWPORT_WIDTH=390 go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1 -v`

Expected: FAIL because blur does not clear `mobileInputFocused` reliably or because `--connect-viewport-height` is unset.

### Task 2: Use Real Focus And Visual Viewport Height

**Files:**
- Modify: `web/src/pages/ConnectPage.tsx:178-530`
- Modify: `web/src/pages/ConnectPage.tsx:1311-1684`
- Modify: `web/styles.css:4654-4661`
- Modify: `web/styles.css:7191-7202`

- [ ] **Step 1: Mirror the visual viewport height**

In `ConnectWorkspace`, register one `visualViewport.resize` listener and maintain a CSS custom property. Remove it during cleanup.

```tsx
useEffect(() => {
  const viewport = window.visualViewport;
  if (!viewport) return;
  const sync = () => document.documentElement.style.setProperty("--connect-viewport-height", `${viewport.height}px`);
  sync();
  viewport.addEventListener("resize", sync);
  return () => {
    viewport.removeEventListener("resize", sync);
    document.documentElement.style.removeProperty("--connect-viewport-height");
  };
}, []);
```

At the mobile breakpoint, use the property as the workspace height:

```css
.connect-workspace {
  height: var(--connect-viewport-height, 100dvh);
}
```

- [ ] **Step 2: Remove duplicated React focus state**

Delete `mobileInputFocused`, its pointer/focusout mutations, and the `mobile-input-focused` class. Let the terminal viewport's actual focus state control the sibling key bar.

```css
.terminal-viewport:focus-within ~ .mobile-terminal-keys {
  display: grid;
}
```

- [ ] **Step 3: Prevent mobile lifecycle autofocus**

Guard the remaining active/fullscreen effect with the existing viewport helper.

```tsx
if (active && !isMobileViewport()) terminal.focus();
```

- [ ] **Step 4: Refit and reveal the latest line after keyboard resize**

Register a mobile `visualViewport.resize` listener beside the existing container resize observer. Reuse `scheduleTerminalFit`, then scroll xterm to the bottom on the next animation frame. Remove the listener in the effect cleanup.

```tsx
const onVisualViewportResize = () => {
  if (!mobileMedia.matches) return;
  scheduleTerminalFit(terminal);
  window.requestAnimationFrame(() => terminal.scrollToBottom());
};
window.visualViewport?.addEventListener("resize", onVisualViewportResize);
```

- [ ] **Step 5: Run focused checks**

Run: `pnpm check`

Expected: PASS.

Run: `GOSSHD_UI_E2E_VIEWPORT_WIDTH=390 go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1 -v`

Expected: PASS.

### Task 3: Regression Validation

**Files:**
- Verify only; no additional files

- [ ] **Step 1: Run narrow and full frontend validation**

Run: `GOSSHD_UI_E2E_VIEWPORT_WIDTH=315 go test ./internal/server -run TestMobileConsoleUIE2EWithBrowser -count=1 -v`

Expected: PASS.

Run: `pnpm build`

Expected: PASS.

- [ ] **Step 2: Check the final diff**

Run: `git diff --check`

Expected: no errors.

- [ ] **Step 3: Commit the fix**

```bash
git add web/src/pages/ConnectPage.tsx web/styles.css web/e2e/ui_e2e.mjs docs/superpowers/plans/2026-07-13-mobile-terminal-focus-viewport.md
git commit -m "fix: keep mobile terminal input visible"
```
