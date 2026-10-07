import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;
try {
  const context = await browser.newContext({ locale: "zh-CN", reducedMotion: "reduce", viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  let commandSubmissions = 0;
  page.on("request", (request) => { if (request.method() === "POST" && new URL(request.url()).pathname === "/api/batch-command-histories") commandSubmissions++; });
  await page.route("**/api/audit?**", (route) => {
    const params = new URL(route.request().url()).searchParams;
    const current = Number(params.get("page") || 1), size = Number(params.get("page_size") || 20);
    const total = params.get("query") === "limited" ? 5 : 235;
    const offset = (current - 1) * size;
    const logs = Array.from({ length: Math.max(0, Math.min(size, total - offset)) }, (_, i) => ({ id: `audit-${offset + i}`, command: `echo audit-${offset + i + 1}`, request_type: "exec", policy_decision: "allow", policy_reason: "allowed", started_at: "2026-10-07T00:00:00Z", ended_at: "2026-10-07T00:00:01Z", exit_code: 0 }));
    return route.fulfill({ json: { logs, total, page: current, page_size: size } });
  });
  await page.route("**/api/batch-command-histories?**", (route) => {
    const params = new URL(route.request().url()).searchParams;
    const current = Number(params.get("page") || 1), size = Number(params.get("page_size") || 10), total = 135, offset = (current - 1) * size;
    const histories = Array.from({ length: Math.max(0, Math.min(size, total - offset)) }, (_, i) => ({ id: `history-${offset + i}`, command: `echo history-${offset + i + 1}`, execute_count: 1 }));
    return route.fulfill({ json: { histories, total, page: current, page_size: size } });
  });
  await page.goto(baseURL, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill("admin");
  await page.getByLabel("密码").fill("admin-pass");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("link", { name: "审计", exact: true }).click();
  const auditPager = page.locator(".audit-page .app-pagination");
  await page.locator("td[data-column=command]").first().getByText("echo audit-1", { exact: true }).waitFor();
  assert.equal(await auditPager.getByRole("button", { name: "下一步", exact: true }).count(), 0);
  await move(auditPager, "/api/audit", 2, 20, () => auditPager.getByRole("button", { name: "下一页", exact: true }).click());
  await size(auditPager, "/api/audit", 50);
  await move(auditPager, "/api/audit", 3, 50, async () => { const input = auditPager.locator(".ant-pagination-options-quick-jumper input"); await input.fill("3"); await input.press("Enter"); });
  await page.locator("td[data-column=command]").first().getByText("echo audit-101", { exact: true }).waitFor();
  await page.locator('.audit-page input[name="query"]').fill("limited");
  await move(auditPager, "/api/audit", 1, 50, () => page.getByRole("button", { name: "搜索", exact: true }).click());
  await page.getByText("共 5 条", { exact: true }).waitFor();
  assert.ok(await auditPager.getByRole("button", { name: "下一页", exact: true }).isDisabled());
  const target = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((response) => response.json());
    const response = await fetch("/api/targets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, name: "分页测试服务", alias: "pagination-test", target_type: "direct", host: "127.0.0.1", port: 22, remote_username: "root", auth_type: "password", secret: "test-pass" }) });
    if (!response.ok) throw new Error("Could not create fixture target");
    return (await response.json()).target;
  });
  await page.getByRole("link", { name: "SSH 服务", exact: true }).click();
  const row = page.locator(".target-tree-row").filter({ hasText: target.alias });
  await row.waitFor();
  await page.locator(".resource-actions").getByRole("button", { name: "更多", exact: true }).click();
  await page.getByRole("menu").getByRole("button", { name: "多选操作", exact: true }).click();
  await row.locator(".tree-check").click();
  await page.getByRole("button", { name: "批量执行命令", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "批量执行命令", exact: true });
  const historyPager = dialog.locator(".app-pagination");
  await dialog.locator(".batch-command-history-list>button").first().waitFor();
  await dialog.locator('textarea[name="command"]').fill("echo safe-pagination-check");
  await size(historyPager, "/api/batch-command-histories", 50);
  await move(historyPager, "/api/batch-command-histories", 2, 50, async () => { const input = historyPager.locator(".ant-pagination-options-quick-jumper input"); await input.fill("2"); await input.press("Enter"); });
  await dialog.getByText("echo history-51", { exact: true }).waitFor();
  assert.equal(commandSubmissions, 0, "Jumping pages must not execute the batch command form");
  await move(historyPager, "/api/batch-command-histories", 1, 50, async () => { await historyPager.locator(".ant-pagination-options-quick-jumper input").fill("1"); await historyPager.getByRole("button", { name: "跳转", exact: true }).click(); });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const bounds = await historyPager.evaluate((pager) => ({ scroll: pager.scrollWidth, width: pager.clientWidth }));
    assert.ok(bounds.scroll <= bounds.width + 1, "Pagination should wrap without horizontal scrolling");
    await historyPager.getByRole("combobox", { name: "每页条数", exact: true }).waitFor();
    await historyPager.locator(".ant-pagination-options-quick-jumper input").waitFor();
  }
  await context.close();

  async function move(pager, path, nextPage, pageSize, action) {
    const response = page.waitForResponse((response) => { const url = new URL(response.url()); return url.pathname === path && Number(url.searchParams.get("page")) === nextPage && Number(url.searchParams.get("page_size")) === pageSize; });
    await action();
    assert.equal((await response).status(), 200);
  }
  async function size(pager, path, pageSize) {
    await move(pager, path, 1, pageSize, async () => { await pager.getByRole("combobox", { name: "每页条数", exact: true }).click(); await page.getByRole("option", { name: new RegExp(`^${pageSize} `) }).click(); });
  }
} finally { await browser.close(); }
