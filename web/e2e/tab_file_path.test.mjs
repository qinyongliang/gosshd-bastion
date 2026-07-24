import { createRequire } from "node:module";

const baseURL = mustEnv("GOSSHD_UI_E2E_BASE_URL");
const playwrightPath = mustEnv("PLAYWRIGHT_REQUIRE_PATH");
const browserExecutable = mustEnv("PLAYWRIGHT_CHROMIUM_EXECUTABLE");
const require = createRequire(import.meta.url);
const { chromium } = require(playwrightPath);
const browser = await chromium.launch({ executablePath: browserExecutable, headless: true });

try {
  const context = await browser.newContext({ locale: "en-US", viewport: { width: 1800, height: 900 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("link", { name: /SSH services/ }).waitFor();

  const targets = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((response) => response.json());
    const ownerID = localStorage.getItem("gosshd_active_org") || me.organizations[0].id;
    const create = async (name, alias) => {
      const response = await fetch("/api/targets", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          owner_type: "organization",
          owner_id: ownerID,
          target_type: "direct",
          name,
          alias,
          host: "127.0.0.1",
          port: 22,
          remote_username: "root",
          auth_type: "password",
          secret: "root-pass",
        }),
      });
      if (!response.ok) throw new Error(await response.text());
      return (await response.json()).target;
    };
    return {
      first: await create("Path tab one", `path-one-${Date.now()}`),
      second: await create("Path tab two", `path-two-${Date.now()}`),
    };
  });

  await page.goto(`${baseURL}/targets/${targets.first.id}/connect`, { waitUntil: "domcontentloaded" });
  await setPath(page, "/var/log");
  await page.evaluate((targetID) => window.postMessage({ type: "gosshd-connect-open-target", targetID }, location.origin), targets.second.id);
  await page.waitForFunction((targetID) => location.pathname === `/targets/${targetID}/connect`, targets.second.id);
  await setPath(page, "/opt");

  await page.locator(".connection-tab").filter({ hasText: targets.first.alias }).locator(".connection-tab-main").click();
  await expectPath(page, "/var/log");
  await page.locator(".connection-tab").filter({ hasText: targets.second.alias }).locator(".connection-tab-main").click();
  await expectPath(page, "/opt");
  await context.close();
} finally {
  await browser.close();
}

async function setPath(page, path) {
  await page.locator(".file-manager-path").dblclick();
  const input = page.getByLabel("File path");
  await input.fill(path);
  await input.press("Enter");
  await expectPath(page, path);
}

async function expectPath(page, path) {
  await page.waitForFunction((expected) => document.querySelector(".file-manager-path")?.getAttribute("title") === expected, path);
}

function mustEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
