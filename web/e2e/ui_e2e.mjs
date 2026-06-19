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
  await zhPage.locator('form[data-action="login"] input[name="email"]').fill("admin");
  await zhPage.locator('form[data-action="login"] input[name="password"]').fill("wrong-pass");
  await zhPage.locator('form[data-action="login"]').getByRole("button", { name: "登录" }).click();
  await zhPage.locator(".status.error").filter({ hasText: "邮箱或密码不正确" }).waitFor();
  await expectCount(zhPage.locator(".status.error").filter({ hasText: "invalid credentials" }), 0);
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
  await loginForm.locator('input[name="password"]').fill("wrong-pass");
  await loginForm.getByRole("button", { name: "Sign in" }).click();
  await page.locator(".status.error").filter({ hasText: "Email or password is incorrect" }).waitFor();
  await expectCount(page.locator(".status.error").filter({ hasText: "invalid credentials" }), 0);
  await loginForm.locator('input[name="email"]').fill("admin");
  await loginForm.locator('input[name="password"]').fill("admin-pass");
  await loginForm.getByRole("button", { name: "Sign in" }).click();
  await waitForHeading(page, "Control plane");
  await expectText(page, "System admin");
  await expectText(page, "Public-key user");
  await expectText(page, "Bastion decision path");
  await expectText(page, "Direct server");
  await expectText(page, "Private node");
  await expectCount(page.locator(".access-flow-map").getByText(/AI Agent|AI agent/), 0);
  await page.getByRole("button", { name: "Personal", exact: true }).click();
  await expectText(page, "Personal settings");
  await expectText(page, "Security settings");
  await page.getByRole("button", { name: "Change password" }).click();
  const ownPasswordForm = page.locator('form[data-action="change-own-password"]');
  await ownPasswordForm.locator('input[name="current_password"]').fill("admin-pass");
  await ownPasswordForm.locator('input[name="new_password"]').fill("new-admin-pass");
  await ownPasswordForm.locator('input[name="confirm_password"]').fill("different-pass");
  await ownPasswordForm.getByRole("button", { name: "Save password" }).click();
  await expectText(page, "New passwords do not match");
  await page.locator(".modal .icon-button").click();
  await expectModalCount(page, 0);
  await expectMobileSidebar(page);
  await page.setViewportSize({ width: 1280, height: 900 });

  await page.getByRole("button", { name: "Public keys" }).click();
  await page.getByRole("button", { name: "Add key" }).click();
  await addPublicKey(page, "UI laptop", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUTThKwa4NlLwH7sntZnYosUoFkNceEce0kEwbE9nNm ui-key-1");
  await page.getByRole("button", { name: "Add key" }).click();
  await addPublicKey(page, "UI workstation", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID+3+em5yQ+epdGqgc0iFaY0LT4d+6m3pqUva06UKvlw ui-key-2");
  await expectText(page, "UI laptop");
  await expectText(page, "UI workstation");
  await expectText(page, "2");

  await page.getByRole("button", { name: "Organizations" }).click();
  await expectFormCount(page, "create-org", 0);
  await expectFormCount(page, "join-org", 0);
  await expectText(page, "Type");
  await expectCount(page.locator(".cloud-table th").filter({ hasText: "Slug" }), 0);
  await page.getByRole("button", { name: "Create organization" }).click();
  await page.getByLabel("Organization name").fill("UI Ops");
  await page.getByLabel("Organization slug").fill(`ui-ops-${Date.now()}`);
  await page.locator('form[data-action="create-org"]').getByRole("button", { name: "Create organization" }).click();
  await page.getByRole("button", { name: "UI Ops" }).click();

  await page.getByRole("button", { name: "SSH services" }).click();
  await expectCount(page.getByRole("button", { name: "Agent SSH" }), 0);
  await page.getByRole("button", { name: "Add service" }).first().click();
  const targetForm = page.locator('form[data-action="create-target"]');
  await targetForm.getByLabel("Service name").fill("UI Service");
  await targetForm.getByLabel("Target alias").fill("ui-test2");
  await targetForm.getByLabel("Target tags").fill("测试环境, ui");
  await expectCount(targetForm.locator(".tag-input-chip").filter({ hasText: "测试环境" }), 1);
  await expectCount(targetForm.locator(".tag-input-chip").filter({ hasText: "ui" }), 1);
  await targetForm.getByRole("button", { name: "Next" }).click();
  await targetForm.getByLabel("Target host").fill("127.0.0.1");
  await targetForm.getByLabel("Target port").fill("22");
  await targetForm.getByLabel("Remote username").fill("root");
  await targetForm.getByRole("button", { name: "Next" }).click();
  await targetForm.getByLabel("Target secret").fill("root-pass");
  await targetForm.getByRole("button", { name: "Next" }).click();
  await targetForm.getByRole("button", { name: "Add service" }).click();
  await expectText(page, "ui-test2");
  await expectText(page, "测试环境");
  const copySSHButton = page.getByRole("button", { name: "Copy SSH command" }).first();
  await copySSHButton.waitFor();
  const copySSHCommand = await copySSHButton.getAttribute("data-value");
  if (copySSHCommand !== "ssh ui-test2@127.0.0.1") {
    throw new Error(`copy ssh command mismatch: ${copySSHCommand}`);
  }
  await copySSHButton.click();
  await page.locator(".copy-tip").filter({ hasText: "Copied" }).waitFor();
  const createdTag = page.locator('.cloud-table .tag-chip[data-tag="测试环境"]').first();
  await createdTag.waitFor();
  await expectTagPaletteClass(createdTag);
  await page.getByRole("button", { name: "View details" }).click();
  await expectText(page, "Route preview");
  await expectText(page, "Tag colors");
  const detailForm = page.locator('form[data-action="rename-target"]');
  await detailForm.getByLabel("Target host").fill("127.0.0.2");
  await detailForm.getByLabel("Target port").fill("2222");
  await detailForm.getByLabel("Remote username").fill("ubuntu");
  await detailForm.getByLabel("Target secret").fill("rotated-pass");
  await detailForm.getByRole("button", { name: "Save" }).click();
  await expectText(page, "Saved");
  await expectText(page, "ubuntu@127.0.0.2:2222");
  const detailTagInput = page.locator(".drawer [data-tag-input]").first();
  await detailTagInput.locator(".tag-input-chip").filter({ hasText: "ui" }).getByRole("button").click();
  await expectCount(detailTagInput.locator(".tag-input-chip").filter({ hasText: "ui" }), 0);
  await detailTagInput.getByLabel("Tags").fill("u");
  await detailTagInput.locator('[data-click="tag-input-select"][data-tag="ui"]').click();
  await expectCount(detailTagInput.locator(".tag-input-chip").filter({ hasText: "ui" }), 1);
  const tagColorRow = page.locator(".tag-color-row").filter({ hasText: "测试环境" }).first();
  await tagColorRow.locator('button[data-color="blue"]').click();
  await page.locator('.drawer .tag-chip[data-tag="测试环境"].tag-color-blue').first().waitFor();
  await closeDrawer(page);

  await page.getByRole("button", { name: "Add service" }).first().click();
  await page.getByRole("button", { name: "Private node" }).click();
  const agentForm = page.locator('form[data-action="create-agent"]');
  await expectCount(agentForm.getByLabel("Agent default host"), 0);
  await expectCount(agentForm.getByLabel("Agent default SSH port"), 0);
  await agentForm.getByLabel("Service alias").fill("ui-agent");
  await agentForm.getByRole("button", { name: "Create install token" }).click();
  await expectText(page, "systemctl");
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  });
  await page.locator(".command-box").first().getByRole("button", { name: /Copy/ }).click();
  await page.locator(".command-box .copy-tip").filter({ hasText: "Copied" }).waitFor();
  await expectCount(page.locator(".status.ok").filter({ hasText: "Copied" }), 0);
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

  await page.getByRole("button", { name: "Audit", exact: true }).click();
  await expectText(page, "No audit rows");
  await expectStaticEmptyState(page);
  await seedAuditRows(page);
  await page.getByRole("button", { name: "Dashboard" }).click();
  await page.getByRole("button", { name: "Audit", exact: true }).click();
  await expectText(page, "Read-only Docker status inspection");
  await expectText(page, "UI laptop");
  await expectText(page, "SHA256:ui-key");
  await expectText(page, "UI Service");
  await expectLightTablePalette(page);

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
  await expectModalScrollContainment(page);
  await page.locator(".modal").getByRole("button", { name: "Reset password" }).first().click();
  await expectModalCount(page, 2);
  await page.getByRole("dialog", { name: "Reset user password" }).waitFor();
  await page.locator('form[data-action="admin-reset-password"] input[name="password"]').fill("new-admin-pass");
  await page.locator('form[data-action="admin-reset-password"]').getByRole("button", { name: "Save new password" }).click();
  await expectText(page, "Saved");
  await expectModalCount(page, 1);
  await page.getByRole("dialog", { name: "Account management" }).waitFor();
  await page.getByRole("dialog", { name: "Account management" }).locator(".icon-button").click();
  await expectModalCount(page, 0);
  const orgBlock = page.locator(".section-block").filter({ hasText: "Organization management" }).first();
  await expectCount(orgBlock.locator(".cloud-table"), 0);
  await orgBlock.getByRole("button", { name: /Open organization list/ }).click();
  await page.locator(".modal .cloud-table").waitFor();
  await expectModalCount(page, 1);
  await expectModalVisibleTextNoUUID(page);
  await page.locator(".modal").getByRole("button", { name: "Manage members" }).first().click();
  await page.locator(".drawer").waitFor();
  await expectAdminOrgDrawerLayout(page);
  await closeDrawer(page);
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
  await expectText(page, "Joined");
  await expectFormCount(page, "add-org-member", 0);
  await expectFormCount(page, "create-group", 0);
  await page.locator('form[data-action="set-member-filter"] input[name="query"]').fill("admin");
  await page.locator('form[data-action="set-member-filter"]').getByRole("button", { name: "Search" }).click();
  await expectText(page, "Administrator");
  await expectVisibleTextNoUUID(page.locator(".section-block").filter({ hasText: "Joined" }).first(), "member list");
  await page.getByRole("button", { name: "Newest" }).click();
  await page.getByRole("button", { name: "Add member" }).click();
  await expectModalCount(page, 1);
  await page.locator('form[data-action="add-org-member"]').waitFor();
  await page.locator(".modal .icon-button").click();
  await expectModalCount(page, 0);
  await page.getByRole("button", { name: "User groups" }).click();
  await expectModalCount(page, 1);
  await page.locator('form[data-action="create-group"]').waitFor();
  await page.locator(".modal .icon-button").click();
  await expectModalCount(page, 0);
  await page.getByRole("button", { name: "Transfer owner" }).click();
  await expectModalCount(page, 1);
  await expectText(page, "No transfer candidate");
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

