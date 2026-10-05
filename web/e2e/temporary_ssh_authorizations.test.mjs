import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const playwright = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const engine = process.env.GOSSHD_UI_E2E_ENGINE || "chromium";
const browser = await playwright[engine].launch({ ...(engine === "chromium" ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE } : {}), headless: true });
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;
const mobile = process.env.GOSSHD_UI_E2E_MOBILE_ONLY === "1";

try {
  const context = await browser.newContext({ locale: "en-US", reducedMotion: "reduce", viewport: { width: mobile ? 390 : 1440, height: 844 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  if (process.env.GOSSHD_UI_E2E_ASSETS_BASE_URL) {
    const assetsURL = process.env.GOSSHD_UI_E2E_ASSETS_BASE_URL;
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin === new URL(baseURL).origin && (url.pathname === "/" || url.pathname.startsWith("/assets/"))) {
        const response = await route.fetch({ url: `${assetsURL}${url.pathname}` });
        await route.fulfill({ response });
      } else {
        await route.continue();
      }
    });
  }
  await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("button", { name: "Sign in" }).waitFor({ state: "detached" });

  const target = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((response) => response.json());
    const response = await fetch("/api/targets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ owner_type: "organization", owner_id: localStorage.getItem("gosshd_active_org") || me.organizations[0].id, target_type: "direct", name: "Temporary SSH test", alias: `temporary-${Date.now()}`, host: "127.0.0.1", port: 22, remote_username: "root", auth_type: "password", secret: "test-pass" }),
    });
    if (!response.ok) throw new Error(await response.text());
    return (await response.json()).target;
  });
  if (mobile) await page.getByRole("button", { name: "Open menu", exact: true }).click();
  await page.getByRole("link", { name: /SSH services/ }).click();
  const row = page.locator(".target-tree-row").filter({ hasText: target.alias });
  await row.waitFor();
  const more = row.getByRole("button", { name: "More", exact: true });
  if (await more.isVisible()) await more.click();
  await page.getByRole("button", { name: "Temporary authorizations", exact: true }).click();
  const manager = page.getByRole("dialog", { name: `Temporary SSH authorizations · ${target.name}`, exact: true });
  await manager.waitFor();
  assert.equal(await manager.getByLabel("Lifetime (hours)").inputValue(), "24");
  await manager.getByText("No temporary SSH authorizations yet.", { exact: true }).waitFor();
  await manager.getByLabel("Label (optional)").fill("Contractor access");
  const createdResponse = page.waitForResponse((response) => response.url().endsWith(`/api/targets/${target.id}/temporary-authorizations`) && response.request().method() === "POST");
  await manager.getByRole("button", { name: "Create authorization", exact: true }).click();
  const createdHTTP = await createdResponse;
  assert.equal(createdHTTP.status(), 201);
  const created = (await createdHTTP.json()).authorization;
  assert.match(created.token, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
  assert.equal(JSON.parse(createdHTTP.request().postData()).duration_seconds, 86400);
  const card = manager.locator(".temporary-ssh-card").filter({ hasText: "Contractor access" });
  await card.getByText(created.token, { exact: true }).waitFor();
  await card.getByText("Active", { exact: true }).waitFor();
  const command = `ssh -p 22022 ${created.token}@127.0.0.1`;
  assert.equal(await card.getByRole("button", { name: "Copy UUID", exact: true }).getAttribute("data-value"), created.token);
  const copyCommand = card.getByRole("button", { name: "Copy SSH command", exact: true });
  assert.equal(await copyCommand.getAttribute("data-value"), command);
  assert.equal(await card.locator("time").getAttribute("datetime"), created.expires_at);
  const layout = await manager.evaluate((dialog) => ({ width: dialog.getBoundingClientRect().width, viewport: innerWidth, commands: [...dialog.querySelectorAll(".command-box code")].every((code) => getComputedStyle(code).whiteSpace === "nowrap") }));
  assert.ok(layout.width <= layout.viewport);
  assert.ok(layout.commands);

  if (engine === "chromium") {
    await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: baseURL });
    await copyCommand.click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), command);
  }

  await card.getByLabel("Extend by (hours)").fill("0.5");
  const renewedResponse = page.waitForResponse((response) => response.url().endsWith(`/temporary-authorizations/${created.id}/renew`) && response.request().method() === "POST");
  await card.getByRole("button", { name: "Renew", exact: true }).click();
  const renewedHTTP = await renewedResponse;
  assert.equal(renewedHTTP.status(), 200);
  const renewed = (await renewedHTTP.json()).authorization;
  assert.equal(renewed.token, created.token);
  assert.equal(Date.parse(renewed.expires_at) - Date.parse(created.expires_at), 1800_000);
  await page.waitForFunction(({ id, expires }) => document.querySelector(`[data-authorization-id="${id}"] time`)?.getAttribute("datetime") === expires, { id: created.id, expires: renewed.expires_at });

  await card.getByRole("button", { name: "Delete", exact: true }).click();
  const confirmation = page.getByRole("dialog", { name: "Delete SSH authorization", exact: true });
  await confirmation.waitFor();
  const deletedResponse = page.waitForResponse((response) => response.url().endsWith(`/temporary-authorizations/${created.id}`) && response.request().method() === "DELETE");
  await confirmation.getByRole("button", { name: "Delete", exact: true }).click();
  assert.equal((await deletedResponse).status(), 204);
  await card.waitFor({ state: "detached" });
  await manager.getByText("No temporary SSH authorizations yet.", { exact: true }).waitFor();
  assert.deepEqual(errors, []);
  await context.close();
} finally {
  await browser.close();
}
