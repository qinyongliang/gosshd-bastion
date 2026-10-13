import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { watch } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { normalizeRemotePath } from "../src/remotePath.ts";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const base = process.env.GOSSHD_UI_E2E_BASE_URL;
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
let agent;
try {
  const context = await browser.newContext({ locale: "en-US", viewport: { width: 1800, height: 1000 } });
  await context.addInitScript(() => localStorage.setItem("gosshd_locale", "en"));
  const page = await context.newPage();
  page.setDefaultTimeout(15_000);
  const sockets = [];
  await page.routeWebSocket("**/terminal/ws?**", (socket) => sockets.push(socket));
  await page.route("**/api/targets/*/system**", (route) => route.fulfill({ json: { os: "windows", filesystems: [{ path: "C:", total_bytes: 1, used_bytes: 0 }, { path: "D:", total_bytes: 1, used_bytes: 0 }] } }));
  await page.route("**/api/targets/*/files?**", (route) => {
    const path = new URL(route.request().url()).searchParams.get("path");
    route.fulfill({ json: { path: path === "." ? "/C:/Users/user" : path, entries: [] } });
  });
  await login(page);
  const targets = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((response) => response.json());
    const create = async (alias) => {
      const response = await fetch("/api/targets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, name: alias, alias, target_type: "direct", host: "127.0.0.1", port: 1, remote_username: "root", auth_type: "password", secret: "test" }) });
      if (!response.ok) throw new Error(await response.text());
      return (await response.json()).target;
    };
    return [await create(`home-first-${Date.now()}`), await create(`home-delayed-${Date.now()}`)];
  });
  await page.goto(`${base}/targets/${targets[0].id}/connect`);
  await expectPath(page, "/C:/Users/user");
  assert.equal(sockets.length, 1);
  sockets[0].send(JSON.stringify({ type: "cwd", data: "C:\\Users\\user\\工作" }));
  await expectPath(page, "/C:/Users/user/工作");
  assert.match(await page.locator(".file-breadcrumb").innerText(), /C:.*user.*工作/s, "Windows SFTP breadcrumbs lost their drive or directory");
  await cwdAndMarker(page, sockets[0], "D:\\3D", "__CD_FIRST__");
  await expectPath(page, "/C:/Users/user/工作");
  await setPath(page, "D:\\3D");
  await cwdAndMarker(page, sockets[0], "C:\\Windows", "__CD_AFTER_NAVIGATION__");
  await expectPath(page, "/D:/3D");
  await page.evaluate((targetID) => window.postMessage({ type: "gosshd-connect-open-target", targetID }, location.origin), targets[1].id);
  await page.waitForFunction((id) => location.pathname === `/targets/${id}/connect`, targets[1].id);
  await expectPath(page, "/C:/Users/user");
  assert.equal(sockets.length, 2);
  await setPath(page, "/D:/manual-before-first-prompt");
  await cwdAndMarker(page, sockets[1], "C:\\Users\\user", "__DELAYED_PROMPT__");
  await expectPath(page, "/D:/manual-before-first-prompt");
  await cwdAndMarker(page, sockets[0], "C:\\inactive", "__INACTIVE_PROMPT__");
  await expectPath(page, "/D:/manual-before-first-prompt");
  await page.locator(".connection-tab").filter({ hasText: targets[0].alias }).locator(".connection-tab-main").click();
  await expectPath(page, "/D:/3D");
  await page.screenshot({ path: resolve("build/tests/terminal-home-sftp-independent.png"), fullPage: true });
  console.log("PASS initial terminal cwd, independent cd/navigation, delayed first prompt, inactive tab and Windows breadcrumbs");
  await context.close();

  if (process.env.GOSSHD_HOME_TEST_AGENT_BINARY) {
    const real = await browser.newContext({ locale: "en-US", acceptDownloads: true, viewport: { width: 1800, height: 1000 } });
    await real.addInitScript(() => { localStorage.setItem("gosshd_locale", "en"); window.showSaveFilePicker = undefined; });
    const realPage = await real.newPage();
    realPage.setDefaultTimeout(20_000);
    await login(realPage);
    const enrollment = await realPage.evaluate(async () => {
      const me = await fetch("/api/me").then((response) => response.json());
      const response = await fetch("/api/agent-enrollments", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, label: "home-sftp-regression" }) });
      if (!response.ok) throw new Error(await response.text());
      return response.json();
    });
    const root = await mkdtemp(join(tmpdir(), "gosshd-home-sftp-"));
    const home = join(root, "home with spaces");
    const install = join(root, "installation");
    await mkdir(home); await mkdir(install);
    const content = Buffer.from("cube(10); // 灯\n");
    await writeFile(join(home, "灯.scad"), content);
    const assignmentReady = waitForAssignment(install);
    agent = spawn(process.env.GOSSHD_HOME_TEST_AGENT_BINARY, ["--server", base, "--enrollment-token", enrollment.token, "--id-file", join(install, "agent.json"), "--shell", process.platform === "win32" ? "powershell.exe" : "/bin/bash"], { cwd: install, windowsHide: true, env: { ...process.env, USERPROFILE: home, HOME: home }, stdio: ["ignore", "ignore", "pipe"] });
    agent.stderr.on("data", () => {});
    const assignment = await assignmentReady;
    const target = { id: assignment.target_id };
    await realPage.goto(`${base}/targets/${target.id}/connect`);
    await expectPath(realPage, normalizeRemotePath(home));
    const row = realPage.locator(".file-manager-body").getByRole("button", { name: "灯.scad", exact: true });
    await row.waitFor();
    const download = realPage.waitForEvent("download");
    await row.dblclick();
    const saved = await download;
    assert.deepEqual(await readFile(await saved.path()), content, "Unicode Windows download changed bytes");
    const upload = Buffer.from("sphere(5); // 新灯\n");
    await realPage.locator('.file-manager input[type="file"]').first().setInputFiles({ name: "新灯.scad", mimeType: "application/octet-stream", buffer: upload });
    await realPage.locator(".file-upload-toast:not(.file-download-toast).success").waitFor();
    assert.deepEqual(await readFile(join(home, "新灯.scad")), upload);
    await realPage.screenshot({ path: resolve("build/tests/terminal-home-sftp-windows-transfer.png"), fullPage: true });
    await writeFile(resolve("build/tests/terminal-home-transfer-fixture.json"), JSON.stringify({ target: target.id, directory: home }));
    console.log("PASS real Agent starts in user home instead of installation cwd, raw SFTP Unicode download and upload");
    await real.close();
  }
} finally {
  if (agent) agent.kill();
  await browser.close();
}