async function expectTagPaletteClass(locator) {
  const className = await locator.getAttribute("class");
  if (!/\btag-color-(gray|red|orange|yellow|green|blue|purple)\b/.test(className || "")) {
    throw new Error(`tag should use a fixed palette class, got ${className}`);
  }
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

async function addPublicKey(page, name, publicKey) {
  const form = page.locator('form[data-action="create-key"]');
  await form.getByLabel("Public key name").fill(name);
  await form.getByLabel("Authorized public key").fill(publicKey);
  const validity = await form.evaluate((element) => ({
    valid: element.checkValidity(),
    name: element.elements.name?.value || "",
    keyLength: element.elements.authorized_key?.value.length || 0,
  }));
  if (!validity.valid) {
    throw new Error(`public key form is invalid before submit: ${JSON.stringify(validity)}`);
  }
  await form.evaluate((element) => element.requestSubmit());
  try {
    await page.locator(".modal").waitFor({ state: "detached" });
  } catch (error) {
    console.error(await page.locator("body").innerText().catch(() => "<body unavailable>"));
    throw error;
  }
}

async function seedAuditRows(page) {
  await page.evaluate(async () => {
    const { state } = await import("/state.js");
    state.audit = [{
      command: "docker ps -q | wc -l; docker ps -a -q | wc -l",
      user_display_name: "Administrator",
      user_email: "admin",
      public_key_name: "UI laptop",
      public_key_fingerprint: "SHA256:ui-key",
      target_name: "UI Service",
      target_alias: "ui-test2",
      target_endpoint: "root@127.0.0.1:22",
      policy_decision: "allow",
      policy_reason: "llm: Read-only Docker status inspection",
      exit_code: 0,
      started_at: new Date().toISOString(),
    }];
  });
}

async function expectLightTablePalette(page) {
  const metrics = await page.locator(".table-wrap").first().evaluate((element) => {
    const table = element.querySelector("table");
    const th = element.querySelector("th");
    const td = element.querySelector("td");
    const code = element.querySelector("code");
    const rgb = (value) => {
      const match = value.match(/rgba?\(([^)]+)\)/);
      if (!match) return [0, 0, 0];
      return match[1].split(",").slice(0, 3).map((part) => Number(part.trim()));
    };
    return {
      tableBg: rgb(getComputedStyle(table).backgroundColor),
      thBg: rgb(getComputedStyle(th).backgroundColor),
      tdBg: rgb(getComputedStyle(td).backgroundColor),
      codeBg: rgb(getComputedStyle(code).backgroundColor),
      tdColor: rgb(getComputedStyle(td).color),
    };
  });
  for (const [name, value] of Object.entries({
    tableBg: metrics.tableBg,
    thBg: metrics.thBg,
    tdBg: metrics.tdBg,
    codeBg: metrics.codeBg,
  })) {
    const average = value.reduce((sum, item) => sum + item, 0) / value.length;
    if (average < 220) throw new Error(`light audit table ${name} is too dark: ${value.join(",")}`);
  }
  const textAverage = metrics.tdColor.reduce((sum, item) => sum + item, 0) / metrics.tdColor.length;
  if (textAverage > 90) throw new Error(`light audit table text is too pale: ${metrics.tdColor.join(",")}`);
}

