import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";

const require = createRequire(import.meta.url);
const playwrightPath = process.env.PLAYWRIGHT_REQUIRE_PATH;
const browserExecutable = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE;
if (!playwrightPath || !browserExecutable) {
  throw new Error("PLAYWRIGHT_REQUIRE_PATH and PLAYWRIGHT_CHROMIUM_EXECUTABLE are required");
}

const { chromium } = require(playwrightPath);
const root = resolve("design/mockups");
const outDir = resolve(root, "output");
await mkdir(outDir, { recursive: true });

const ids = [
  "app-auth",
  "app-dashboard",
  "app-organizations",
  "app-members",
  "app-keys",
  "app-targets",
  "app-agents",
  "app-policies",
  "app-audit",
  "app-system-admin",
  "site-home-en",
  "site-home-zh",
  "site-docs-en",
  "site-docs-zh",
];

const browser = await chromium.launch({ executablePath: browserExecutable, headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1024 }, deviceScaleFactor: 1 });
  await page.goto(`file:///${resolve(root, "index.html").replaceAll("\\", "/")}`, { waitUntil: "networkidle" });
  for (const id of ids) {
    const locator = page.locator(`#${id}`);
    await locator.screenshot({ path: resolve(outDir, `${id}.png`) });
  }
} finally {
  await browser.close();
}
