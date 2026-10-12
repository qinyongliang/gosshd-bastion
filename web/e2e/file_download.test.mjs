import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { normalizeSFTPFixturePaths, readStreamedDownload } from "./file_fixture.mjs";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const base = process.env.GOSSHD_UI_E2E_BASE_URL;
const target = process.env.GOSSHD_UPLOAD_TARGET;
const dir = process.env.GOSSHD_UPLOAD_DIR.replaceAll("\\", "/");
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
try {
  for (const [mode, locale] of [["direct", "en"], ["interrupt", "zh-CN"], ["relay", "en"], ["empty", "zh-CN"], ["cancel", "en"], ["legacy", "en"], ["stream", "en"], ["stream-cancel", "zh-CN"], ["corrupt", "en"], ["native", "en"], ["stream-error", "en"]]) {
    const zh = locale === "zh-CN";
    const streaming = mode.startsWith("stream") || ["direct", "interrupt"].includes(mode);
    const content = Buffer.alloc(mode === "empty" ? 0 : mode.includes("cancel") ? 32 * 1024 * 1024 : ["direct", "stream"].includes(mode) ? 8 * 1024 * 1024 : 2 * 1024 * 1024);
    for (let i = 0; i < content.length; i++) content[i] = (i * 31 + 255) % 256;
    const name = `download-${mode}.bin`;
    await writeFile(join(dir, name), content);
    const context = await browser.newContext({ acceptDownloads: true, locale: zh ? "zh-CN" : "en-US", viewport: { width: 1440, height: 1000 } });
    await context.addInitScript(({ mode, locale }) => {
      localStorage.setItem("gosshd_locale", locale);
      window.downloadTest = { directBytes: 0, relayBytes: 0, interrupted: false, chunks: [], saved: false, aborted: false, sockets: 0, offers: 0 };
      let markRelay, markDirect, cancelWrite;
      const firstRelay = new Promise((resolve) => { markRelay = resolve; });
      const directReady = new Promise((resolve) => { markDirect = resolve; });
      const cancelled = new Promise((resolve) => { cancelWrite = resolve; });
      const OriginalWS = WebSocket;
      window.WebSocket = class extends OriginalWS {
        constructor(...args) {
          super(...args);
          if (this.url.includes("/files/download/ws")) window.downloadTest.sockets++;
          if (this.url.includes("/files/download/ws")) this.addEventListener("message", (event) => {
            if (event.data instanceof ArrayBuffer && new Uint8Array(event.data)[0] === 1) {
              window.downloadTest.relayBytes += event.data.byteLength;
              markRelay();
              if (mode === "corrupt" && event.data.byteLength > 18) new Uint8Array(event.data)[18] ^= 1;
            }
          });
        }
      };
      const offer = RTCPeerConnection.prototype.createOffer;
      RTCPeerConnection.prototype.createOffer = async function(...args) {
        window.downloadTest.offers++;
        // Event-gated fixture forces relay startup, not an elapsed-time delay.
        if (["direct", "interrupt", "stream"].includes(mode)) await firstRelay;
        return offer.apply(this, args);
      };
      if (["relay", "cancel", "corrupt", "stream-cancel"].includes(mode)) {
        window.RTCPeerConnection = undefined;
      } else {
        const original = RTCPeerConnection.prototype.createDataChannel;
        RTCPeerConnection.prototype.createDataChannel = function(...args) {
          const channel = original.apply(this, args);
          channel.addEventListener("message", (event) => {
            if (event.data instanceof ArrayBuffer && new Uint8Array(event.data)[0] === 7) markDirect();
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
      if (mode.startsWith("stream") || ["direct", "interrupt"].includes(mode)) {
        window.showSaveFilePicker = async () => ({ createWritable: async () => ({
          write: async (bytes) => {
            if (mode === "stream-error") throw new Error("Test disk write failed");
            // Wait for observed transport/cancellation events only in the fixture.
            if (mode === "stream-cancel") await cancelled;
            else if (["direct", "interrupt", "stream"].includes(mode)) await directReady;
            window.downloadTest.chunks.push(bytes.slice());
          },
          close: async () => { window.downloadTest.saved = true; },
          abort: async () => { window.downloadTest.aborted = true; window.downloadTest.chunks = []; cancelWrite(); },
        }) });
      } else {
        window.showSaveFilePicker = undefined;
      }
    }, { mode, locale });
    const page = await context.newPage();
    await normalizeSFTPFixturePaths(page);
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
    await page.waitForFunction(() => { const path = document.querySelector(".file-manager-path")?.getAttribute("title"); return path && path !== "."; });
    await page.locator(".file-manager-path").dblclick();
    const pathInput = page.locator('.file-manager-path input[aria-label="File path"]');
    await pathInput.fill(dir);
    await pathInput.press("Enter");
    await page.waitForFunction((dir) => document.querySelector(".file-manager-path")?.getAttribute("title")?.replaceAll("\\", "/") === dir, dir).catch(async (error) => {
      const state = await page.evaluate(() => ({ path: document.querySelector(".file-manager-path")?.getAttribute("title"), error: document.querySelector(".file-operation-error")?.textContent }));
      throw new Error(`Directory fixture mismatch: ${JSON.stringify({ expected: dir, ...state })}`, { cause: error });
    });
    assert.equal(await page.locator(".file-breadcrumb-menu").count(), 0, "path editing left a menu over downloadable files");
    const downloadPromise = !mode.includes("cancel") && mode !== "corrupt" && !streaming ? page.waitForEvent("download") : null;
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
      const toast = page.locator(mode === "native" ? ".file-download-toast.browser" : ".file-download-toast.success");
      await toast.waitFor().catch(async (error) => {
        const state = await page.evaluate(() => ({ transport: { ...window.downloadTest, chunks: undefined }, writtenChunks: window.downloadTest.chunks.length, toast: document.querySelector(".file-download-toast")?.textContent, error: document.querySelector(".file-operation-error")?.textContent }));
        throw new Error(`Download failed in ${mode}: ${JSON.stringify(state)}`, { cause: error });
      });
      const toastText = await toast.textContent();
      if (downloadPromise) {
        const download = await downloadPromise;
        assert.equal(download.suggestedFilename(), name);
        assert.deepEqual(await readFile(await download.path()), content, "downloaded bytes differ");
      } else {
        await page.waitForFunction(() => window.downloadTest.saved);
        assert.deepEqual(await readStreamedDownload(page), content, "streamed bytes differ");
      }
      const stats = await page.evaluate(() => ({ ...window.downloadTest, chunks: [] }));
      if (mode === "direct" || mode === "stream") {
        assert(stats.directBytes > stats.relayBytes, "most large-file download data must bypass the bastion");
        assert(stats.relayBytes > 0, "download must start over relay before upgrading to direct");
        assert(toastText.includes("Direct"));
        console.log(`download first-file data frames: relay=${stats.relayBytes}B direct=${stats.directBytes}B`);
      }
      if (mode === "interrupt") {
        assert(stats.interrupted && stats.directBytes > 0 && stats.relayBytes > 0, "interruption did not continue on relay");
        assert(toastText.includes("中转"));
      }
      if (mode === "relay") assert(stats.relayBytes > 0 && stats.directBytes === 0);
      const checksum = createHash("sha256").update(content).digest("hex");
      await page.waitForFunction(async ({ target, name, checksum, legacy }) => {
        const response = await fetch(`/api/audit?target_id=${target}&request_type=sftp&limit=100`);
        const data = await response.json();
        return (data.logs || []).some((log) => log.command?.includes(name) && log.exit_code === 0 && (legacy || log.policy_reason?.includes(checksum)));
      }, { target, name, checksum, legacy: mode === "legacy" || mode === "native" });
    }
    if (["direct", "interrupt", "relay", "stream"].includes(mode)) {
      for (const expected of [content, Buffer.alloc(0)]) {
        await writeFile(join(dir, name), expected);
        await page.evaluate(() => { window.downloadTest.chunks = []; window.downloadTest.saved = false; });
        const saved = streaming ? null : page.waitForEvent("download");
        await page.locator(".file-manager-body").getByRole("button", { name, exact: true }).dblclick();
        await page.locator(".file-download-toast.success").waitFor();
        if (saved) { const file = await saved; assert.deepEqual(await readFile(await file.path()), expected); }
        else { await page.waitForFunction(() => window.downloadTest.saved); assert.deepEqual(await readStreamedDownload(page), expected); }
      }
      const stats = await page.evaluate(() => ({ sockets: window.downloadTest.sockets, offers: window.downloadTest.offers }));
      assert.equal(stats.sockets, 1, "downloads reopened WebSocket");
      assert.equal(stats.offers, mode === "relay" ? 0 : 1, "downloads repeated P2P negotiation");
      await writeFile(join(dir, name), content);
    }
    assert.deepEqual(await readFile(join(dir, name)), content, "source was changed by download");
    assert.deepEqual(errors, []);
    await page.unrouteAll({ behavior: "wait" });
    await context.close();
    console.log(`file download ${mode} (${locale}): passed`);
  }
} finally {
  for (const context of browser.contexts()) for (const page of context.pages()) await page.unrouteAll({ behavior: "ignoreErrors" });
  await browser.close();
}
