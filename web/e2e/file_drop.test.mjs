import assert from "node:assert/strict";
import test from "node:test";
import { collectDrop, filesUploadPlan, joinUploadPath, snapshotDrop, validateRelativePath } from "../src/fileDrop.ts";

const file = (name, contents = name) => new File([contents], name);
const entry = (name, contents) => ({ name, isFile: true, isDirectory: false, file: (ok) => queueMicrotask(() => ok(file(name, contents))) });
const directory = (name, pages) => ({ name, isFile: false, isDirectory: true, createReader: () => {
  let index = 0;
  return { readEntries: (ok) => queueMicrotask(() => ok(pages[index++] || [])) };
} });

test("multiple roots retain nested files, repeated basenames and empty directories", async () => {
  const plan = await collectDrop([
    { entry: directory("first", [[entry("same.txt", "first"), directory("nested", [[entry("empty.txt", "")]]), directory("empty-dir", [])]]) },
    { entry: directory("second", [[entry("same.txt", "second")]]) },
    { file: file("loose.txt") },
  ], new AbortController().signal);
  assert.deepEqual(plan.directories, ["first", "first/nested", "first/empty-dir", "second"]);
  assert.deepEqual(plan.files.map(({ relativePath }) => relativePath), ["first/same.txt", "first/nested/empty.txt", "second/same.txt", "loose.txt"]);
  assert.deepEqual(await Promise.all(plan.files.map(({ file }) => file.text())), ["first", "", "second", "loose.txt"]);
});

test("directory enumeration drains all pages rather than stopping after 100 entries", async () => {
  const children = Array.from({ length: 105 }, (_, i) => entry(`${i}.txt`));
  const plan = await collectDrop([{ entry: directory("many", [children.slice(0, 100), children.slice(100)]) }], new AbortController().signal);
  assert.equal(plan.files.length, 105);
  assert.equal(plan.files.at(-1).relativePath, "many/104.txt");
});

test("drop entries are captured while the protected drag store is still accessible", async () => {
  let readable = true;
  const dropped = directory("folder", [[entry("a.txt")]]);
  const sources = snapshotDrop({ items: [{ kind: "file", webkitGetAsEntry: () => {
    assert.ok(readable); return dropped;
  } }], files: [] });
  readable = false;
  const plan = await collectDrop(sources, new AbortController().signal);
  assert.equal(plan.files[0].relativePath, "folder/a.txt");
  assert.equal(snapshotDrop({ items: [], files: [file("fallback.txt")] })[0].file.name, "fallback.txt");
});

test("unsafe relative paths and duplicate roots fail before anything is uploaded", async () => {
  for (const path of ["", "/absolute", "../escape", "a/../b", "a//b", "C:escape", "a\\b", "a\0b"]) {
    assert.throws(() => validateRelativePath(path));
  }
  assert.equal(joinUploadPath("C:\\root", "folder/file.txt"), "C:/root/folder/file.txt");
  assert.equal(joinUploadPath("/root/", " folder /file.txt"), "/root/ folder /file.txt");
  assert.equal(filesUploadPlan([file("a.txt")]).files[0].relativePath, "a.txt");
  await assert.rejects(collectDrop([{ entry: directory("../escape", []) }], new AbortController().signal));
  await assert.rejects(collectDrop([{ file: file("a") }, { file: file("a") }], new AbortController().signal), /Duplicate/);
});

test("cancellation and unreadable entries stop directory traversal", async () => {
  const controller = new AbortController();
  const interrupted = directory("stop", []);
  interrupted.createReader = () => ({ readEntries: (ok) => { controller.abort(); ok([entry("ignored.txt")]); } });
  await assert.rejects(collectDrop([{ entry: interrupted }], controller.signal), { name: "AbortError" });
  await assert.rejects(collectDrop([{ entry: { name: "broken", isFile: true, isDirectory: false, file: (_, fail) => fail(new DOMException("unreadable")) } }], new AbortController().signal), /unreadable/);
});