async function expectStaticEmptyState(page) {
  const emptyState = page.locator(".empty-state").first();
  const orbit = emptyState.locator(".empty-orbit");
  await orbit.waitFor();
  const styles = await orbit.evaluate((element) => {
    const orbitStyle = getComputedStyle(element);
    const emptyStyle = getComputedStyle(element.closest(".empty-state"));
    return {
      animationName: orbitStyle.animationName,
      backgroundImage: emptyStyle.backgroundImage,
    };
  });
  if (styles.animationName !== "none") {
    throw new Error(`empty state should be static, got animation ${styles.animationName}`);
  }
  if (!styles.backgroundImage.includes("255, 255, 255")) {
    throw new Error(`light empty state should use a light background, got ${styles.backgroundImage}`);
  }
}

async function expectModalScrollContainment(page) {
  const metrics = await page.locator(".modal").first().evaluate((element) => {
    const body = element.querySelector(".surface-body");
    const table = element.querySelector(".modal-list-shell > .cloud-table");
    return {
      modalOverflow: getComputedStyle(element).overflowY,
      bodyOverflow: body ? getComputedStyle(body).overflowY : "",
      tableOverflow: table ? getComputedStyle(table).overflowY : "",
    };
  });
  if (metrics.modalOverflow !== "hidden") {
    throw new Error(`modal should not own scrolling; got ${metrics.modalOverflow}`);
  }
  if (metrics.bodyOverflow !== "hidden") {
    throw new Error(`list modal body should contain scroll regions; got ${metrics.bodyOverflow}`);
  }
  if (metrics.tableOverflow !== "auto") {
    throw new Error(`modal table should own scrolling; got ${metrics.tableOverflow}`);
  }
}

