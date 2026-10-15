import assert from "node:assert/strict";
import test from "node:test";
import { filePreviewType, FilePreviewError, MAX_PREVIEW_SIZE, readFilePreview } from "../src/filePreview.ts";
import { closeFileTransfers } from "../src/fileTransfer.ts";

import { FALLBACK_EDITOR_LANGUAGE, inferEditorLanguage, isTextFile } from "../src/fileEditor.ts";

const flush = () => new Promise(resolve => setImmediate(resolve));

test("preview allowlist handles case without embedding active documents", () => {
  for (const [name, kind, mime] of [["photo.PNG", "image", "image/png"], ["photo.JPEG", "image", "image/jpeg"], ["image.svg", "image", "image/svg+xml"], ["movie.mp4", "video", "video/mp4"], ["song.mp3", "audio", "audio/mpeg"], ["manual.pdf", "pdf", "application/pdf"]]) {
    assert.deepEqual(filePreviewType(name), { kind, mime });
  }
  for (const name of ["page.html", "page.xhtml", "app.js", "data.xml", "archive.zip", "noextension", "file.constructor", "file.__proto__", "file.png.exe"]) assert.equal(filePreviewType(name), undefined);
});

test("preview rejects unsupported, oversized and already cancelled files before opening a channel", async () => {
  await assert.rejects(readFilePreview("target", "/file.html", 10), error => error instanceof FilePreviewError && error.code === "unsupported");
  for (const size of [-1, NaN, Infinity, MAX_PREVIEW_SIZE + 1]) await assert.rejects(readFilePreview("target", "/file.png", size), error => error instanceof FilePreviewError && error.code === "too-large");
  await assert.rejects(readFilePreview("target", "/file.png", 10, undefined, AbortSignal.abort()), { name: "AbortError" });
});

async function fallbackFixture(t, response) {
  const saved = Object.getOwnPropertyDescriptors(globalThis);
  let socket;
  const requests = [];
  class FakeSocket {
    static OPEN = 1;
    readyState = 1;
    bufferedAmount = 0;
    constructor() { socket = this; }
    send() {}
    close() { this.readyState = 3; }
  }
  Object.defineProperties(globalThis, {
    WebSocket: { configurable: true, value: FakeSocket },
    location: { configurable: true, value: new URL("https://console.example/") },
    fetch: { configurable: true, value: async (url, options) => { requests.push({ url, options }); return response; } },
  });
  t.after(() => {
    closeFileTransfers(t.name);
    for (const key of ["WebSocket", "location", "fetch"]) {
      if (saved[key]) Object.defineProperty(globalThis, key, saved[key]);
      else delete globalThis[key];
    }
  });
  return {
    requests,
    async read(size, progress, signal, ready, path = "/example/a & b.png") {
      const operation = readFilePreview(t.name, path, size, progress, signal);
      operation.catch(() => {});
      await flush();
      socket.onmessage({ data: JSON.stringify(ready || { type: "unavailable" }) });
      return { operation };
    },
  };
}

test("preview falls back to bounded authenticated HTTP without invoking any browser save API", async (t) => {
  const f = await fallbackFixture(t, new Response("image bytes", { headers: { "Content-Length": "11" } }));
  const progress = [];
  const { operation } = await f.read(11, value => progress.push(value));
  const blob = await operation;
  assert.equal(blob.type, "image/png");
  assert.equal(await blob.text(), "image bytes");
  assert.equal(f.requests.length, 1);
  assert.equal(new URL(f.requests[0].url, "https://console.example/").searchParams.get("path"), "/example/a & b.png");
  assert.equal(f.requests[0].options.credentials, "same-origin");
  assert.deepEqual(progress.at(-1), { loaded: 11, total: 11, transport: "relay" });
});

test("preview rejects a stale listing when channel metadata exceeds its buffer limit", async (t) => {
  const f = await fallbackFixture(t, null);
  const { operation } = await f.read(10, undefined, undefined, { type: "ready", size: MAX_PREVIEW_SIZE + 1, path: "/example/a & b.png" });
  await assert.rejects(operation, error => error instanceof FilePreviewError && error.code === "too-large");
  assert.equal(f.requests.length, 0);
});

test("preview HTTP fallback rejects overlarge, truncated and failed responses", async (t) => {
  for (const response of [new Response("x", { headers: { "Content-Length": String(MAX_PREVIEW_SIZE + 1) } }), new Response("x", { headers: { "Content-Length": "2" } }), Response.json({ error: "Denied" }, { status: 403 })]) {
    await t.test(String(response.headers.get("Content-Length") || response.status), async t => {
      const f = await fallbackFixture(t, response);
      const { operation } = await f.read(1);
      await assert.rejects(operation);
    });
  }
});

test("preview HTTP fallback propagates cancellation to fetch", async (t) => {
  const controller = new AbortController();
  const f = await fallbackFixture(t, new Response("x"));
  const { operation } = await f.read(1, () => { if (f.requests.length) controller.abort(); }, controller.signal);
  await assert.rejects(operation, { name: "AbortError" });
  assert.equal(f.requests[0].options.signal, controller.signal);
});


test("preview refuses active HTML renamed to PDF", async (t) => {
  const f = await fallbackFixture(t, new Response("<script>alert(1)</script>"));
  const { operation } = await f.read(25, undefined, undefined, undefined, "/example/fake.pdf");
  await assert.rejects(operation, error => error instanceof FilePreviewError && error.code === "invalid");
});


test("text activation recognizes documents, configuration and code without guessing binary contents", () => {
  for (const name of ["notes.txt", "notes.TXT", "data.JSON", "settings.jsonc", "README.md", "guide.markdown", "app.yaml", "app.yml", "app.conf", ".env", ".env.production", "Dockerfile", "Makefile", "README", "LICENSE", ".gitignore", ".editorconfig", "index.html", "main.ts", "script.py", "events.log", "table.csv"]) assert.ok(isTextFile(name), name);
  for (const name of ["archive.bin", "archive.zip", "video.mp4", "photo.png", "manual.pdf", "notes.txt.exe", "unknown", "/etc/nginx/data.bin", "file.constructor"]) assert.equal(isTextFile(name), false, name);
});

test("text activation shares editor language inference while unknown files keep the fallback", () => {
  for (const [name, language] of [["notes.txt", "plaintext"], ["data.JSON", "json"], ["README.md", "markdown"], ["app.yaml", "yaml"], ["/etc/nginx/sites-enabled/example.conf", "nginx"], ["Dockerfile", "dockerfile"], ["main.ts", "typescript"], ["unknown", FALLBACK_EDITOR_LANGUAGE]]) assert.equal(inferEditorLanguage(name), language, name);
});
