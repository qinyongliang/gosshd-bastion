import { createRequire } from "node:module";

const mobileOnly = process.env.GOSSHD_UI_E2E_MOBILE_ONLY === "1";
const mobileViewportWidth = Number(process.env.GOSSHD_UI_E2E_VIEWPORT_WIDTH || 390);
const baseURL = mustEnv("GOSSHD_UI_E2E_BASE_URL");
const playwrightPath = mustEnv("PLAYWRIGHT_REQUIRE_PATH");
const browserExecutable = mustEnv("PLAYWRIGHT_CHROMIUM_EXECUTABLE");

const require = createRequire(import.meta.url);
const { chromium } = require(playwrightPath);

const browser = await chromium.launch({ executablePath: browserExecutable, headless: true });
let context;

try {
  await verifyChineseAuth();
  context = await browser.newContext({ locale: "en-US" });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  page.on("pageerror", (error) => console.error(`browser page error: ${error.stack || error.message}`));
  page.on("console", (message) => {
    if (message.type() === "error") console.error(`browser console: ${message.text()}`);
  });

  await assertStatus(page, "/app.js", 404);
  await assertStatus(page, "/unknown-route", 404);

  await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await page.getByRole("tab", { name: "Login" }).waitFor();
  await page.getByRole("button", { name: /Dark|黑/ }).click();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_theme"))) !== "dark") throw new Error("dark theme was not persisted");
  await page.getByRole("button", { name: /Light|白/ }).click();
  await page.getByRole("button", { name: "中文" }).click();
  await page.getByRole("tab", { name: "登录" }).waitFor();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_locale"))) !== "zh-CN") throw new Error("locale was not persisted");
  await page.getByRole("button", { name: "EN" }).click();
  await page.getByRole("tab", { name: "Login" }).waitFor();

  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in" }).click();

  if (mobileOnly) {
    await expectText(page, /Access path|访问链路/);
    const mobileAlias = `mobile-${Date.now()}`;
    const mobileTargetID = await page.evaluate(async (alias) => {
      const me = await fetch("/api/me").then((response) => response.json());
      const ownerID = localStorage.getItem("gosshd_active_org") || me.organizations[0].id;
      const response = await fetch("/api/targets", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          owner_type: "organization",
          owner_id: ownerID,
          target_type: "direct",
          name: "Mobile E2E service",
          alias,
          host: "127.0.0.1",
          port: 22,
          remote_username: "root",
          auth_type: "password",
          secret: "root-pass",
          tags: ["mobile"],
        }),
      });
      if (!response.ok) throw new Error(await response.text());
      return (await response.json()).target.id;
    }, mobileAlias);

    await page.setViewportSize({ width: mobileViewportWidth, height: 844 });
    await page.goto(`${baseURL}/targets/${mobileTargetID}/connect`, { waitUntil: "domcontentloaded" });
    await page.locator(".mobile-terminal-keys").waitFor();
    await expectCount(page.locator(".mobile-terminal-keys button"), 12);
    await expectCount(page.locator(".connect-host-panel > .collapsed-zone-button"), 1);
    await expectCount(page.locator(".files-zone > .collapsed-zone-button"), 1);
    const terminalLayout = await page.evaluate(() => {
      const toolbar = document.querySelector(".terminal-pane-toolbar");
      const viewport = document.querySelector(".terminal-viewport");
      if (!(toolbar instanceof HTMLElement) || !(viewport instanceof HTMLElement)) throw new Error("terminal layout is missing");
      return {
        toolbarPosition: getComputedStyle(toolbar).position,
        toolbarBottom: toolbar.getBoundingClientRect().bottom,
        viewportTop: viewport.getBoundingClientRect().top,
      };
    });
    if (terminalLayout.toolbarPosition !== "static") throw new Error(`mobile terminal toolbar should be static, got ${terminalLayout.toolbarPosition}`);
    if (terminalLayout.toolbarBottom > terminalLayout.viewportTop + 1) throw new Error(`mobile terminal toolbar overlaps viewport: ${terminalLayout.toolbarBottom} > ${terminalLayout.viewportTop}`);

    await page.keyboard.press("Alt+N");
    const switcherSearch = page.locator("[data-connect-switcher-search]");
    await switcherSearch.waitFor();
    const switcherBounds = await page.locator(".server-switcher-menu").evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      return { left: bounds.left, right: bounds.right, innerWidth };
    });
    if (switcherBounds.left < 8 || switcherBounds.right > switcherBounds.innerWidth - 8) {
      throw new Error(`mobile server switcher is outside viewport: ${JSON.stringify(switcherBounds)}`);
    }
    await switcherSearch.fill(mobileAlias);
    await page.keyboard.press("Enter");
    await page.locator(".server-switcher-menu").waitFor({ state: "detached" });
  } else {
  await expectText(page, "SSH ingress online");
  await expectText(page, /Access path|访问链路/);
  await expectText(page, /Target service|目标服务/);

  await page.getByRole("link", { name: /Public keys/ }).click();
  await page.getByRole("button", { name: "Add public key" }).click();
  await page.getByLabel("Title").fill("React laptop");
  await page.getByRole("textbox", { name: "Public key" }).fill("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUTThKwa4NlLwH7sntZnYosUoFkNceEce0kEwbE9nNm react-key");
  await page.getByRole("button", { name: "Add public key" }).last().click();
  await expectText(page, "React laptop");

  await page.getByRole("link", { name: /Organizations/ }).click();
  await page.getByRole("button", { name: "Create organization" }).click();
  await page.getByLabel("Organization name").fill("React Ops");
  await page.getByLabel("Organization slug").fill(`react-ops-${Date.now()}`);
  await page.getByRole("button", { name: "Create organization" }).last().click();
  await page.locator("tbody").getByText("React Ops").waitFor();

  await page.getByRole("link", { name: /SSH services/ }).click();
  await page.getByRole("button", { name: "Add service" }).click();
  await page.getByLabel("Service name").fill("React Service");
  await page.getByLabel("Target alias").fill("react-test2");
  await page.getByLabel("Tags").fill("test, common");
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByLabel("Target host").fill("127.0.0.1");
  await page.getByLabel("Target port").fill("22");
  await page.getByLabel("Remote username").fill("root");
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByLabel("Key or password").fill("root-pass");
  await page.getByRole("button", { name: "Add service" }).last().click();
  await expectText(page, "react-test2");
  const copyCommand = await page.locator("tr").filter({ hasText: "react-test2" }).locator("button.copy-anchor").getAttribute("data-value");
  if (!copyCommand?.includes("-p 22022")) throw new Error(`copy command should use public ssh port 22022, got ${copyCommand}`);
  await page.getByRole("button", { name: /Copy SSH command/ }).first().click();
  await expectText(page, "Copied");

  await page.getByRole("button", { name: "Add service" }).click();
  await page.getByRole("tab", { name: /Private node|私有节点/ }).click();
  await page.getByLabel("Target alias").fill("react-private");
  await page.getByRole("button", { name: "Create install token" }).click();
  await expectText(page, "systemctl");
  await expectText(page, "sc.exe");
  await page.getByRole("button", { name: "Close" }).click();

  const externalAlias = `refresh-${Date.now()}`;
  await page.getByRole("link", { name: /^Audit$/ }).click();
  const externalTargetID = await page.evaluate(async (alias) => {
    const me = await fetch("/api/me").then((response) => response.json());
    const ownerID = localStorage.getItem("gosshd_active_org") || me.organizations[0].id;
    const response = await fetch("/api/targets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        owner_type: "organization",
        owner_id: ownerID,
        target_type: "direct",
        name: "Externally created service",
        alias,
        host: "127.0.0.1",
        port: 22,
        remote_username: "root",
        auth_type: "password",
        secret: "root-pass",
        tags: ["refresh"],
      }),
    });
    if (!response.ok) throw new Error(await response.text());
    return (await response.json()).target.id;
  }, externalAlias);
  await page.getByRole("link", { name: /SSH services/ }).click();
  await expectText(page, externalAlias);

  const privateAlias = `private-edit-${Date.now()}`;
  const privateTargetID = await page.evaluate(async (alias) => {
    const me = await fetch("/api/me").then((response) => response.json());
    const ownerID = localStorage.getItem("gosshd_active_org") || me.organizations[0].id;
    const response = await fetch("/api/targets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        owner_type: "organization",
        owner_id: ownerID,
        target_type: "agent",
        name: "Private E2E node",
        alias,
        host: "127.0.0.1",
        port: 22,
        remote_username: "root",
        auth_type: "password",
        agent_id: `agent-${Date.now()}`,
        tags: ["private-refresh"],
      }),
    });
    if (!response.ok) throw new Error(await response.text());
    return (await response.json()).target.id;
  }, privateAlias);
  await page.getByRole("link", { name: /^Audit$/ }).click();
  await page.getByRole("link", { name: /SSH services/ }).click();
  await expectText(page, privateAlias);
  const privateRow = page.locator("tr").filter({ hasText: privateAlias }).first();
  await privateRow.getByText("root@127.0.0.1:22").waitFor();
  await privateRow.getByRole("button", { name: "Edit" }).click();
  await expectText(page, "Private node metadata");
  await expectText(page, "Replace private node");
  await expectText(page, "Tag colors");
  await expectCount(page.getByLabel("Target host"), 0);
  await expectCount(page.getByLabel("Target port"), 0);
  await expectCount(page.getByLabel("Remote username"), 0);
  await expectCount(page.getByLabel("Authentication method"), 0);
  await page.getByRole("button", { name: /Set tag color private-refresh blue/ }).click();
  await page.getByRole("button", { name: "Create replacement install token" }).click();
  await expectText(page, "systemctl");
  await page.getByRole("button", { name: "Close" }).last().click();
  await page.getByRole("button", { name: "Close" }).first().click();

  await page.getByRole("link", { name: /Command policy/ }).click();
  await page.getByRole("button", { name: "Create safety group" }).click();
  const policyDialog = page.getByRole("dialog", { name: "Create safety group" });
  await policyDialog.getByLabel("Name").fill("React readonly");
  await policyDialog.getByLabel("IP allowlist or ranges").fill("private");
  await policyDialog.getByText("Allow interactive terminal").click();
  await policyDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expectText(page, "React readonly");

  await page.getByRole("link", { name: /^Audit$/ }).click();
  await expectText(page, /No audit records|暂无审计记录/);
  await expectStaticEmptyState(page);

  await page.getByRole("link", { name: /System admin/ }).click();
  await expectText(page, "Account management");
  await page.getByRole("button", { name: /Account management/ }).click();
  await page.getByRole("dialog", { name: "Account management" }).waitFor();
  await page.getByRole("button", { name: "Reset password" }).first().click();
  await page.getByRole("dialog", { name: "Reset user password" }).waitFor();

  let terminalSocketCount = 0;
  page.on("websocket", (socket) => {
    if (socket.url().includes(`/api/targets/${externalTargetID}/terminal/ws`)) terminalSocketCount += 1;
  });
  await page.goto(`${baseURL}/targets/${externalTargetID}/connect`, { waitUntil: "domcontentloaded" });
  await page.locator(".terminal-reconnect-button").waitFor();
  const initialSocketCount = terminalSocketCount;
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.press("Enter");
  await waitUntil(() => terminalSocketCount > initialSocketCount, "Enter should reconnect a disconnected web terminal");

  await page.keyboard.press("Alt+N");
  const switcherSearch = page.locator("[data-connect-switcher-search]");
  await switcherSearch.waitFor();
  await switcherSearch.fill(privateAlias);
  await page.keyboard.press("Enter");
  await page.waitForFunction((id) => location.pathname === `/targets/${id}/connect`, privateTargetID);
  }
} finally {
  await context?.close().catch(() => {});
  await browser.close();
}

