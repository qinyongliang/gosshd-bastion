import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;
try {
  for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
    let finished = false;
    let stops = 0;
    const record = {
      id: `live-${viewport.width}`, user_id: "test-user", target_id: "test-target",
      target_name: "Long running server command", command: "sleep 60",
      policy_decision: "allow", policy_reason: "allowed", request_type: "exec",
      started_at: new Date(Date.now() - 3000).toISOString(),
    };
    const currentLog = () => ({ ...record, running: !finished, ...(finished ? { ended_at: new Date(new Date(record.started_at).getTime() + 4000).toISOString(), exit_code: 137 } : {}) });
    const context = await browser.newContext({ locale: "en-US", viewport, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(10_000);
    await page.route("**/api/audit/running?**", (route) => route.fulfill({ json: { logs: finished ? [] : [currentLog()] } }));
    await page.route("**/api/audit?**", (route) => route.fulfill({ json: { logs: finished ? [currentLog()] : [], total: finished ? 1 : 0, page: 1, page_size: 20 } }));
    await page.route(`**/api/audit-live/${record.id}`, (route) => route.fulfill({ json: { log: currentLog(), ...(!finished ? { output: "captured before stop" } : {}) } }));
    await page.route(`**/api/audit-live/${record.id}/stop`, async (route) => {
      assert.equal(route.request().method(), "POST");
      stops++;
      await new Promise((resolve) => setTimeout(resolve, 300));
      finished = true;
      await route.fulfill({ status: 202, json: { ok: true } });
    });
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    await page.getByLabel("Email").fill("admin");
    await page.getByLabel("Password").fill("admin-pass");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await page.getByRole("button", { name: "Sign in", exact: true }).waitFor({ state: "detached" });
    await page.goto(`${baseURL}/audit`, { waitUntil: "domcontentloaded" });
    const row = page.locator(".audit-page tbody tr").filter({ hasText: record.command });
    await row.getByRole("button", { name: "Live output", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByText("captured before stop", { exact: true }).waitFor();
    assert.match(await dialog.locator(".ant-modal-title").innerText(), /Live output · Duration: \d/);
    assert.equal(await dialog.locator(".terminal-meta").getByText("Duration", { exact: true }).count(), 0);
    const metaBox = await dialog.locator(".running-output-meta").boundingBox();
    const tagBox = await dialog.locator(".running-output-status").boundingBox();
    assert.ok(Math.abs(metaBox.x + metaBox.width - tagBox.x - tagBox.width) < 2, "Live tag is not aligned to the right edge");
    await dialog.getByRole("button", { name: "Stop command", exact: true }).click();
    await dialog.getByRole("button", { name: "Stopping…", exact: true }).waitFor();
    assert.equal(await dialog.getByRole("button", { name: "Stopping…", exact: true }).isDisabled(), true);
    await dialog.getByRole("status").getByText("● Finished", { exact: true }).waitFor();
    assert.equal(stops, 1);
    assert.equal(await dialog.getByRole("button", { name: "Stop command", exact: true }).isDisabled(), true);
    assert.equal(await dialog.locator(".running-output-content").innerText(), "captured before stop");
    assert.match(await dialog.locator(".ant-modal-title").innerText(), /Duration: 4s$/);
    assert.equal(await dialog.getByRole("alert").count(), 0);
    await context.close();
  }
} finally { await browser.close(); }
