import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const traffic = { bucket_start: Math.floor(Date.now() / 300000) * 300, relay_up: 1024, relay_down: 2048, direct_up: 4096, direct_down: 8192, connections_opened: 3, peak_connections: 2 };
const sources = [
  { ...traffic, source_ip: "192.0.2.1", direct_up: 0, direct_down: 0, connections_opened: 8, active_connections: 1 },
  { ...traffic, source_ip: "2001:db8::1", relay_up: 0, relay_down: 0, connections_opened: 3, active_connections: 1 },
];
const fixtures = [
  { id: "managed", name: "Traffic test" },
  { id: "local", name: "SSH local", temporary: true, forward_type: "local" },
  { id: "remote", name: "SSH remote", temporary: true, forward_type: "remote" },
].map((t) => ({ entry_target_id: "", exit_target_id: "", listen_host: "127.0.0.1", listen_port: 8080, listen_address: "127.0.0.1:8080", destination_host: "127.0.0.1", destination_port: 80, duration_seconds: 0, enabled: true, status: "running", transport: "mixed", connections: 2, direct_connections: 1, traffic, ...t }));
try {
  for (const locale of ["zh-CN", "en"]) {
    const zh = locale === "zh-CN";
    const context = await browser.newContext({ locale: zh ? "zh-CN" : "en-US", reducedMotion: "reduce", viewport: { width: 1440, height: 1000 } });
    await context.addInitScript((locale) => localStorage.setItem("gosshd_locale", locale), locale);
    const page = await context.newPage();
    page.setDefaultTimeout(10_000);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.route("**/api/tunnels?**", (route) => route.fulfill({ json: { tunnels: fixtures } }));
    const requests = [];
    await page.route("**/api/tunnels/*/traffic?**", (route) => {
      const params = new URL(route.request().url()).searchParams;
      requests.push(params);
      const ip = params.get("source_ip");
      const matched = sources.filter((row) => (!ip || row.source_ip === ip) && row.bucket_start >= Number(params.get("from")) && row.bucket_start < Number(params.get("to")));
      const bucket = matched.reduce((sum, row) => ({ ...sum, relay_up: sum.relay_up + row.relay_up, relay_down: sum.relay_down + row.relay_down, direct_up: sum.direct_up + row.direct_up, direct_down: sum.direct_down + row.direct_down, connections_opened: sum.connections_opened + row.connections_opened, peak_connections: Math.max(sum.peak_connections, row.peak_connections) }), { ...traffic, relay_up: 0, relay_down: 0, direct_up: 0, direct_down: 0, connections_opened: 0, peak_connections: 0 });
      return route.fulfill({ json: { interval_seconds: 300, buckets: [bucket], sources: matched, active_connections: matched.length } });
    });
    await page.goto(process.env.GOSSHD_UI_E2E_BASE_URL);
    await page.getByLabel(zh ? "邮箱" : "Email", { exact: true }).fill("admin");
    await page.getByLabel(zh ? "密码" : "Password", { exact: true }).fill("admin-pass");
    await page.getByRole("button", { name: zh ? "登录" : "Sign in", exact: true }).click();
    await page.getByRole("link", { name: zh ? "隧道管理" : "Tunnels", exact: true }).click();
    const up = zh ? "发往目标 (入口 → 目标服务)" : "To destination (Entry → Destination service)";
    const down = zh ? "目标返回 (目标服务 → 入口)" : "From destination (Destination service → Entry)";
    const cards = page.locator(".tunnel-card");
    await cards.first().waitFor();
    assert.equal(await cards.count(), 3);
    for (const card of await cards.all()) {
      await card.getByText(up + ": 5.00 KiB", { exact: true }).waitFor();
      await card.getByText(down + ": 10.0 KiB", { exact: true }).waitFor();
      assert.equal(await card.locator(".tunnel-card-metrics > div").first().locator("strong").textContent(), "15.0 KiB");
    }
    await cards.first().getByRole("button", { name: zh ? "统计" : "Statistics", exact: true }).click();
    const dialog = page.getByRole("dialog");
    assert.equal(await dialog.locator(".tunnel-traffic-help").count(), 0);
    await dialog.locator(".tunnel-chart-legend").getByText(up, { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart-legend").getByText(down, { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart svg").hover();
    await dialog.locator(".tunnel-chart-readout").getByText(up + ": 5.00 KiB", { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart-readout").getByText(down + ": 10.0 KiB", { exact: true }).waitFor();
    const tableRows = dialog.locator(".tunnel-statistics-table .ant-table-tbody tr.ant-table-row");
    await tableRows.first().getByRole("button", { name: "2001:db8::1", exact: true }).waitFor();
    await dialog.getByRole("combobox", { name: zh ? "排序" : "Sort by", exact: true }).click();
    await page.locator(".ant-select-dropdown:visible").getByText(zh ? "连接数从高到低" : "Connections: high to low", { exact: true }).click();
    await tableRows.first().getByRole("button", { name: "192.0.2.1", exact: true }).waitFor();
    for (const [name, first] of [[zh ? "连接数从低到高" : "Connections: low to high", "2001:db8::1"], [zh ? "流量从低到高" : "Traffic: low to high", "192.0.2.1"]]) {
      await dialog.getByRole("combobox", { name: zh ? "排序" : "Sort by", exact: true }).click();
      await page.locator(".ant-select-dropdown:visible").getByText(name, { exact: true }).click();
      await tableRows.first().getByRole("button", { name: first, exact: true }).waitFor();
    }
    await tableRows.first().getByRole("button", { name: "192.0.2.1", exact: true }).click();
    await dialog.locator(".tunnel-chart-summary strong").first().getByText("3.00 KiB", { exact: true }).waitFor();
    assert.equal(await tableRows.count(), 1);
    assert.equal(requests.at(-1).get("source_ip"), "192.0.2.1");
    await dialog.getByLabel(zh ? "来源 IP" : "Source IP", { exact: true }).fill("2001:db8::1");
    await dialog.getByRole("button", { name: zh ? "筛选" : "Filter", exact: true }).click();
    await dialog.locator(".tunnel-chart-summary strong").first().getByText("12.0 KiB", { exact: true }).waitFor();
    await dialog.getByLabel(zh ? "来源 IP" : "Source IP", { exact: true }).fill("192.0.2.99");
    await dialog.getByRole("button", { name: zh ? "筛选" : "Filter", exact: true }).click();
    await dialog.locator(".tunnel-chart-summary strong").first().getByText("0 B", { exact: true }).waitFor();
    await dialog.getByText(zh ? "所选条件下暂无来源统计" : "No source statistics for this selection", { exact: true }).waitFor();
    await dialog.getByRole("button", { name: zh ? "全部 IP" : "All IPs", exact: true }).click();
    await dialog.locator(".tunnel-chart-summary strong").first().getByText("15.0 KiB", { exact: true }).waitFor();
    await dialog.getByRole("combobox", { name: zh ? "时间范围" : "Time range", exact: true }).click();
    await page.locator(".ant-select-dropdown:visible").getByText(zh ? "自定义时间" : "Custom range", { exact: true }).click();
    const datetime = (seconds) => {
      const date = new Date(seconds * 1000);
      return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
    };
    await dialog.getByLabel(zh ? "开始时间" : "Start time", { exact: true }).fill(datetime(traffic.bucket_start - 3600));
    await dialog.getByLabel(zh ? "结束时间" : "End time", { exact: true }).fill(datetime(traffic.bucket_start + 300));
    const from = traffic.bucket_start - 3600, to = traffic.bucket_start + 300;
    const customRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname.endsWith("/traffic") && Number(url.searchParams.get("from")) === from && Number(url.searchParams.get("to")) === to;
    });
    await dialog.getByRole("button", { name: zh ? "应用时间范围" : "Apply range", exact: true }).click();
    await customRequest;
    await dialog.locator(".tunnel-chart-summary strong").first().getByText("15.0 KiB", { exact: true }).waitFor();
    await dialog.getByLabel(zh ? "开始时间" : "Start time", { exact: true }).fill(datetime(traffic.bucket_start - 32 * 86400));
    assert.equal(await dialog.getByRole("button", { name: zh ? "应用时间范围" : "Apply range", exact: true }).isDisabled(), true);
    await dialog.getByLabel(zh ? "开始时间" : "Start time", { exact: true }).fill(datetime(from));
    for (const [name, total] of [[zh ? "服务器中转" : "Server relay", "3.00 KiB"], [zh ? "直连" : "Direct", "12.0 KiB"]]) {
      await dialog.getByRole("combobox", { name: zh ? "传输路径" : "Transport path", exact: true }).click();
      await page.locator(".ant-select-dropdown:visible").getByText(name, { exact: true }).click();
      await dialog.locator(".tunnel-chart-summary strong").first().getByText(total, { exact: true }).waitFor();
    }
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      for (const element of [dialog, dialog.locator(".tunnel-chart-legend")]) {
        assert.ok(await element.evaluate((el) => el.scrollWidth <= el.clientWidth + 1), "Traffic labels and explanations must wrap without horizontal overflow");
      }
      await dialog.locator(".tunnel-chart-footer").scrollIntoViewIfNeeded();
      assert.ok(await dialog.locator(".tunnel-chart-footer").isVisible(), "Statistics footer must remain reachable on all viewport sizes");
      await dialog.locator(".tunnel-chart-toolbar").scrollIntoViewIfNeeded();
      if (process.env.GOSSHD_UI_E2E_SCREENSHOT_DIR) {
        await mkdir(process.env.GOSSHD_UI_E2E_SCREENSHOT_DIR, { recursive: true });
        await page.screenshot({ path: join(process.env.GOSSHD_UI_E2E_SCREENSHOT_DIR, "tunnel-traffic-" + locale + "-" + width + ".png") });
      }
    }
    await dialog.getByRole("button", { name: zh ? "连接数" : "Connections", exact: true }).click();
    assert.equal(await dialog.locator(".tunnel-traffic-help").count(), 0);
    await dialog.locator(".tunnel-chart-legend").getByText(zh ? "峰值并发" : "Peak concurrency", { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart-tabs button").first().click();
    await dialog.locator(".tunnel-chart-legend").getByText(up, { exact: true }).waitFor();
    assert.deepEqual(errors, []);
    assert.ok(requests.every((params) => Number(params.get("to")) > Number(params.get("from"))), "Switching to custom ranges must never submit an empty range");
    await context.close();
  }
} finally { await browser.close(); }