async function expectModalVisibleTextNoUUID(page) {
  await expectVisibleTextNoUUID(page.locator(".modal").first(), "organization modal");
}

async function expectVisibleTextNoUUID(locator, label) {
  const text = await locator.innerText();
  if (/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i.test(text)) {
    throw new Error(`${label} should not expose UUIDs in visible text: ${text}`);
  }
}

async function expectAdminOrgDrawerLayout(page) {
  await expectCount(page.locator(".drawer .cloud-table"), 0);
  const cardCount = await page.locator(".drawer .admin-member-card").count();
  if (cardCount < 1) throw new Error("admin organization drawer should render member cards");
  await expectCount(page.locator(".drawer .admin-member-card.owner"), 1);
  await expectCount(page.locator('.drawer .admin-member-card.owner [data-click="admin-transfer-org-owner"]'), 0);
  const metrics = await page.locator(".drawer").evaluate((element) => {
    const list = element.querySelector(".admin-member-list");
    const backgroundColor = getComputedStyle(element).backgroundColor;
    const alpha = backgroundColor.startsWith("rgba(")
      ? Number(backgroundColor.split(",").at(-1).replace(")", "").trim())
      : 1;
    return {
      alpha,
      backgroundColor,
      listClientWidth: list ? list.clientWidth : 0,
      listScrollWidth: list ? list.scrollWidth : 0,
    };
  });
  if (metrics.alpha < 1) {
    throw new Error(`drawer should be opaque, got ${metrics.backgroundColor}`);
  }
  if (metrics.listScrollWidth > metrics.listClientWidth + 1) {
    throw new Error(`admin member list should not scroll horizontally: ${JSON.stringify(metrics)}`);
  }
}

async function expectMobileSidebar(page) {
  await page.setViewportSize({ width: 533, height: 900 });
  await page.waitForFunction(() => {
    const sidebar = document.querySelector(".sidebar");
    const menuButton = document.querySelector(".mobile-menu-button");
    if (!sidebar || !menuButton) return false;
    const rect = sidebar.getBoundingClientRect();
    return rect.right < 8 && getComputedStyle(menuButton).display !== "none";
  });
  await page.getByRole("button", { name: "Menu" }).click();
  await page.waitForFunction(() => {
    const sidebar = document.querySelector(".sidebar");
    const scrim = document.querySelector(".sidebar-scrim");
    if (!sidebar || !scrim) return false;
    const rect = sidebar.getBoundingClientRect();
    return rect.left > -2 && Number(getComputedStyle(scrim).opacity) > 0.8;
  });
  await page.locator(".sidebar").getByRole("button", { name: "Dashboard" }).click();
  await page.waitForFunction(() => {
    const sidebar = document.querySelector(".sidebar");
    if (!sidebar) return false;
    return sidebar.getBoundingClientRect().right < 8;
  });
}