async function verifyChineseAuth() {
  const zh = await browser.newContext({ locale: "zh-CN" });
  try {
    const page = await zh.newPage();
    page.setDefaultTimeout(10_000);
    await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
    await page.getByRole("tab", { name: "登录" }).waitFor();
    await page.getByLabel("邮箱").fill("admin");
    await page.getByLabel("密码").fill("wrong-pass");
    await page.getByRole("button", { name: "登录" }).click();
    await page.locator(".status.error").filter({ hasText: "邮箱或密码不正确" }).waitFor();
    await expectCount(page.locator(".status.error").filter({ hasText: "invalid credentials" }), 0);
  } finally {
    await zh.close();
  }
}

async function assertStatus(page, route, expected) {
  const response = await page.goto(`${baseURL}${route}`, { waitUntil: "domcontentloaded" });
  if (!response || response.status() !== expected) {
    throw new Error(`${route} status mismatch: got ${response?.status()} want ${expected}`);
  }
}

async function expectText(page, text) {
  await page.getByText(text).first().waitFor();
}

async function expectCount(locator, expected) {
  const actual = await locator.count();
  if (actual !== expected) throw new Error(`count mismatch: got ${actual}, want ${expected}`);
}

async function expectStaticEmptyState(page) {
  const emptyState = page.locator(".empty-state").first();
  await emptyState.waitFor();
  const animationName = await emptyState.locator(".empty-orbit").evaluate((element) => getComputedStyle(element).animationName);
  if (animationName !== "none") throw new Error(`empty state should be static, got ${animationName}`);
}

async function waitUntil(predicate, message, timeout = 5_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(message);
}

function mustEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
