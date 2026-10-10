import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const base = process.env.GOSSHD_UI_E2E_BASE_URL;
const target = process.env.GOSSHD_UPLOAD_TARGET;
const dir = process.env.GOSSHD_UPLOAD_DIR;
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
try {
  for (const [mode, locale] of [["direct", "en"], ["interrupt", "zh-CN"], ["relay", "en"], ["empty", "zh-CN"], ["cancel", "en"], ["legacy", "en"]]) {
    const zh = locale === "zh-CN";
    const context = await browser.newContext({ locale: zh ? "zh-CN" : "en-US", viewport: { width: 1440, height: 1000 } });
    await context.addInitScript(({ mode, locale }) => {
      localStorage.setItem("gosshd_locale", locale);
      window.uploadTest = { directBytes: 0, relayBytes: 0, interrupted: false };
      const wsSend = WebSocket.prototype.send;
      WebSocket.prototype.send = function(data) {
        if (this.url.includes("/files/upload/ws") && data instanceof Uint8Array && data[0] === 1) window.uploadTest.relayBytes += data.byteLength;
        return wsSend.call(this, data);
      };
      if (mode === "relay" || mode === "cancel") {
        window.RTCPeerConnection = undefined;
      } else {
        const original = RTCPeerConnection.prototype.createDataChannel;
        RTCPeerConnection.prototype.createDataChannel = function(...args) {
          const channel = original.apply(this, args);
          const send = channel.send.bind(channel);
          channel.send = (data) => {
            if (data instanceof Uint8Array && data[0] === 1) {
              window.uploadTest.directBytes += data.byteLength;
              if (mode === "interrupt" && window.uploadTest.directBytes > 128 * 1024) {
                window.uploadTest.interrupted = true;
                channel.close();
                throw new Error("Test closed the direct path");
              }
            }
            return send(data);
          };
          return channel;
        };
      }
    }, { mode, locale });
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    // The system probe is unrelated to uploading and need not perform network discovery.
    await page.route("**/api/targets/*/system**", (route) => route.fulfill({ json: { os: "linux", hostname: "upload-test", filesystems: [] } }));
    if (mode === "legacy") {
      await page.routeWebSocket("**/files/upload/ws?**", (ws) => { ws.send(JSON.stringify({ type: "unavailable" })); });
    }
    await page.goto(base);
    await page.getByLabel(zh ? "邮箱" : "Email", { exact: true }).fill("admin");
    await page.getByLabel(zh ? "密码" : "Password", { exact: true }).fill("admin-pass");
    await page.getByRole("button", { name: zh ? "登录" : "Sign in", exact: true }).click();
    await page.getByRole("link", { name: zh ? "SSH 服务" : "SSH services", exact: true }).waitFor();
    await page.goto(`${base}/targets/${target}/connect`);
    await page.locator(".file-manager-path").dblclick();
    const pathInput = page.getByLabel(zh ? "文件路径" : "File path", { exact: true });
    await pathInput.fill(dir);
    await pathInput.press("Enter");
    await page.waitForFunction((dir) => document.querySelector(".file-manager-path")?.getAttribute("title") === dir, dir);
    const content = mode === "empty" ? Buffer.alloc(0) : Buffer.alloc(mode === "cancel" ? 32 * 1024 * 1024 : 2 * 1024 * 1024);
    for (let i = 0; i < content.length; i++) content[i] = (i * 31 + 255) % 256;
    const name = `${mode}.bin`;
    const auditResponse = page.waitForResponse((response) => response.url().includes("/files?") && response.request().method() === "GET");
    await page.locator('input[type="file"]').setInputFiles({ name, mimeType: "application/octet-stream", buffer: content });
    if (mode === "cancel") {
      await page.getByRole("button", { name: "Cancel upload", exact: true }).click();
      await page.locator(".file-upload-toast.cancelled").waitFor();
      await new Promise((resolve) => setTimeout(resolve, 500));
      const names = await readdir(dir);
      assert(!names.includes(name));
      assert(!names.some((name) => name.startsWith(".gosshd-upload-")));
    } else {
      await page.locator(".file-upload-toast.success").waitFor();
      const stats = await page.evaluate(() => window.uploadTest);
      if (mode === "direct") {
        assert(stats.directBytes > 0, "browser did not send directly to Agent");
        assert.equal(stats.relayBytes, 0, "file content crossed the bastion despite direct connection");
        assert((await page.locator(".file-upload-toast").textContent()).includes("Direct"));
      }
      if (mode === "interrupt") {
        assert(stats.interrupted && stats.directBytes > 0 && stats.relayBytes > 0, "direct interruption did not resume over relay");
        assert((await page.locator(".file-upload-toast").textContent()).includes("中转"));
      }
      if (mode === "relay") assert(stats.relayBytes > 0 && stats.directBytes === 0);
      const result = await readFile(join(dir, name));
      assert.deepEqual(result, content, "target file is corrupted");
      // Audit is asynchronous at the end of the request; wait for its trusted receipt.
      await page.waitForFunction(async ({ target, name, checksum }) => {
        const response = await fetch(`/api/audit?target_id=${target}&request_type=sftp&limit=100`);
        const data = await response.json();
        return (data.logs || []).some((log) => log.command?.includes(name) && log.exit_code === 0 && (name.startsWith("legacy") || log.policy_reason?.includes(checksum)));
      }, { target, name, checksum: createHash("sha256").update(content).digest("hex") });
    }
    await auditResponse;
    assert.deepEqual(errors, []);
    await context.close();
    console.log(`file upload ${mode} (${locale}): passed`);
  }
} finally { await browser.close(); }
