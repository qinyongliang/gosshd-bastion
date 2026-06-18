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

try {
  const page = await browser.newPage();
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
  await page.getByLabel("Service name").fill("UI Service");
  await page.getByLabel("Target alias").fill("ui-test2");
  await page.getByLabel("Target tags").fill("测试环境, ui");
  await page.getByLabel("Target host").fill("127.0.0.1");
  await page.getByLabel("Target port").fill("22");
  await page.getByLabel("Remote username").fill("root");
  await page.getByRole("button", { name: "Add service" }).click();
  await expectText(page, "ui-test2");
  await expectText(page, "测试环境");

  await page.getByRole("button", { name: "Agent SSH" }).click();
  await page.getByLabel("Agent service alias").fill("ui-agent");
  await page.getByLabel("Agent default host").fill("127.0.0.1");
  await page.getByLabel("Agent default SSH port").fill("22");
  await page.getByRole("button", { name: "Create enrollment" }).click();
  await expectText(page, "systemctl");
  await expectText(page, "sc.exe");

  await page.getByRole("button", { name: "System admin" }).click();
  await page.getByRole("heading", { name: "System administration" }).waitFor();
  await expectText(page, "Global settings");
  await expectText(page, "Account management");
  await expectText(page, "Organization management");
  await page.locator('form[data-action="admin-save-ldap"] input[name="server_url"]').fill("ldap://ui.example");
  await page.locator('form[data-action="admin-save-ldap"] input[name="bind_dn"]').fill("cn=reader,dc=ui,dc=example");
  await page.locator('form[data-action="admin-save-ldap"] input[name="base_dn"]').fill("dc=ui,dc=example");
  await page.locator('form[data-action="admin-save-ldap"] input[name="user_filter"]').fill("(uid={username})");
  await page.locator('form[data-action="admin-save-ldap"] input[name="email_attr"]').fill("mail");
  await page.locator('form[data-action="admin-save-ldap"] input[name="name_attr"]').fill("cn");
  await page.locator('form[data-action="admin-save-ldap"]').getByRole("button", { name: "Save LDAP settings" }).click();
  await expectText(page, "Saved");

  await page.getByRole("button", { name: "Members", exact: true }).click();
  await page.getByRole("heading", { name: "Organization members" }).waitFor();
  await expectText(page, "All Members");
} finally {
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
  await page.getByText(text, { exact: false }).first().waitFor();
}

async function waitForHeading(page, name) {
  try {
    await page.getByRole("heading", { name }).waitFor();
  } catch (error) {
    console.error(await page.locator("body").innerText().catch(() => "<body unavailable>"));
    throw error;
  }
}
