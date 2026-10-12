import { watch } from "node:fs";
import { readdir } from "node:fs/promises";

// The Go SFTP fixture reports POSIX /C:/... realpaths on Windows. Keep this
// transport test focused on real ICE/relay/SSH traffic, not that baseline path bug.
export async function normalizeSFTPFixturePaths(page) {
  if (process.platform !== "win32") return;
  await page.route(/\/api\/targets\/[^/]+\/files\?/, async (route) => {
    const response = await route.fetch();
    if (!response.ok()) { await route.fulfill({ response }); return; }
    const data = await response.json();
    const normalize = (path) => path?.replace(/^\/([A-Za-z]:\/)/, "$1");
    data.path = normalize(data.path);
    for (const entry of data.entries || []) entry.path = normalize(entry.path);
    await route.fulfill({ response, json: data });
  });
}

// Transfer test bytes as one string instead of serializing millions of array
// elements through the browser protocol. No production download path is mocked.
export async function readStreamedDownload(page) {
  const base64 = await page.evaluate(() => {
    const parts = [];
    for (const chunk of window.downloadTest.chunks) {
      for (let offset = 0; offset < chunk.length; offset += 8192) parts.push(String.fromCharCode(...chunk.subarray(offset, offset + 8192)));
    }
    return btoa(parts.join(""));
  });
  return Buffer.from(base64, "base64");
}

// Observe Agent cleanup instead of polling with fixed sleeps.
export function waitForNoTemporaryUploads(dir) {
  return new Promise((resolve, reject) => {
    let done = false;
    const signal = AbortSignal.timeout(3000);
    const finish = (error, names) => {
      if (done) return;
      done = true; watcher.close(); signal.removeEventListener("abort", abort);
      if (error) reject(error); else resolve(names);
    };
    const check = async () => {
      if (done) return;
      try {
        const names = await readdir(dir);
        if (!names.some((name) => name.startsWith(".gosshd-upload-"))) finish(undefined, names);
      } catch (error) { finish(error); }
    };
    const watcher = watch(dir, () => { void check(); });
    const abort = () => finish(new Error("Agent upload cleanup timed out"));
    watcher.on("error", (error) => finish(error));
    signal.addEventListener("abort", abort, { once: true });
    void check();
  });
}
