import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const root = fileURLToPath(new URL("../../", import.meta.url));
const fixture = `<!doctype html><html><head><link rel="stylesheet" href="/styles.css"></head><body><main style="max-width:720px;margin:auto;padding:16px"><div id="root"></div></main><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import {I18nProvider} from '/src/i18n.tsx';
import {AgentInstallGuide} from '/src/components/AgentInstallGuide.tsx';
const enrollment={server_url:'https://bastion.example.com',android_apk_url:'https://downloads.example.com/gosshd-agent-android.apk',token:'example-enrollment-token',install_sh:'example-run',service_sh:'example-service',install_cmd:'example-cmd',service_cmd:'example-cmd-service',install_ps1:'example-ps1',service_ps1:'example-ps1-service',install_pwsh:'example-pwsh',service_pwsh:'example-pwsh-service'};
createRoot(document.getElementById('root')).render(React.createElement(I18nProvider,null,React.createElement(AgentInstallGuide,{enrollment})));
</script></body></html>`;
const vite = await createServer({
  configFile: fileURLToPath(new URL("../../vite.config.ts", import.meta.url)),
  root: root + "web",
  logLevel: "error",
  server: { host: "127.0.0.1", port: 0, strictPort: false },
  plugins: [{ name: "install-guide-fixture", configureServer(server) {
    server.middlewares.use(async (request, response, next) => {
      if (request.url?.split("?")[0] !== "/__install_guide") return next();
      try {
        const html = await server.transformIndexHtml("/__install_guide", fixture);
        response.setHeader("Content-Type", "text/html");
        response.end(html);
      } catch (error) { next(error); }
    });
  }}],
});
let browser;
try {
  await vite.listen();
  const address = vite.httpServer.address();
  const base = 'http://127.0.0.1:' + address.port;
  browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
  for (const [userAgent, expected] of [
    ['Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Windows'],
    ['Mozilla/5.0 (X11; Linux x86_64)', 'Linux'],
    ['Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)', 'macOS'],
    ['Mozilla/5.0 (Linux; Android 14; Example)', 'Android (ARMv7 / ARM64)'],
    ['Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)', null],
  ]) {
    const context = await browser.newContext({ userAgent, viewport: { width: 390, height: 844 }, reducedMotion: "reduce" });
    await context.addInitScript(() => localStorage.setItem('gosshd_locale', 'en'));
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(base + '/__install_guide');
    await page.getByRole('button', { name: 'Windows', exact: true }).waitFor();
    const expanded = page.locator('.agent-install-platform>h3>button[aria-expanded=true]');
    await page.waitForFunction(count => document.querySelectorAll('.agent-install-platform>h3>button[aria-expanded=true]').length === count, expected ? 1 : 0);
    if (expected) assert.equal(await expanded.innerText(), expected);
    const android = page.getByRole('button', { name: 'Android (ARMv7 / ARM64)', exact: true });
    if (expected !== 'Android (ARMv7 / ARM64)') await android.click();
    assert.equal(await expanded.count(), 1);
    assert.equal(await expanded.innerText(), 'Android (ARMv7 / ARM64)');
    assert.equal(await page.locator('.agent-install-advanced').getAttribute('open'), null);
    await page.getByLabel('Enrollment token (enter in the app)', { exact: true }).waitFor();
    const linux = page.getByRole('button', { name: 'Linux', exact: true });
    await linux.click();
    assert.equal(await expanded.count(), 1);
    assert.equal(await expanded.innerText(), 'Linux');
    assert.equal(await page.getByLabel('Enrollment token (enter in the app)', { exact: true }).count(), 0);
    await linux.press('Enter');
    assert.equal(await expanded.count(), 0);
    await linux.press('Space');
    assert.equal(await expanded.count(), 1);
    await page.getByRole('button', { name: 'macOS', exact: true }).click();
    await page.getByText('launchd startup service', { exact: true }).waitFor();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.deepEqual(errors, []);
    await context.close();
  }
  console.log('Platform default, exclusive expansion, collapse, keyboard and mobile layout checks passed');
} finally {
  await browser?.close();
  await vite.close();
}
