import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const traffic = { bucket_start: Math.floor(Date.now() / 300000) * 300, relay_up: 1024, relay_down: 2048, direct_up: 4096, direct_down: 8192, connections_opened: 3, peak_connections: 2 };
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
    await page.route("**/api/tunnels/*/traffic?**", (route) => route.fulfill({ json: { interval_seconds: 300, buckets: [traffic] } }));
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
    await cards.first().getByRole("button", { name: zh ? "流量与连接" : "Traffic & connections", exact: true }).click();
    const dialog = page.getByRole("dialog");
    assert.equal(await dialog.locator(".tunnel-traffic-help").count(), 0);
    await dialog.locator(".tunnel-chart-legend").getByText(up, { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart-legend").getByText(down, { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart svg").hover();
    await dialog.locator(".tunnel-chart-readout").getByText(up + ": 5.00 KiB", { exact: true }).waitFor();
    await dialog.locator(".tunnel-chart-readout").getByText(down + ": 10.0 KiB", { exact: true }).waitFor();
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
    await context.close();
  }
} finally { await browser.close(); }
