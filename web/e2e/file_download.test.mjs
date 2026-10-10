import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const base = process.env.GOSSHD_UI_E2E_BASE_URL;
const target = process.env.GOSSHD_UPLOAD_TARGET;
const dir = process.env.GOSSHD_UPLOAD_DIR;
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
try {
  for (const [mode, locale] of [["direct", "en"], ["interrupt", "zh-CN"], ["relay", "en"], ["empty", "zh-CN"], ["cancel", "en"], ["legacy", "en"], ["stream", "en"], ["stream-cancel", "zh-CN"], ["corrupt", "en"], ["native", "en"], ["stream-error", "en"]]) {
    const zh = locale === "zh-CN";
    const content = Buffer.alloc(mode === "empty" ? 0 : mode.includes("cancel") ? 32 * 1024 * 1024 : 2 * 1024 * 1024);
    for (let i = 0; i < content.length; i++) content[i] = (i * 31 + 255) % 256;
    const name = `download-${mode}.bin`;
    await writeFile(join(dir, name), content);
    const context = await browser.newContext({ acceptDownloads: true, locale: zh ? "zh-CN" : "en-US", viewport: { width: 1440, height: 1000 } });
    await context.addInitScript(({ mode, locale }) => {
      localStorage.setItem("gosshd_locale", locale);
      window.downloadTest = { directBytes: 0, relayBytes: 0, interrupted: false, chunks: [], saved: false, aborted: false };
      const OriginalWS = WebSocket;
      window.WebSocket = class extends OriginalWS {
        constructor(...args) {
          super(...args);
          if (this.url.includes("/files/download/ws")) this.addEventListener("message", (event) => {
            if (event.data instanceof ArrayBuffer && new Uint8Array(event.data)[0] === 1) {
              window.downloadTest.relayBytes += event.data.byteLength;
              if (mode === "corrupt" && event.data.byteLength > 18) new Uint8Array(event.data)[18] ^= 1;
            }
          });
        }
      };
      if (["relay", "cancel", "corrupt", "stream-cancel"].includes(mode)) {
        window.RTCPeerConnection = undefined;
      } else {
        const original = RTCPeerConnection.prototype.createDataChannel;
        RTCPeerConnection.prototype.createDataChannel = function(...args) {
          const channel = original.apply(this, args);
          channel.addEventListener("message", (event) => {
            if (event.data instanceof ArrayBuffer && new Uint8Array(event.data)[0] === 1) {
              window.downloadTest.directBytes += event.data.byteLength;
              if (mode === "interrupt" && window.downloadTest.directBytes > 128 * 1024 && !window.downloadTest.interrupted) {
                window.downloadTest.interrupted = true;
                channel.close();
              }
            }
          });
          return channel;
        };
      }
      if (mode.startsWith("stream")) {
        window.showSaveFilePicker = async () => ({ createWritable: async () => ({
          write: async (bytes) => {
            if (mode === "stream-error") throw new Error("Test disk write failed");
            // Slow writes exercise ACK backpressure and cancellation of the sink.
            await new Promise((resolve) => setTimeout(resolve, 2));
            window.downloadTest.chunks.push(Array.from(bytes));
          },
          close: async () => { window.downloadTest.saved = true; },
          abort: async () => { window.downloadTest.aborted = true; window.downloadTest.chunks = []; },
        }) });
      } else {
        window.showSaveFilePicker = undefined;
      }
    }, { mode, locale });
    const page = await context.newPage();
    page.setDefaultTimeout(25_000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/api/targets/*/system**", (route) => route.fulfill({ json: { os: "linux", hostname: "download-test", filesystems: [] } }));
    if (mode === "native") await page.routeWebSocket("**/files/download/ws?**", (ws) => { ws.send(JSON.stringify({ type: "ready", path: name, size: 256 * 1024 * 1024 + 1, stun_servers: [] })); });
    if (mode === "legacy") await page.routeWebSocket("**/files/download/ws?**", (ws) => { ws.send(JSON.stringify({ type: "unavailable" })); });
    await page.goto(base);
    await page.getByLabel(zh ? "邮箱" : "Email", { exact: true }).fill("admin");
    await page.getByLabel(zh ? "密码" : "Password", { exact: true }).fill("admin-pass");
    await page.getByRole("button", { name: zh ? "登录" : "Sign in", exact: true }).click();
    await page.getByRole("link", { name: zh ? "SSH 服务" : "SSH services", exact: true }).waitFor();
    await page.goto(`${base}/targets/${target}/connect`);
    await page.locator(".files-zone").waitFor();
    if (await page.locator(".files-zone .collapsed-zone-button").count()) await page.locator(".files-zone .collapsed-zone-button").click();
    await page.locator(".file-manager-path").dblclick();
    await page.getByLabel("File path", { exact: true }).fill(dir);
    await page.getByLabel("File path", { exact: true }).press("Enter");
    await page.waitForFunction((dir) => document.querySelector(".file-manager-path")?.getAttribute("title") === dir, dir);
    const downloadPromise = !mode.includes("cancel") && mode !== "corrupt" && !mode.startsWith("stream") ? page.waitForEvent("download") : null;
    await page.locator(".file-manager-body").getByRole("button", { name, exact: true }).dblclick();
    if (mode.includes("cancel")) {
      await page.getByRole("button", { name: zh ? "取消下载" : "Cancel download", exact: true }).click();
      await page.locator(".file-download-toast.cancelled").waitFor();
      if (mode === "stream-cancel") await page.waitForFunction(() => window.downloadTest.aborted && !window.downloadTest.saved);
    } else if (mode === "stream-error") {
      await page.locator(".file-download-toast.error").waitFor();
      await page.waitForFunction(() => window.downloadTest.aborted && !window.downloadTest.saved);
      assert((await page.locator(".file-operation-error").textContent()).includes("disk write failed"));
    } else if (mode === "corrupt") {
      await page.locator(".file-download-toast.error").waitFor();
      assert((await page.locator(".file-operation-error").textContent()).includes("checksum"));
    } else {
      if (downloadPromise) {
        const download = await downloadPromise;
        assert.equal(download.suggestedFilename(), name);
        assert.deepEqual(await readFile(await download.path()), content, "downloaded bytes differ");
      } else {
        await page.waitForFunction(() => window.downloadTest.saved);
        const chunks = await page.evaluate(() => window.downloadTest.chunks);
        assert.deepEqual(Buffer.concat(chunks.map((chunk) => Buffer.from(chunk))), content, "streamed bytes differ");
      }
      await page.locator(mode === "native" ? ".file-download-toast.browser" : ".file-download-toast.success").waitFor();
      const stats = await page.evaluate(() => ({ ...window.downloadTest, chunks: [] }));
      if (mode === "direct" || mode === "stream") {
        assert(stats.directBytes > 0, "Agent did not send directly to browser");
        assert.equal(stats.relayBytes, 0, "download content crossed the bastion on direct path");
        assert((await page.locator(".file-download-toast").textContent()).includes("Direct"));
      }
      if (mode === "interrupt") {
        assert(stats.interrupted && stats.directBytes > 0 && stats.relayBytes > 0, "interruption did not continue on relay");
        assert((await page.locator(".file-download-toast").textContent()).includes("中转"));
      }
      if (mode === "relay") assert(stats.relayBytes > 0 && stats.directBytes === 0);
      const checksum = createHash("sha256").update(content).digest("hex");
      await page.waitForFunction(async ({ target, name, checksum, legacy }) => {
        const response = await fetch(`/api/audit?target_id=${target}&request_type=sftp&limit=100`);
        const data = await response.json();
        return (data.logs || []).some((log) => log.command?.includes(name) && log.exit_code === 0 && (legacy || log.policy_reason?.includes(checksum)));
      }, { target, name, checksum, legacy: mode === "legacy" || mode === "native" });
    }
    assert.deepEqual(await readFile(join(dir, name)), content, "source was changed by download");
    assert.deepEqual(errors, []);
    await context.close();
    console.log(`file download ${mode} (${locale}): passed`);
  }
} finally { await browser.close(); }
