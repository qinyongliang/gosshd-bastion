import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
try {
  const context = await browser.newContext({ locale: "en-US", viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem("gosshd_locale", "en");
    // Exercise browser downloads without opening a native save dialog in headless CI.
    window.showSaveFilePicker = undefined;
  });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const deleted = [];
  const moved = [];
  const transfers = [];
  let listingRequests = 0;
  const entries = Array.from({ length: 12 }, (_, i) => ({ name: `${String(i + 1).padStart(2, "0")}.txt`, path: `/selection/${String(i + 1).padStart(2, "0")}.txt`, type: "file", size: i, mode: "-rw-------" }));
  entries.unshift({ name: "folder", path: "/selection/folder", type: "dir", size: 0, mode: "drwx------" });
  await page.route("**/api/targets/*/files?**", (route) => {
    listingRequests++;
    const url = new URL(route.request().url());
    const path = url.searchParams.get("path") === "." ? "/selection" : url.searchParams.get("path");
    const rows = path !== "/selection" ? [] : entries.filter((entry) => !deleted.includes(entry.path) && !moved.includes(entry.path));
    route.fulfill({ json: { path, entries: url.searchParams.get("order") === "desc" ? [...rows].reverse() : rows } });
  });
  await page.route("**/api/targets/*/files/delete", async (route) => {
    deleted.push(route.request().postDataJSON().path);
    await route.fulfill({ json: { path: deleted.at(-1) } });
  });
  for (const action of ["copy", "move"]) await page.route(`**/api/targets/*/files/${action}`, async (route) => {
    const body = route.request().postDataJSON();
    transfers.push({ action, ...body });
    await new Promise((resolve) => setTimeout(resolve, 150));
    if (body.destination === "/failure") return route.fulfill({ status: 502, json: { error: "/selection/04.txt: test transfer failed" } });
    if (action === "move") moved.push(...(body.sources || [body.source]));
    await route.fulfill({ json: body });
  });
  await page.route("**/api/targets/*/system**", (route) => route.fulfill({ json: { os: "linux", filesystems: [] } }));
  await page.routeWebSocket("**/files/download/ws?**", (ws) => ws.send(JSON.stringify({ type: "unavailable" })));
  await page.route("**/api/targets/*/files/download?**", (route) => route.fulfill({ body: new URL(route.request().url()).searchParams.get("path").split("/").at(-1), contentType: "application/octet-stream" }));
  const base = process.env.GOSSHD_UI_E2E_BASE_URL;
  await page.goto(base);
  await page.getByLabel("Email", { exact: true }).fill("admin");
  await page.getByLabel("Password", { exact: true }).fill("admin-pass");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "SSH services", exact: true }).waitFor();
  const target = await page.evaluate(async () => {
    const me = await fetch("/api/me").then((r) => r.json());
    const response = await fetch("/api/targets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ owner_type: "organization", owner_id: me.organizations[0].id, name: "Selection test", alias: "selection", target_type: "direct", host: "127.0.0.1", port: 1, remote_username: "root", auth_type: "password", secret: "test" }) });
    return (await response.json()).target;
  });
  await page.goto(`${base}/targets/${target.id}/connect`);
  await page.locator(".files-zone").waitFor();
  if (await page.locator(".files-zone .collapsed-zone-button").count()) await page.locator(".files-zone .collapsed-zone-button").click();
  const body = page.locator(".file-list-selectable");
  const row = (name) => body.locator("tr[data-file-path]").filter({ has: page.getByRole("button", { name, exact: true }) });
  await body.locator("tr[data-file-path]").first().waitFor();
  const chosen = () => body.locator('tr[aria-selected="true"]').evaluateAll((rows) => rows.map((row) => row.dataset.filePath.split("/").at(-1)));
  const expectChosen = async (names) => {
    await page.waitForFunction((expected) => JSON.stringify([...document.querySelectorAll('.file-list-selectable tr[aria-selected="true"]')].map((row) => row.dataset.filePath.split("/").at(-1)).sort()) === JSON.stringify(expected.sort()), names);
    assert.deepEqual((await chosen()).sort(), [...names].sort());
  };
  await row("02.txt").click();
  await row("05.txt").click({ modifiers: ["Shift"] });
  await expectChosen(["02.txt", "03.txt", "04.txt", "05.txt"]);
  await row("03.txt").click({ modifiers: ["Alt"] });
  await row("08.txt").click({ modifiers: ["Alt"] });
  await expectChosen(["02.txt", "04.txt", "05.txt", "08.txt"]);
  await row("04.txt").click({ button: "right" });
  await expectChosen(["02.txt", "04.txt", "05.txt", "08.txt"]);
  const downloads = [];
  page.on("download", (download) => downloads.push(download));
  let downloadCount = 0;
  const batchDownloaded = page.waitForEvent("download", { predicate: () => ++downloadCount === 4 });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "Download", exact: true }).click();
  try { await batchDownloaded; }
  catch (error) {
    console.error({ downloaded: downloads.map((download) => download.suggestedFilename()), errors, operationError: await page.locator(".file-operation-error").allTextContents() });
    throw error;
  }
  assert.deepEqual(downloads.map((download) => download.suggestedFilename()).sort(), ["02.txt", "04.txt", "05.txt", "08.txt"]);
  for (const download of downloads) assert.equal(await readFile(await download.path(), "utf8"), download.suggestedFilename());
  await expectChosen(["02.txt", "04.txt", "05.txt", "08.txt"]);
  const transferSelection = async (action, destination, browse = false) => {
    await row("04.txt").click({ button: "right" });
    await page.locator(".file-context-menu").getByRole("menuitem", { name: action, exact: true }).click();
    const dialog = page.getByRole("dialog");
    assert.equal(await dialog.locator("li").count(), 4);
    const input = dialog.getByLabel("Destination directory", { exact: true });
    if (browse) {
      await dialog.getByRole("button", { name: "folder", exact: true }).click();
      await page.waitForFunction((expected) => document.querySelector('.transfer-modal label input')?.value === expected, destination);
    } else await input.fill(destination);
    const count = transfers.length;
    await dialog.getByRole("button", { name: action, exact: true }).click();
    await page.waitForFunction(() => !document.querySelector(".transfer-modal"));
    assert.equal(transfers.length, count + 1, "A batch must submit once");
    assert.deepEqual(transfers.at(-1), { action: action.toLowerCase(), sources: ["02.txt", "04.txt", "05.txt", "08.txt"].map((name) => `/selection/${name}`), destination });
  };
  await transferSelection("Copy", "/selection/folder", true);
  await expectChosen(["02.txt", "04.txt", "05.txt", "08.txt"]);
  const beforeFailure = listingRequests;
  await transferSelection("Move", "/failure");
  await page.locator(".file-operation-error").getByText("/selection/04.txt: test transfer failed", { exact: true }).waitFor();
  assert.ok(listingRequests > beforeFailure, "Failed operations must refresh the remote listing");
  await page.locator(".file-operation-error").getByRole("button", { name: "Close", exact: true }).click();
  await row("04.txt").click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "Delete", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete", exact: true }).click();
  await page.waitForFunction(() => !document.querySelector('[data-file-path="/selection/08.txt"]'));
  assert.deepEqual(deleted.sort(), ["02.txt", "04.txt", "05.txt", "08.txt"].map((name) => `/selection/${name}`).sort());
  await body.focus();
  await body.press("Control+a");
  assert.equal((await chosen()).length, 9);
  await body.press("Escape");
  await expectChosen([]);
  const dragRows = async (first, last, modifiers = []) => {
    const start = await row(first).boundingBox(), end = await row(last).boundingBox();
    for (const key of modifiers) await page.keyboard.down(key);
    await page.mouse.move(start.x + start.width - 4, start.y + 2);
    await page.mouse.down();
    await page.mouse.move(end.x + end.width - 18, end.y + end.height - 2, { steps: 8 });
    await page.mouse.up();
    for (const key of modifiers) await page.keyboard.up(key);
  };
  await dragRows("03.txt", "07.txt");
  await expectChosen(["03.txt", "06.txt", "07.txt"]);
  await dragRows("06.txt", "07.txt", ["Alt"]);
  await expectChosen(["03.txt"]);
  await row("09.txt").click();
  await body.locator(".file-sort-button").first().click();
  await page.waitForFunction(() => document.querySelector(".file-list-selectable [data-file-path]")?.dataset.filePath.endsWith("12.txt"));
  await row("12.txt").click({ modifiers: ["Shift"] });
  await expectChosen(["09.txt", "10.txt", "11.txt", "12.txt"]);
  await row("10.txt").click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "Move", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Destination directory", { exact: true }).fill("/selection/folder");
  await page.getByRole("dialog").getByRole("button", { name: "Move", exact: true }).click();
  await page.waitForFunction(() => !document.querySelector('.file-list-selectable [data-file-path="/selection/09.txt"]'));
  assert.deepEqual(transfers.at(-1), { action: "move", sources: ["12.txt", "11.txt", "10.txt", "09.txt"].map((name) => `/selection/${name}`), destination: "/selection/folder" });
  await expectChosen([]);
  // Mixed file/folder selections use the same batch action.
  await row("folder").click();
  await row("01.txt").click({ modifiers: ["Alt"] });
  await row("folder").click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "Copy", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Destination directory", { exact: true }).fill("/destination");
  await page.getByRole("dialog").getByRole("button", { name: "Copy", exact: true }).click();
  await page.waitForFunction(() => !document.querySelector(".transfer-modal"));
  assert.deepEqual(new Set(transfers.at(-1).sources), new Set(["/selection/folder", "/selection/01.txt"]));
  // A single item keeps the full destination path, allowing rename-on-copy.
  await row("03.txt").click();
  await row("03.txt").click({ button: "right" });
  await page.locator(".file-context-menu").getByRole("menuitem", { name: "Copy", exact: true }).click();
  await page.getByRole("dialog").getByLabel("Destination path", { exact: true }).fill("/selection/folder/renamed.txt");
  await page.getByRole("dialog").getByRole("button", { name: "Copy", exact: true }).click();
  await page.waitForFunction(() => !document.querySelector(".transfer-modal"));
  assert.deepEqual(transfers.at(-1), { action: "copy", source: "/selection/03.txt", destination: "/selection/folder/renamed.txt" });
  await row("folder").click();
  assert.equal(await page.locator(".file-manager-path").getAttribute("title"), "/selection", "Single clicks must select folders without navigating");
  await row("folder").dblclick();
  await page.waitForFunction(() => document.querySelector(".file-manager-path").title === "/selection/folder");
  await expectChosen([]);
  assert.deepEqual(errors, []);
  await context.close();
} finally { await browser.close(); }
