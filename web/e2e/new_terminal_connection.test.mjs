import { createRequire } from "node:module";

const baseURL = mustEnv("GOSSHD_UI_E2E_BASE_URL");
const playwrightPath = mustEnv("PLAYWRIGHT_REQUIRE_PATH");
const browserExecutable = mustEnv("PLAYWRIGHT_CHROMIUM_EXECUTABLE");
const require = createRequire(import.meta.url);
const { chromium } = require(playwrightPath);
const browser = await chromium.launch({ executablePath: browserExecutable, headless: true });

try {
  const context = await browser.newContext({ locale: "en-US" });
  await context.addInitScript(() => {
    window.__terminalSocketURLs = [];
    window.WebSocket = class {
      static OPEN = 1;
      readyState = 0;
      constructor(url) {
        window.__terminalSocketURLs.push(String(url));
      }
      close() {}
      send() {}
    };
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("link", { name: /SSH services/ }).waitFor();

  const target = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((response) => response.json());
    const ownerID = localStorage.getItem("gosshd_active_org") || me.organizations[0].id;
    const response = await fetch("/api/targets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        owner_type: "organization",
        owner_id: ownerID,
        target_type: "direct",
        name: "Fresh terminal",
        alias: `fresh-${Date.now()}`,
        host: "127.0.0.1",
        port: 22,
        remote_username: "root",
        auth_type: "password",
        secret: "root-pass",
      }),
    });
    if (!response.ok) throw new Error(await response.text());
    return (await response.json()).target;
  });
  await page.evaluate((targetID) => localStorage.setItem(`gosshd-terminal-session:${targetID}`, "existing-session"), target.id);
  await page.goto(`${baseURL}/targets/${target.id}/connect?new=1`, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => window.__terminalSocketURLs?.length > 0);
  const socketURL = await page.evaluate(() => window.__terminalSocketURLs[0]);
  if (new URL(socketURL).searchParams.has("session_id")) throw new Error(`new terminal reused an existing session: ${socketURL}`);
  await context.close();
} finally {
  await browser.close();
}

function mustEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
