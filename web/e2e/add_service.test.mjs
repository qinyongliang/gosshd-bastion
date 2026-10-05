import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const playwright = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const engine = process.env.GOSSHD_UI_E2E_ENGINE || "chromium";
const browser = await playwright[engine].launch({ ...(engine === "chromium" ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE } : {}), headless: true });
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;

try {
  const context = await browser.newContext({ locale: "en-US", reducedMotion: "reduce" });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  const errors = [];
  page.on("pageerror", (error) => { errors.push(error.message); console.error(error.stack); });
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
  await page.getByRole("link", { name: /SSH services/ }).click();
  for (const reducedMotion of ["reduce", "no-preference", "reduce"]) {
    await page.emulateMedia({ reducedMotion });
    for (const width of [1440, 900, 390]) {
      await page.setViewportSize({ width, height: 844 });
      await page.getByRole("button", { name: "Add service", exact: true }).click();
      await page.getByRole("dialog").waitFor({ state: "visible" });
      await page.waitForFunction(() => {
        const dialog = document.querySelector('[role="dialog"]');
        return dialog && getComputedStyle(dialog).opacity === "1";
      }, null, { timeout: 2500 });
      await page.getByLabel("Service name", { exact: true }).fill("Dialog regression");
      await page.getByLabel("Target alias", { exact: true }).fill("dialog-regression");
      await page.getByRole("button", { name: "Next", exact: true }).click();
      await page.getByLabel("Remote username", { exact: true }).fill("root");
      await page.getByRole("button", { name: "Cancel", exact: true }).click();
      await page.getByRole("dialog").waitFor({ state: "detached" });
    }
  }
  await page.getByRole("button", { name: "Add service", exact: true }).click();
  await page.getByLabel("Service name", { exact: true }).fill("Dialog regression");
  await page.getByLabel("Target alias", { exact: true }).fill("dialog-regression");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await page.getByLabel("Target host", { exact: true }).fill("127.0.0.1");
  await page.getByLabel("Remote username", { exact: true }).fill("root");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await page.getByLabel("Key or password", { exact: true }).fill("test-pass");
  await page.getByRole("dialog").getByRole("button", { name: "Add service", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "detached" });
  await page.getByText("dialog-regression", { exact: true }).waitFor();
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
