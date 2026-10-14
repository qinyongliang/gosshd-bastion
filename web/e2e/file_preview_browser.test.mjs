import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a0YQAAAAASUVORK5CYII=", "base64");
const wav = Buffer.alloc(46);
wav.write("RIFF"); wav.writeUInt32LE(38, 4); wav.write("WAVEfmt ", 8); wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(8000, 24); wav.writeUInt32LE(16000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write("data", 36); wav.writeUInt32LE(2, 40);
let pdf = "%PDF-1.4\n";
const offsets = [0];
for (const [index, body] of ["<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>"].entries()) {
  offsets.push(Buffer.byteLength(pdf)); pdf += `${index + 1} 0 obj\n${body}\nendobj\n`;
}
const xref = Buffer.byteLength(pdf);
pdf += `xref\n0 4\n0000000000 65535 f \n${offsets.slice(1).map(offset => `${String(offset).padStart(10, "0")} 00000 n \n`).join("")}trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
try {
  for (const locale of ["en", "zh-CN"]) {
    const context = await browser.newContext({ locale: locale === "en" ? "en-US" : locale, acceptDownloads: true, viewport: { width: 1440, height: 1000 } });
    await context.addInitScript(locale => {
      localStorage.setItem("gosshd_locale", locale);
      window.showSaveFilePicker = undefined;
      window.previewURLs = { created: [], revoked: [] };
      const create = URL.createObjectURL.bind(URL), revoke = URL.revokeObjectURL.bind(URL);
      URL.createObjectURL = blob => { const url = create(blob); window.previewURLs.created.push(url); return url; };
      URL.revokeObjectURL = url => { window.previewURLs.revoked.push(url); revoke(url); };
    }, locale);
    const page = await context.newPage();
    page.setDefaultTimeout(15_000);
    const errors = [], downloads = [], reads = [], sockets = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("download", download => downloads.push(download));
    const names = ["photo.PNG", "song.wav", "manual.pdf", "broken.png", "slow.png", "large.png", "archive.bin"];
    const bodies = { "photo.PNG": png, "song.wav": wav, "manual.pdf": Buffer.from(pdf), "broken.png": Buffer.from("not an image"), "slow.png": png, "archive.bin": Buffer.from("archive") };
    const entries = names.map(name => ({ name, path: `/preview/${name}`, type: "file", size: name === "large.png" ? 256 * 1024 * 1024 + 1 : bodies[name].length, mode: "-rw-------" }));
    await page.route("**/api/targets/*/system**", route => route.fulfill({ json: { os: "linux", hostname: "preview.example", filesystems: [] } }));
    await page.route("**/api/targets/*/files?**", route => route.fulfill({ json: { path: "/preview", entries } }));
    await page.routeWebSocket("**/api/targets/*/files/download/ws?**", socket => {
      sockets.push(socket);
      if (!socket.url().includes("slow.png")) socket.send(JSON.stringify({ type: "unavailable" }));
    });
    await page.route("**/api/targets/*/files/download?**", route => {
      const name = new URL(route.request().url()).searchParams.get("path").split("/").at(-1);
      reads.push(name);
      return route.fulfill({ body: bodies[name], contentType: "application/octet-stream" });
    });
    const base = process.env.GOSSHD_UI_E2E_BASE_URL;
    await page.goto(base);
    await page.getByLabel(locale === "en" ? "Email" : "邮箱", { exact: true }).fill("admin");
    await page.getByLabel(locale === "en" ? "Password" : "密码", { exact: true }).fill("admin-pass");
    await page.getByRole("button", { name: locale === "en" ? "Sign in" : "登录", exact: true }).click();
    await page.getByRole("link", { name: locale === "en" ? "SSH services" : "SSH 服务", exact: true }).waitFor();
    const target = await page.evaluate(async locale => {
      const me = await fetch("/api/me").then(response => response.json());
      const response = await fetch("/api/targets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, name: "Preview test", alias: `preview-${locale}`, target_type: "direct", host: "127.0.0.1", port: 1, remote_username: "test", auth_type: "password", secret: "test" }) });
      return (await response.json()).target;
    }, locale);
    await page.goto(`${base}/targets/${target.id}/connect`);
    await page.locator(".files-zone").waitFor();
    if (await page.locator(".files-zone .collapsed-zone-button").count()) await page.locator(".files-zone .collapsed-zone-button").click();
    const row = name => page.locator(".file-manager-body").getByRole("button", { name, exact: true });
    const preview = page.locator(".file-preview-pane");
    const close = async () => {
      await preview.getByRole("button", { name: locale === "en" ? "Close" : "关闭", exact: true }).click();
      await preview.waitFor({ state: "detached" });
    };
    await row("photo.PNG").dblclick();
    await page.waitForFunction(() => document.querySelector(".file-preview-content img")?.naturalWidth === 1);
    assert.equal(await preview.locator('[data-transport="relay"]').count(), 1);
    const bounds = await preview.locator(".file-preview-content").boundingBox();
    const paneBounds = await preview.boundingBox();
    assert.ok(bounds.height > 100 && bounds.height < paneBounds.height, "Preview fills the pane below its header");
    await row("photo.PNG").dblclick();
    assert.equal(await preview.count(), 1, "Opening an existing preview should focus it");
    assert.equal(reads.filter(name => name === "photo.PNG").length, 1);
    const imageURL = await preview.locator("img").getAttribute("src");
    await close();
    assert.ok(await page.evaluate(url => window.previewURLs.revoked.includes(url), imageURL));
    assert.equal(await page.locator(".terminal-zone").count(), 1, "Closing preview must retain terminal");
    await row("song.wav").dblclick();
    await page.waitForFunction(() => document.querySelector(".file-preview-content audio")?.readyState >= 1);
    assert.equal(await preview.locator("audio[controls]").count(), 1);
    await close();
    await row("manual.pdf").dblclick();
    const frame = preview.locator("iframe");
    await frame.waitFor();
    assert.equal(await frame.getAttribute("title"), "/preview/manual.pdf");
    assert.equal(await frame.getAttribute("sandbox"), null, "Native PDF viewer must not be disabled by sandbox flags");
    await page.waitForFunction(() => document.querySelector(".file-preview-content iframe")?.contentDocument?.contentType === "application/pdf");
    await close();
    const pdfReads = reads.filter(name => name === "manual.pdf").length;
    await page.evaluate(() => Object.defineProperty(navigator, "pdfViewerEnabled", { configurable: true, value: false }));
    await row("manual.pdf").dblclick();
    await preview.getByRole("alert").waitFor();
    await close();
    assert.equal(reads.filter(name => name === "manual.pdf").length, pdfReads, "Disabled PDF viewers must not initiate reads or native downloads");
    await page.evaluate(() => delete navigator.pdfViewerEnabled);
    await row("broken.png").dblclick();
    await preview.getByRole("alert").waitFor();
    await close();
    await row("large.png").dblclick();
    await preview.getByRole("alert").waitFor();
    assert.ok((await preview.innerText()).includes("256 MiB"));
    assert.ok(!reads.includes("large.png"));
    await close();
    await row("slow.png").dblclick();
    await preview.getByRole("status").waitFor();
    const slow = sockets.at(-1);
    const closed = new Promise(resolve => slow.onClose(resolve));
    await close();
    await closed;
    assert.ok(!reads.includes("slow.png"));
    assert.equal(downloads.length, 0, "Preview must never invoke browser download");
    // The explicit context-menu action still downloads previewable files.
    await row("photo.PNG").click({ button: "right" });
    const download = page.waitForEvent("download");
    await page.locator(".file-context-menu").getByRole("menuitem", { name: locale === "en" ? "Download" : "下载", exact: true }).click();
    await download;
    const archiveDownload = page.waitForEvent("download");
    await row("archive.bin").dblclick();
    await archiveDownload;
    const urls = await page.evaluate(() => window.previewURLs);
    assert.ok(urls.created.slice(0, 4).every(url => urls.revoked.includes(url)), "Closing previews releases Blob URLs");
    assert.deepEqual(errors, []);
    await page.unrouteAll({ behavior: "wait" });
    await context.close();
    console.log(`file preview (${locale}): passed`);
  }
} finally { await browser.close(); }
