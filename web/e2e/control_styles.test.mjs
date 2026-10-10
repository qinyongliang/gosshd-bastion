import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
try {
  const context = await browser.newContext({ locale: "zh-CN", reducedMotion: "reduce", viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(() => { localStorage.setItem("gosshd_locale", "zh-CN"); localStorage.setItem("gosshd_theme", "light"); });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  const members = [{ user_id: "style-owner", display_name: "所有者", email: "owner@test", role: "owner" }, { user_id: "style-member", display_name: "样式测试成员", email: "member@test", role: "member" }];
  await page.route("**/api/orgs/*/members", (route) => route.fulfill({ json: { members } }));
  await page.route("**/api/admin/users", (route) => route.fulfill({ json: { users: members.map((m, i) => ({ ...m, id: m.user_id, is_system_admin: i === 0, auth_provider: "local" })) } }));
  await page.route("**/api/admin/orgs", (route) => route.fulfill({ json: { organizations: [{ id: "style-org", name: "样式测试组织", role: "owner", is_personal: false }] } }));
  await page.route("**/api/admin/orgs/*/members", (route) => route.fulfill({ json: { members } }));
  await page.goto(process.env.GOSSHD_UI_E2E_BASE_URL);
  await page.getByLabel("邮箱", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill("admin-pass");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("link", { name: "组织", exact: true }).click();
  await page.getByRole("button", { name: "创建组织", exact: true }).click();
  await checkFooter(page.getByRole("dialog"));
  await page.locator(".ant-modal-close").click();
  await page.getByRole("link", { name: "成员", exact: true }).click();
  await page.getByRole("button", { name: "添加成员", exact: true }).click();
  await page.getByRole("dialog").locator(".ant-select-content").getByText("成员", { exact: true }).waitFor();
  await checkFooter(page.getByRole("dialog"));
  await page.locator(".ant-modal-close").click();
  await page.getByRole("button", { name: "转移所有者", exact: true }).click();
  await page.getByRole("dialog").locator(".ant-select").click();
  await page.locator(".ant-select-dropdown:visible").getByText("样式测试成员", { exact: true }).click();
  await checkSelect(page.getByRole("dialog").locator(".ant-select"));
  await page.locator(".ant-modal-close").click();
  await page.getByRole("link", { name: "系统管理", exact: true }).click();
  await page.locator(".admin-card").filter({ hasText: "账号管理" }).click();
  await page.getByRole("switch").first().waitFor();
  for (const item of await page.getByRole("switch").all()) {
    const result = await item.evaluate((el) => { const r = el.getBoundingClientRect(), c = el.parentElement.getBoundingClientRect(); return { width: r.width, height: r.height, offset: r.x + r.width / 2 - c.x - c.width / 2 }; });
    assert.equal(result.width, 44);
    assert.equal(result.height, 22);
    assert.ok(Math.abs(result.offset) < 1, "Admin switches must align with the centered column heading");
  }
  await page.locator(".ant-modal-close").click();
  await page.locator(".admin-card").filter({ hasText: "组织管理" }).click();
  await page.locator(".admin-orgs-table").getByRole("button", { name: "成员", exact: true }).click();
  await page.locator(".admin-member-list .ant-select").waitFor();
  await checkSelect(page.locator(".admin-member-list .ant-select"));
  assert.equal((await page.locator(".admin-member-list .ant-select-content").textContent()).trim(), "成员");
  await page.locator(".ant-drawer-close").click();
  await page.locator(".ant-modal-close").click();
  const target = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((r) => r.json());
    const response = await fetch("/api/targets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, name: "样式测试服务", alias: "style-test", target_type: "direct", host: "127.0.0.1", port: 22, remote_username: "root", auth_type: "password", secret: "test", tags: ["生产环境"] }) });
    if (!response.ok) throw new Error(await response.text());
    return (await response.json()).target;
  });
  await page.getByRole("link", { name: "SSH 服务", exact: true }).click();
  const row = page.locator(".target-tree-row").filter({ hasText: target.alias });
  await row.getByRole("button", { name: "更多", exact: true }).click();
  await page.getByRole("menu").getByRole("button", { name: "编辑", exact: true }).click();
  await checkFooter(page.locator(".ant-drawer"));
  const spacing = await page.locator(".ant-drawer-body").evaluate((el) => ({ formBottom: el.querySelector("form").getBoundingClientRect().bottom, colorsTop: el.querySelector("form + .section-block").getBoundingClientRect().top }));
  assert.ok(spacing.colorsTop - spacing.formBottom >= 20, "Tag color section must not overlap the advanced field or actions");
  await page.locator(".ant-drawer-close").click();
  await page.route("**/api/targets/*/files?**", (route) => route.fulfill({ json: { path: "/", entries: [{ name: "very-long-filename-for-ellipsis.log", path: "/example.log", type: "file", size: 1024, mode: "-rw-r--r--", modified_at: "2026-10-09T00:00:00Z" }] } }));
  await page.route("**/api/targets/*/files/read?**", (route) => route.fulfill({ json: { path: "/example.log", content: "style check\n" } }));
  await page.goto(`${process.env.GOSSHD_UI_E2E_BASE_URL}/targets/${target.id}/connect`);
  await page.locator(".terminal-panel").waitFor();
  await page.locator(".files-zone").waitFor();
  if (await page.locator(".files-zone .collapsed-zone-button").isVisible()) await page.locator(".files-zone .collapsed-zone-button").click();
  await page.locator(".file-name").first().waitFor();
  const fileBounds = await page.locator(".files-zone .file-manager-body").evaluate((el) => ({ width: el.clientWidth, scroll: el.scrollWidth, rowHeight: el.querySelector("tbody tr").getBoundingClientRect().height }));
  assert.ok(fileBounds.scroll <= fileBounds.width + 1, "Narrow file sidebar must not need horizontal scrolling");
  assert.ok(fileBounds.rowHeight <= 42, "File rows should remain compact");
  await page.locator(".file-name").first().click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "编辑", exact: true }).click();
  await page.locator(".editor-pane-head").waitFor();
  for (const theme of ["light", "dark", "light"]) {
    await page.locator(".connect-appbar-actions").getByRole("button", { name: theme === "light" ? "白" : "黑", exact: true }).click();
    await page.waitForFunction((theme) => document.documentElement.dataset.theme === theme, theme);
    for (const selector of [".connect-appbar", ".files-zone .connect-zone-head", ".editor-pane-head"]) {
      const color = await page.locator(selector).evaluate((el) => getComputedStyle(el).backgroundColor);
      const channels = color.match(/[\d.]+/g).slice(0, 3).map(Number);
      assert.ok(theme === "light" ? Math.min(...channels) > 220 : Math.max(...channels) < 80, `${selector} must follow ${theme} theme: ${color}`);
    }
  }
  await context.close();
} finally { await browser.close(); }

async function checkFooter(container) {
  const result = await container.locator("form").first().evaluate((form) => { const a = form.querySelector(".form-actions").getBoundingClientRect(); return { width: a.width, formWidth: form.clientWidth, top: a.top, fieldsBottom: Math.max(...[...form.querySelectorAll(".field")].map((el) => el.getBoundingClientRect().bottom)) }; });
  assert.ok(Math.abs(result.width - result.formWidth) < 2, "Form actions must span both columns");
  assert.ok(result.top >= result.fieldsBottom + 4, "Actions must sit below form fields");
}
async function checkSelect(select) {
  const result = await select.evaluate((el) => { const input = el.querySelector("input"), content = el.querySelector(".ant-select-content"); const r = el.getBoundingClientRect(), c = content.getBoundingClientRect(); return { inputHeight: input.getBoundingClientRect().height, height: r.height, offset: c.y + c.height / 2 - r.y - r.height / 2, fill: getComputedStyle(el.querySelector("svg")).fill }; });
  assert.ok(result.inputHeight <= result.height, "Select input must not overflow the control");
  assert.ok(Math.abs(result.offset) < 2, "Selected text must be vertically centered");
  assert.notEqual(result.fill, "none", "Select arrow must retain its filled icon");
}
