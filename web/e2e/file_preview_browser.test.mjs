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
    const errors = [], downloads = [], reads = [], sockets = [], textReads = [], writes = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("download", download => downloads.push(download));
    const names = ["photo.PNG", "song.wav", "manual.pdf", "broken.png", "slow.png", "large.png", "archive.bin", "notes.txt", "data.JSON", "readme.md"];
    const texts = { "notes.txt": "Hello text\n", "data.JSON": '{"enabled": true}\n', "readme.md": "# Readme\n\nHello Markdown\n" };
    const bodies = { ...Object.fromEntries(Object.entries(texts).map(([name, content]) => [name, Buffer.from(content)])), "photo.PNG": png, "song.wav": wav, "manual.pdf": Buffer.from(pdf), "broken.png": Buffer.from("not an image"), "slow.png": png, "archive.bin": Buffer.from("archive") };
    const entries = names.map(name => ({ name, path: `/preview/${"a-long-directory-name/".repeat(4)}${name}`, type: "file", size: name === "large.png" ? 256 * 1024 * 1024 + 1 : bodies[name].length, mode: "-rw-------" }));
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
    await page.route("**/api/targets/*/files/read?**", route => {
      const path = new URL(route.request().url()).searchParams.get("path");
      const name = path.split("/").at(-1);
      textReads.push(name);
      return route.fulfill({ json: { path, content: texts[name] } });
    });
    await page.route("**/api/targets/*/files/write", route => {
      const body = route.request().postDataJSON();
      writes.push(body);
      return route.fulfill({ json: { path: body.path, content: body.content } });
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
    const compactToolbar = async pane => {
      const view = pane.locator("..");
      const originalWidth = await view.evaluate(element => element.style.width);
      try {
        for (const width of [240, 160, 100]) {
          await view.evaluate((element, width) => { element.style.width = `${width}px`; }, width);
          await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
          const state = await pane.evaluate(element => {
            const pane = element.getBoundingClientRect();
            const header = element.querySelector(".editor-pane-head").getBoundingClientRect();
            const button = element.querySelector(".editor-pane-actions button:last-child");
            const rect = button.getBoundingClientRect();
            const title = element.querySelector(".dock-drag-handle > span > span");
            return {
              paneWidth: pane.width, headerWidth: header.width,
              closeInside: rect.left >= pane.left && rect.right <= pane.right + 1 && rect.top >= pane.top && rect.bottom <= pane.bottom,
              closeHit: button.contains(document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2)),
              titleClipped: title.scrollWidth > title.clientWidth,
              titleEllipsis: getComputedStyle(title).textOverflow === "ellipsis",
            };
          });
          assert.ok(state.headerWidth <= state.paneWidth + 1 && state.closeInside && state.closeHit, `Close button must stay inside and clickable at ${width}px: ${JSON.stringify(state)}`);
          assert.ok(state.titleClipped && state.titleEllipsis, "Long titles must truncate rather than displace actions");
        }
      } finally { await view.evaluate((element, width) => { element.style.width = width; }, originalWidth); }
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
    await compactToolbar(preview);
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
    assert.equal(await frame.getAttribute("title"), entries.find(entry => entry.name === "manual.pdf").path);
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
    for (const [name, content] of Object.entries(texts)) {
      await row(name).dblclick();
      const editor = page.locator(".dock-view-editor .editor-pane");
      await editor.locator(".monaco-editor textarea").waitFor();
      await page.waitForFunction(content => document.querySelector(".dock-view-editor .view-lines")?.textContent.replace(/\u00a0/g, " ").includes(content), content.split("\n")[0]);
      assert.equal(await editor.count(), 1);
      await compactToolbar(editor);
      if (name === "notes.txt") {
        await editor.locator(".monaco-editor .view-lines").click({ position: { x: 30, y: 10 } });
        await page.keyboard.press("Control+End");
        await page.keyboard.type("!");
        await page.waitForFunction(() => document.querySelector(".dock-view-editor .editor-pane-head")?.textContent.includes("* "));
        await compactToolbar(editor);
        await editor.getByRole("button", { name: locale === "en" ? "Save" : "保存", exact: true }).click();
        await page.waitForFunction(() => !document.querySelector(".dock-view-editor .editor-pane-head")?.textContent.includes("* "));
        assert.equal(writes.at(-1).content, `${content}!`, "Resizing preserves the original buffer and the saved edit");
      }
      await editor.getByRole("button", { name: locale === "en" ? "Close" : "关闭", exact: true }).click();
      await editor.waitFor({ state: "detached" });
      assert.equal(downloads.length, 0, "Text activation must open an editor rather than download");
    }
    assert.deepEqual(textReads, Object.keys(texts));
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
