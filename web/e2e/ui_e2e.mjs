import { createRequire } from "node:module";

const baseURL = mustEnv("GOSSHD_UI_E2E_BASE_URL");
const playwrightPath = mustEnv("PLAYWRIGHT_REQUIRE_PATH");
const browserExecutable = mustEnv("PLAYWRIGHT_CHROMIUM_EXECUTABLE");

const require = createRequire(import.meta.url);
const { chromium } = require(playwrightPath);

const browser = await chromium.launch({
  executablePath: browserExecutable,
  headless: true,
});
let enContext;

try {
  const zhContext = await browser.newContext({ locale: "zh-CN" });
  await zhContext.addInitScript(() => {
    Object.defineProperty(navigator, "languages", { get: () => ["zh-CN", "zh"] });
    Object.defineProperty(navigator, "language", { get: () => "zh-CN" });
  });
  const zhPage = await zhContext.newPage();
  zhPage.setDefaultTimeout(10_000);
  await zhPage.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await zhPage.getByRole("tab", { name: "登录" }).waitFor();
  await zhContext.close();

  enContext = await browser.newContext({ locale: "en-US" });
  const page = await enContext.newPage();
  page.setDefaultTimeout(10_000);
  page.on("console", (message) => {
    if (message.type() === "error") console.error(`browser console: ${message.text()}`);
  });
  page.on("pageerror", (error) => {
    console.error(`browser page error: ${error.stack || error.message}`);
  });
  page.on("requestfailed", (request) => {
    console.error(`request failed: ${request.method()} ${request.url()} ${request.failure()?.errorText}`);
  });

  await assertStatus(page, "/app.js", 404);
  await assertStatus(page, "/unknown-route", 404);

  await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await page.getByRole("tab", { name: "登录" }).waitFor();
  if ((await page.evaluate(() => document.documentElement.dataset.theme)) !== "light") {
    throw new Error("default theme should be light");
  }
  await page.getByRole("button", { name: "黑" }).click();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_theme"))) !== "dark") {
    throw new Error("theme was not persisted after switching to dark");
  }
  await page.reload({ waitUntil: "networkidle" });
  if ((await page.evaluate(() => document.documentElement.dataset.theme)) !== "dark") {
    throw new Error("dark theme was not restored after reload");
  }
  await page.getByRole("button", { name: "白" }).click();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_theme"))) !== "light") {
    throw new Error("theme was not persisted after switching back to light");
  }
  await page.getByRole("button", { name: "EN" }).click();
  await expectFormCount(page, "login", 1);
  await expectFormCount(page, "register", 0);
  await page.getByRole("tab", { name: "Register" }).click();
  await expectFormCount(page, "register", 1);
  await expectFormCount(page, "login", 0);
  await page.getByRole("tab", { name: "Login" }).click();
  await expectFormCount(page, "login", 1);
  await expectFormCount(page, "register", 0);
  await page.getByRole("button", { name: "中文" }).click();
  await page.getByRole("tab", { name: "注册" }).waitFor();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_locale"))) !== "zh-CN") {
    throw new Error("locale was not persisted after switching to zh-CN");
  }
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("tab", { name: "登录" }).waitFor();
  await page.getByRole("button", { name: "EN" }).click();
  await page.getByRole("tab", { name: "Login" }).waitFor();
  const loginForm = page.locator('form[data-action="login"]');
  await loginForm.locator('input[name="email"]').fill("admin");
  await loginForm.locator('input[name="password"]').fill("admin-pass");
  await loginForm.getByRole("button", { name: "Sign in" }).click();
  await waitForHeading(page, "Control plane");
  await expectText(page, "System admin");

  await page.getByRole("button", { name: "Organizations" }).click();
  await page.getByLabel("Organization name").fill("UI Ops");
  await page.getByLabel("Organization slug").fill(`ui-ops-${Date.now()}`);
  await page.getByRole("button", { name: "Create organization" }).click();
  await page.getByRole("button", { name: "UI Ops" }).click();

  await page.getByRole("button", { name: "SSH services" }).click();
  await page.getByRole("button", { name: "Add service" }).first().click();
  const targetForm = page.locator('form[data-action="create-target"]');
  await targetForm.getByLabel("Service name").fill("UI Service");
  await targetForm.getByLabel("Target alias").fill("ui-test2");
  await targetForm.getByLabel("Target tags").fill("测试环境, ui");
  await targetForm.getByLabel("Target host").fill("127.0.0.1");
  await targetForm.getByLabel("Target port").fill("22");
  await targetForm.getByLabel("Remote username").fill("root");
  await targetForm.getByRole("button", { name: "Add service" }).click();
  await expectText(page, "ui-test2");
  await expectText(page, "测试环境");
  await page.getByRole("button", { name: "View details" }).click();
  await expectText(page, "Route preview");
  await closeDrawer(page);

  await page.getByRole("button", { name: "Agent SSH" }).click();
  await page.getByRole("button", { name: "New enrollment" }).click();
  const agentForm = page.locator('form[data-action="create-agent"]');
  await agentForm.getByLabel("Agent service alias").fill("ui-agent");
  await agentForm.getByLabel("Agent default host").fill("127.0.0.1");
  await agentForm.getByLabel("Agent default SSH port").fill("22");
  await agentForm.getByRole("button", { name: "Create enrollment" }).click();
  await expectText(page, "systemctl");
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  });
  await page.locator(".command-box").first().getByRole("button", { name: /Copy/ }).click();
  await expectText(page, "Copied");
  await page.getByRole("button", { name: "Windows" }).click();
  await expectText(page, "sc.exe");
  await closeDrawer(page);

  await page.getByRole("button", { name: "Command policy" }).click();
  await page.getByRole("button", { name: "Configure LLM" }).click();
  await expectModalCount(page, 1);
  await page.getByLabel("LLM config name").click();
  await expectModalCount(page, 1);
  await page.locator(".modal .icon-button").click();
  await expectModalCount(page, 0);

  await page.getByRole("button", { name: "System admin" }).click();
  await page.getByRole("heading", { name: "System administration" }).waitFor();
  await expectText(page, "Identity providers");
  await expectText(page, "Account management");
  await expectText(page, "Organization management");
  await expectCount(page.getByRole("button", { name: "Configure DingTalk" }), 0);
  await expectCount(page.getByRole("button", { name: "Configure LDAP" }), 0);
  const accountBlock = page.locator(".section-block").filter({ hasText: "Account management" }).first();
  await expectCount(accountBlock.locator(".cloud-table"), 0);
  await accountBlock.getByRole("button", { name: /Open account list/ }).click();
  await page.locator('form[data-action="admin-update-user"]').first().waitFor();
  await expectModalCount(page, 1);
  await page.locator(".modal").getByRole("button", { name: "Reset password" }).first().click();
  await page.locator('form[data-action="admin-reset-password"] input[name="password"]').fill("new-admin-pass");
  await page.locator('form[data-action="admin-reset-password"]').getByRole("button", { name: "Save new password" }).click();
  await expectText(page, "Saved");
  await expectModalCount(page, 0);
  const orgBlock = page.locator(".section-block").filter({ hasText: "Organization management" }).first();
  await expectCount(orgBlock.locator(".cloud-table"), 0);
  await orgBlock.getByRole("button", { name: /Open organization list/ }).click();
  await page.locator(".modal .cloud-table").waitFor();
  await expectModalCount(page, 1);
  await page.locator(".modal .icon-button").click();
  await expectModalCount(page, 0);
  await page.locator('.identity-grid [data-modal="admin-ldap"]').click();
  const ldapForm = page.locator('form[data-action="admin-save-ldap"]');
  await ldapForm.locator('input[name="server_url"]').fill("ldap://ui.example");
  await ldapForm.locator('input[name="bind_dn"]').fill("cn=reader,dc=ui,dc=example");
  await ldapForm.locator('input[name="base_dn"]').fill("dc=ui,dc=example");
  await ldapForm.locator('input[name="user_filter"]').fill("(uid={username})");
  await ldapForm.locator('input[name="email_attr"]').fill("mail");
  await ldapForm.locator('input[name="name_attr"]').fill("cn");
  await ldapForm.getByRole("button", { name: "Save LDAP settings" }).click();
  await expectText(page, "Saved");

  await page.getByRole("button", { name: "Members", exact: true }).click();
  await page.getByRole("heading", { name: "Organization members" }).waitFor();
  await expectText(page, "All Members");
} finally {
  await enContext?.close().catch(() => {});
  await browser.close();
}

function mustEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

async function assertStatus(page, route, expected) {
  const response = await page.goto(`${baseURL}${route}`, { waitUntil: "domcontentloaded" });
  if (!response || response.status() !== expected) {
    throw new Error(`${route} status mismatch: got ${response?.status()} want ${expected}`);
  }
}

async function expectText(page, text) {
  try {
    await page.getByText(text, { exact: false }).first().waitFor();
  } catch (error) {
    console.error(await page.locator("body").innerText().catch(() => "<body unavailable>"));
    throw error;
  }
}

async function expectFormCount(page, action, expected) {
  const count = await page.locator(`form[data-action="${action}"]`).count();
  if (count !== expected) throw new Error(`${action} form count mismatch: got ${count} want ${expected}`);
}

async function expectCount(locator, expected) {
  const count = await locator.count();
  if (count !== expected) throw new Error(`locator count mismatch: got ${count} want ${expected}`);
}

async function waitForHeading(page, name) {
  try {
    await page.getByRole("heading", { name }).waitFor();
  } catch (error) {
    console.error(await page.locator("body").innerText().catch(() => "<body unavailable>"));
    throw error;
  }
}

async function closeDrawer(page) {
  await page.locator(".drawer .icon-button").click();
}

async function expectModalCount(page, expected) {
  const count = await page.locator(".modal").count();
  if (count !== expected) throw new Error(`modal count mismatch: got ${count} want ${expected}`);
}