async function login(page) {
  await page.goto(base);
  await page.getByLabel("Email", { exact: true }).fill("admin");
  await page.getByLabel("Password", { exact: true }).fill("admin-pass");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "SSH services", exact: true }).waitFor();
}
async function expectPath(page, expected) {
  await page.waitForFunction((path) => document.querySelector(".terminal-tab-layer.active .files-zone:not(.collapsed) .file-manager-path")?.getAttribute("title") === path, expected);
}
async function setPath(page, path) {
  await page.locator(".terminal-tab-layer.active .files-zone:not(.collapsed) .file-manager-path").dblclick();
  const input = page.locator(".terminal-tab-layer.active").getByRole("textbox", { name: "File path", exact: true });
  await input.fill(path); await input.press("Enter");
  await expectPath(page, normalizeRemotePath(path));
}
async function cwdAndMarker(page, socket, cwd, marker) {
  socket.send(JSON.stringify({ type: "cwd", data: cwd }));
  socket.send(JSON.stringify({ type: "output", data: `${marker}\r\n` }));
  await page.waitForFunction((text) => document.body.textContent.includes(text), marker);
}

function waitForAssignment(directory) {
  return new Promise((resolve, reject) => {
    let finished = false;
    const signal = AbortSignal.timeout(20_000);
    const watcher = watch(directory, check);
    const abort = () => finish(new Error("Agent assignment timed out"));
    const finish = (error, assignment) => {
      if (finished) return;
      finished = true; watcher.close(); signal.removeEventListener("abort", abort);
      if (error) reject(error); else resolve(assignment);
    };
    async function check() {
      try {
        const assignment = JSON.parse(await readFile(join(directory, "agent.json"), "utf8"));
        if (assignment.target_id) finish(undefined, assignment);
      } catch (error) { if (error.code !== "ENOENT" && !(error instanceof SyntaxError)) finish(error); }
    }
    watcher.on("error", (error) => finish(error));
    signal.addEventListener("abort", abort, { once: true });
    void check();
  });
}
