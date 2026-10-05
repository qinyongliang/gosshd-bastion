import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const playwright = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const engine = process.env.GOSSHD_UI_E2E_ENGINE || "chromium";
const browser = await playwright[engine].launch({ ...(engine === "chromium" ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE } : {}), headless: true });
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;

try {
  const context = await browser.newContext({ locale: "en-US", reducedMotion: "reduce", viewport: { width: 390, height: 844 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(baseURL, { waitUntil: "domcontentloaded" });
  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("button", { name: "Open menu", exact: true }).click();
  await page.getByRole("link", { name: /SSH services/ }).click();
  await page.locator(".sidebar-open").waitFor({ state: "detached" });
  const target = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((response) => response.json());
    const response = await fetch("/api/targets", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, target_type: "direct", name: "Mobile actions target", alias: "mobile-actions", host: "127.0.0.1", port: 22, remote_username: "root", auth_type: "password", secret: "test-pass" }),
    });
    if (!response.ok) throw new Error("Fixture target creation failed");
    return (await response.json()).target;
  });
  await page.getByRole("button", { name: "Open menu", exact: true }).click();
  await page.getByRole("link", { name: /Dashboard/ }).click();
  await page.getByRole("button", { name: "Open menu", exact: true }).click();
  await page.getByRole("link", { name: /SSH services/ }).click();
  const row = page.locator(".target-tree-row").filter({ hasText: target.alias });
  await row.waitFor();
  for (const width of [390, 768, 1024, 1440, 1800]) {
    await page.setViewportSize({ width, height: 844 });
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await selectAction(page.locator(".resource-actions"), "New folder");
    const folderDialog = page.getByRole("dialog", { name: "New folder", exact: true });
    await folderDialog.getByLabel("Folder name").fill(`Actions ${width}`);
    await folderDialog.getByRole("button", { name: "New folder", exact: true }).click();
    await folderDialog.waitFor({ state: "detached" });
    await page.getByText(`Actions ${width}`, { exact: true }).waitFor();
    await selectAction(row, "Edit");
    const drawer = page.getByRole("dialog", { name: new RegExp(target.name) });
    await drawer.getByLabel("Service name").waitFor();
    await drawer.getByRole("button", { name: "Close", exact: true }).click();
    await drawer.waitFor({ state: "detached" });
    await selectAction(page.locator(".resource-actions"), "Credentials");
    const credentials = page.getByRole("dialog", { name: "Credentials", exact: true });
    await credentials.getByLabel("Credential name").waitFor();
    await credentials.getByRole("button", { name: "Cancel", exact: true }).click();
    await credentials.waitFor({ state: "detached" });
  }
  assert.deepEqual(errors, []);
  await context.close();

  async function selectAction(container, name) {
    const inline = container.getByRole("button", { name, exact: true });
    if (await inline.isVisible()) {
      await inline.click();
    } else {
      await container.getByRole("button", { name: "More", exact: true }).click();
      await page.getByRole("menu").getByRole("button", { name, exact: true }).click();
    }
  }
} finally {
  await browser.close();
}
