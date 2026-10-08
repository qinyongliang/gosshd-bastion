import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;
let phase = "empty";
const record = { id: "handoff-test", user_id: "test-user", target_id: "test-target", command: "echo realtime-handoff-test", public_key_name: "临时维护授权", policy_decision: "allow", policy_reason: "allowed", request_type: "exec", started_at: new Date().toISOString() };
try {
  const context = await browser.newContext({ locale: "en-US", reducedMotion: "reduce" });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  await page.route("**/api/audit/running?**", (route) => route.fulfill({ json: { logs: phase === "running" || phase === "overlap" ? [{ ...record, running: true }] : [] } }));
  await page.route("**/api/audit?**", async (route) => {
    const complete = phase === "finished" || phase === "overlap";
    if (complete) await new Promise((resolve) => setTimeout(resolve, 700));
    const logs = complete ? [{ ...record, running: false, ended_at: new Date().toISOString(), exit_code: 0 }] : [];
    await route.fulfill({ json: { logs, total: logs.length, page: 1, page_size: 20 } });
  });
  await page.goto(baseURL, { waitUntil: "domcontentloaded" });
  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "Audit", exact: true }).click();
  await page.getByText("No audit records yet", { exact: false }).count();
  phase = "running";
  const row = page.locator(".audit-page tbody tr").filter({ hasText: record.command });
  await row.waitFor();
  await row.getByText(record.public_key_name, { exact: true }).waitFor();
  await page.evaluate((command) => {
    window.__auditDisappeared = false;
    const panel = document.querySelector(".audit-page .panel");
    new MutationObserver(() => { if (!panel.textContent.includes(command)) window.__auditDisappeared = true; }).observe(panel, { childList: true, subtree: true });
  }, record.command);
  phase = "finished";
  await row.locator("td[data-column=exit]").getByText("0", { exact: true }).waitFor();
  assert.equal(await page.evaluate(() => window.__auditDisappeared), false, "Live record disappeared during handoff to history");
  assert.equal(await row.count(), 1);
  phase = "overlap";
  await page.waitForResponse((response) => new URL(response.url()).pathname === "/api/audit/running");
  assert.equal(await row.count(), 1, "Persisted and running versions were duplicated");
  await context.close();
} finally { await browser.close(); }
