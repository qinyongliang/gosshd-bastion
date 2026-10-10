import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const target = { id: "layout-target", owner_type: "organization", owner_id: "layout-org", target_type: "direct", name: "Layout preview", alias: "layout-preview", host: "127.0.0.1", port: 22, remote_username: "root", auth_type: "password", tags: [] };
const second = { ...target, id: "layout-second", name: "Second connection", alias: "layout-second" };

try {
  const context = await browser.newContext({ viewport: { width: 1800, height: 1000 }, locale: "zh-CN", reducedMotion: "reduce" });
  await context.addInitScript(() => {
    localStorage.setItem("gosshd_locale", "zh-CN");
    localStorage.setItem("gosshd_theme", "light");
    window.__terminalSocketURLs = [];
    const OriginalSocket = window.WebSocket;
    window.WebSocket = class extends EventTarget {
      static OPEN = 1;
      static CLOSED = 3;
      readyState = 0;
      constructor(url, protocols) {
        super();
        if (!String(url).includes("/api/targets/")) return new OriginalSocket(url, protocols);
        window.__terminalSocketURLs.push(String(url));
        setTimeout(() => {
          this.readyState = 1;
          this.onopen?.(new Event("open"));
          this.onmessage?.({ data: JSON.stringify({ type: "output", data: "root@layout-preview:~# " }) });
        }, 10);
      }
      send() {}
      close() { this.readyState = 3; this.onclose?.(new Event("close")); }
    };
  });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const errors = [];
  let systemRequests = 0;
  page.on("pageerror", error => { errors.push(error.message); console.log("pageerror:", error.stack); });
  await page.route("**/api/**", async route => {
    const url = new URL(route.request().url());
    let json = {};
    if (url.pathname === "/api/me") json = { user: { id: "layout-user", email: "test@layout", display_name: "Layout", is_system_admin: false, auth_provider: "local" }, organizations: [{ id: "layout-org", name: "Layout", slug: "layout", is_personal: false, role: "member" }], runtime: { client_mode: true, app_name: "SSH Workspace", app_description: "布局预览", ssh_port: 22 } };
    else if (url.pathname === "/api/targets") json = { targets: [target, second] };
    else if (url.pathname.endsWith("/system")) {
      systemRequests++;
      json = { os: "linux", hostname: "layout-preview", cpu_percent: 12, memory: { used_bytes: 1073741824, total_bytes: 4294967296, percent: 25 }, network: [], filesystems: [] };
    }
    else if (url.pathname.endsWith("/files")) {
      const path = url.searchParams.get("path");
      json = { path: path === "." ? "/tmp" : path, entries: [{ name: "app.conf", path: "/tmp/app.conf", type: "file", size: 1024, mode: "-rw-r--r--", modified_at: "2026-10-10T00:00:00Z" }, { name: "logs", path: "/tmp/logs", type: "dir", size: 0, mode: "drwxr-xr-x" }] };
    } else if (url.pathname.endsWith("/files/read")) json = { path: "/tmp/app.conf", content: "server.port=8080\nserver.host=localhost\n" };
    else if (url.pathname.endsWith("/settings")) json = { connect_open_mode: "popup", connect_attach_existing: false };
    await route.fulfill({ json });
  });
  await page.goto(`${baseURL}/targets/${target.id}/connect`);
  const workspace = () => page.locator(".terminal-tab-layer.active .dock-workspace");
  const view = type => workspace().locator(`.dock-view-${type}`).first();
  await view("files").locator(".file-name").first().waitFor();
  await page.waitForFunction(() => window.__terminalSocketURLs.length === 1);
  await view("host").locator(".resource-meter").first().waitFor();
  assert.equal(await view("host").locator(".connect-zone-head").count(), 1, "Host metadata and metrics should share a single panel header");
  assert.equal(await view("host").locator(".connect-panel, .telemetry-head").count(), 0, "System metrics should be embedded without a second card");
  assert.equal(await view("host").locator(".connect-host-body .connect-host-list").count(), 1);
  const requestsBeforeRefresh = systemRequests;
  await Promise.all([
    page.waitForResponse(response => new URL(response.url()).pathname.endsWith("/system")),
    view("host").getByRole("button", { name: "刷新", exact: true }).click(),
  ]);
  assert.ok(systemRequests > requestsBeforeRefresh, "The merged header should still refresh system information");
  await page.screenshot({ path: "build/workspace-default.png" });

  // Dock files across the whole bottom edge.
  await drag(view("files"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width / 2, y: r.y + r.height - 10 }; });
  let root = await workspace().boundingBox(), files = await view("files").boundingBox();
  assert.ok(Math.abs(files.x - root.x) < 1 && Math.abs(files.width - root.width) < 1, "File manager must span the entire bottom");
  assert.equal(await page.evaluate(() => window.__terminalSocketURLs.length), 1, "Docking must preserve the shell connection");
  assert.equal(await view("files").locator(".dock-panel-toggle .lucide-chevron-down").count(), 1);
  await view("files").locator(".dock-panel-toggle").click();
  assert.equal(await view("files").locator(".dock-panel-toggle .lucide-chevron-up").count(), 1, "Bottom panels should expand upward");
  await view("files").locator(".dock-panel-toggle").click();

  // Open an editor, then make it dirty and move it twice without remounting it.
  await view("files").locator(".file-name").filter({ hasText: "app.conf" }).click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "编辑", exact: true }).click();
  await view("editor").locator(".monaco-editor textarea").waitFor();
  await view("editor").locator(".monaco-editor .view-lines").click({ position: { x: 100, y: 10 } });
  await page.keyboard.press("Control+End");
  await page.keyboard.type("# unsaved-layout-note");
  await page.waitForFunction(() => document.querySelector(".terminal-tab-layer.active .editor-pane-head").textContent.includes("* "));
  await view("editor").evaluate(el => window.__originalEditor = el.querySelector(".monaco-editor"));
  await drag(view("editor"), async () => { const r = await view("terminal").boundingBox(); return { x: r.x + r.width / 2, y: r.y + 45 }; });
  await page.screenshot({ path: "build/workspace-editor-first-move.png" });
  await drag(view("editor"), async () => { const r = await view("terminal").boundingBox(); return { x: r.x + r.width - 35, y: r.y + r.height / 2 }; });
  const editorBounds = await view("editor").boundingBox(), shellBounds = await view("terminal").boundingBox();
  assert.ok(editorBounds.x > shellBounds.x && Math.abs(editorBounds.y - shellBounds.y) < 1, "Editor should be to the right of the terminal");
  assert.ok(await view("editor").evaluate(el => window.__originalEditor === el.querySelector(".monaco-editor")), "Moving an editor must preserve its DOM and buffer");
  assert.ok((await view("editor").locator(".editor-pane-head").textContent()).includes("* "), "Unsaved state must survive docking");

  // Resize the bottom region with the same divider in its new orientation.
  const columnDivider = workspace().locator(".dock-splitter.column").last();
  const oldHeight = (await view("files").boundingBox()).height;
  const divider = await columnDivider.boundingBox();
  await page.mouse.move(divider.x + divider.width / 2, divider.y + divider.height / 2);
  await page.mouse.down();
  await page.mouse.move(divider.x + divider.width / 2, divider.y - 65, { steps: 10 });
  await page.mouse.up();
  assert.ok((await view("files").boundingBox()).height > oldHeight + 40);

  // Cancellation leaves the tree unchanged; host view can occupy a full edge too.
  const beforeCancel = await page.evaluate(() => localStorage.getItem("gosshd-workspace-layout:v1:layout-user:layout-org:desktop:target:layout-target"));
  await drag(view("host"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width - 10, y: r.y + r.height / 2 }; }, true);
  assert.equal(await page.evaluate(() => localStorage.getItem("gosshd-workspace-layout:v1:layout-user:layout-org:desktop:target:layout-target")), beforeCancel);
  assert.equal(await page.locator(".dock-drop-preview").count(), 0);
  await page.screenshot({ path: "build/workspace-files-bottom-editor-right.png" });
  await page.locator(".connect-appbar-actions").getByRole("button", { name: "黑", exact: true }).click();
  await page.waitForFunction(() => document.documentElement.dataset.theme === "dark");
  await page.screenshot({ path: "build/workspace-files-bottom-editor-right-dark.png" });
  await page.locator(".connect-appbar-actions").getByRole("button", { name: "白", exact: true }).click();

  // Switching tabs retains the existing editors and each target's layout.
  await page.evaluate(id => window.postMessage({ type: "gosshd-connect-open-target", targetID: id }, location.origin), second.id);
  await page.waitForFunction(id => location.pathname === `/targets/${id}/connect`, second.id);
  await page.waitForFunction(alias => document.querySelector(".connection-tab.active")?.textContent.includes(alias), second.alias);
  await view("files").locator(".file-name").filter({ hasText: "app.conf" }).click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "编辑", exact: true }).click();
  await view("editor").locator(".monaco-editor textarea").waitFor();
  assert.ok(!(await view("editor").locator(".editor-pane-head").textContent()).includes("* "), "Same file path on another target must have an independent buffer");
  await page.locator(".connection-tab").filter({ hasText: target.alias }).locator(".connection-tab-main").click();
  assert.ok(await view("editor").evaluate(el => window.__originalEditor === el.querySelector(".monaco-editor")));
  assert.ok((await view("editor").locator(".editor-pane-head").textContent()).includes("* "));

  await page.evaluate(id => window.postMessage({ type: "gosshd-connect-open-target", targetID: id }, location.origin), target.id);
  await page.waitForFunction(() => document.querySelectorAll(".connection-tab").length === 3);
  assert.equal(await view("editor").count(), 0, "A new connection to the same address should use positions without reopening its files");
  assert.equal(await workspace().locator(".dock-view-terminal").count(), 1);
  await page.locator(".connection-tab").filter({ hasText: target.alias }).first().locator(".connection-tab-main").click();
  assert.ok(await view("editor").evaluate(el => window.__originalEditor === el.querySelector(".monaco-editor")), "Opening a fresh connection should preserve the original live tab");

  // Browser reload restores component positions, without reopening temporary editors.
  const saved = JSON.parse(await page.evaluate(() => localStorage.getItem("gosshd-workspace-layout:v1:layout-user:layout-org:desktop:target:layout-target")));
  const savedFiles = await view("files").boundingBox();
  await page.reload();
  await view("terminal").waitFor();
  await view("files").locator(".file-name").first().waitFor();
  assert.equal(await view("editor").count(), 0, "New connections must not reopen temporary editors");
  assert.equal(saved.version, 2);
  assert.equal(saved.filePath, undefined);
  files = await view("files").boundingBox();
  assert.ok(Math.abs(files.y - savedFiles.y) < 2 && Math.abs(files.height - savedFiles.height) < 2, "Reload should restore divider proportions");
  await view("files").locator(".file-name").filter({ hasText: "app.conf" }).click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "编辑", exact: true }).click();
  await view("editor").locator(".monaco-editor textarea").waitFor();

  // Relocate host to the whole right edge and verify a single active shell handles fullscreen.
  await drag(view("host"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width / 2, y: r.y + 10 }; });
  const metadataBounds = await view("host").locator(".connect-host-list").boundingBox();
  const systemBounds = await view("host").locator(".host-system-content").boundingBox();
  assert.ok(systemBounds.x > metadataBounds.x, "A wide host panel should place metadata and system metrics side by side");
  assert.equal(await view("host").locator(".dock-panel-toggle .lucide-chevron-up").count(), 1);
  await page.screenshot({ path: "build/workspace-host-wide.png" });
  await drag(view("host"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width - 10, y: r.y + r.height / 2 }; });
  const host = await view("host").boundingBox();
  root = await workspace().boundingBox();
  assert.ok(Math.abs(host.y - root.y) < 1 && Math.abs(host.height - root.height) < 1);
  assert.equal(await view("host").locator(".dock-panel-toggle .lucide-chevron-right").count(), 1);
  await view("terminal").locator(".terminal-viewport").click();
  await page.keyboard.press("F11");
  await workspace().filter({ has: page.locator(".terminal-panel.fullscreen") }).waitFor();
  await page.screenshot({ path: "build/workspace-terminal-fullscreen.png" });
  const fullscreenPanel = page.locator(".terminal-panel.fullscreen");
  const fullscreenToolbar = await fullscreenPanel.locator(".terminal-pane-toolbar").boundingBox();
  const fullscreenViewport = await fullscreenPanel.locator(".terminal-viewport").boundingBox();
  const fullscreenBounds = await fullscreenPanel.boundingBox();
  assert.ok(fullscreenToolbar.height <= 48, `Fullscreen toolbar should be compact, got ${fullscreenToolbar.height}px`);
  assert.ok(fullscreenViewport.height >= fullscreenBounds.height - 52, "Fullscreen terminal should occupy the remaining panel height");
  await page.keyboard.press("F11");
  assert.equal(await page.locator(".terminal-panel.fullscreen").count(), 0);

  await drag(view("host"), async () => { const r = await view("terminal").boundingBox(); return { x: r.x + r.width - 35, y: r.y + r.height / 2 }; });
  assert.equal(await view("host").locator(".dock-panel-toggle .lucide-panel-top-close").count(), 1, "Interior panels should use a fold icon");
  await view("host").locator(".dock-panel-toggle").click();
  assert.equal(await view("host").locator(".dock-panel-toggle .lucide-panel-top-open").count(), 1);
  await view("host").locator(".dock-panel-toggle").click();

  // Reset preserves content panes but returns the tool panels to their usual edges.
  await page.getByRole("button", { name: "恢复默认面板布局", exact: true }).click();
  root = await workspace().boundingBox();
  assert.ok(Math.abs((await view("host").boundingBox()).x - root.x) < 1);
  assert.ok(Math.abs((await view("files").boundingBox()).x + (await view("files").boundingBox()).width - root.x - root.width) < 1);
  assert.equal(await view("editor").count(), 1);
  // Terminals can move too; splitting adds a fresh shell without replacing existing views.
  const shellCount = await page.evaluate(() => window.__terminalSocketURLs.length);
  await view("terminal").getByRole("button", { name: "向右切分", exact: true }).click();
  await page.waitForFunction(count => window.__terminalSocketURLs.length === count + 1, shellCount);
  assert.equal(await workspace().locator(".dock-view-terminal").count(), 2);
  await drag(view("terminal"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width / 2, y: r.y + 10 }; });
  const movedShell = await view("terminal").boundingBox();
  assert.ok(Math.abs(movedShell.width - (await workspace().boundingBox()).width) < 1);
  assert.equal(await page.evaluate(() => window.__terminalSocketURLs.length), shellCount + 1);
  await view("terminal").locator(".terminal-viewport").click();
  await page.keyboard.press("F11");
  assert.equal(await page.locator(".terminal-panel.fullscreen").count(), 1, "Only the active shell should toggle fullscreen");
  await page.keyboard.press("F11");
  assert.equal(await page.locator(".terminal-panel.fullscreen").count(), 0);
  assert.deepEqual(errors, []);

  // Close the final shell, then the temporary editor; reconnect with the remembered shell slot.
  await page.reload();
  await view("terminal").waitFor();
  assert.equal(await workspace().locator(".dock-view-terminal").count(), 1, "New connections do not recreate extra shells");
  await view("files").locator(".file-name").filter({ hasText: "app.conf" }).click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "编辑", exact: true }).click();
  await view("editor").locator(".monaco-editor textarea").waitFor();
  await drag(view("terminal"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width / 2, y: r.y + r.height - 10 }; });
  const rememberedShell = await view("terminal").boundingBox();
  await view("terminal").locator(".terminal-viewport").click();
  await page.keyboard.press("Control+w");
  await page.waitForFunction(() => document.querySelectorAll(".terminal-tab-layer.active .dock-view-terminal").length === 0);
  assert.equal(await view("editor").count(), 1);
  await view("editor").locator(".editor-pane-actions button").filter({ has: page.locator("svg.lucide-x") }).click();
  await page.waitForFunction(() => location.pathname === "/connect");
  await page.evaluate(id => window.postMessage({ type: "gosshd-connect-open-target", targetID: id }, location.origin), target.id);
  await view("terminal").waitFor();
  assert.equal(await view("editor").count(), 0);
  const reconnectedShell = await view("terminal").boundingBox();
  assert.ok(Math.abs(reconnectedShell.y - rememberedShell.y) < 2 && Math.abs(reconnectedShell.height - rememberedShell.height) < 2, "Closing the last shell must preserve its bottom position for the next connection");

  // Mobile uses a separate persisted arrangement, and pointer dragging still works.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await view("terminal").waitFor();
  assert.ok((await page.locator(".connect-appbar").boundingBox()).height <= 56, "Mobile header should use a single compact row");
  assert.equal(await page.locator(".connect-appbar-title").isVisible(), false);
  assert.equal(await page.locator(".connect-appbar-meta").isVisible(), false);
  assert.equal(await page.locator(".connect-appbar-host code").isVisible(), false);
  assert.equal(await page.locator(".connect-appbar-host strong").textContent(), target.name);
  const switcher = page.locator(".connect-server-switcher");
  await switcher.click();
  await page.locator(".machine-picker-menu").getByRole("menuitem").filter({ hasText: second.name }).waitFor();
  await page.keyboard.press("Escape");
  assert.equal(await switcher.getAttribute("aria-expanded"), "false");
  assert.equal(await view("host").locator(".collapsed-zone-button span").isVisible(), true, "Collapsed horizontal panels should retain their labels");
  const expandFiles = view("files").locator(".collapsed-zone-button");
  if (await expandFiles.count()) await expandFiles.click();
  await drag(view("files"), async () => { const r = await workspace().boundingBox(); return { x: r.x + r.width / 2, y: r.y + r.height - 10 }; });
  const overflow = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
  assert.ok(overflow.scroll <= overflow.width + 1);
  await page.screenshot({ path: "build/workspace-mobile.png" });
  await view("terminal").getByRole("button", { name: "全屏", exact: true }).click();
  const mobileFullscreen = page.locator(".terminal-panel.fullscreen");
  await mobileFullscreen.waitFor();
  assert.ok((await mobileFullscreen.locator(".terminal-pane-toolbar").boundingBox()).height <= 40, "Mobile fullscreen should retain a compact toolbar");
  assert.ok((await mobileFullscreen.locator(".terminal-viewport").boundingBox()).height >= (await mobileFullscreen.boundingBox()).height - 100);
  await page.screenshot({ path: "build/workspace-terminal-fullscreen-mobile.png" });
  await mobileFullscreen.getByRole("button", { name: "退出全屏", exact: true }).click();
  assert.deepEqual(errors, []);
  console.log("PASS: docking, buffers, sizing, fresh connection templates, closed terminal positions, host panel, fullscreen, collapse icons and mobile.");
  await context.close();

  async function drag(source, destination, cancel = false) {
    const handle = source.locator(".dock-drag-handle").first();
    const h = await handle.boundingBox();
    const point = await destination();
    await page.mouse.move(h.x + Math.min(30, h.width / 2), h.y + h.height / 2);
    await page.mouse.down();
    await page.mouse.move(point.x, point.y, { steps: 12 });
    await page.locator(".dock-drop-preview").waitFor();
    if (cancel) await page.keyboard.press("Escape");
    await page.mouse.up();
  }
} finally {
  await browser.close();
}
