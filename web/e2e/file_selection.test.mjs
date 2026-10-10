import assert from "node:assert/strict";
import test from "node:test";
import { applySelection, intersectsSelection, selectEntry, selectionRect } from "../src/fileSelection.ts";

const modifiers = (extra = {}) => ({ shiftKey: false, altKey: false, ctrlKey: false, metaKey: false, ...extra });
const sorted = ["directory", "c", "b", "a"];

test("click replaces selection and Shift ranges follow the displayed sort order", () => {
  assert.deepEqual([...selectEntry(new Set(["a"]), sorted, "a", "b", modifiers())], ["b"]);
  assert.deepEqual([...selectEntry(new Set(["a"]), sorted, "a", "c", modifiers({ shiftKey: true }))], ["c", "b", "a"]);
  assert.deepEqual([...selectEntry(new Set(), sorted, "missing", "b", modifiers({ shiftKey: true }))], ["b"]);
});

test("Alt toggles individual items without changing the other selected paths", () => {
  const initial = new Set(["a", "b"]);
  const removed = selectEntry(initial, sorted, "a", "b", modifiers({ altKey: true }));
  assert.deepEqual([...removed], ["a"]);
  assert.deepEqual([...selectEntry(removed, sorted, "a", "c", modifiers({ altKey: true }))], ["a", "c"]);
  assert.deepEqual([...initial], ["a", "b"]);
});

test("rectangle selection supports replacement, addition and exclusion from a stable base", () => {
  const base = new Set(["a", "b"]);
  assert.deepEqual([...applySelection(base, ["b", "c"], "replace")], ["b", "c"]);
  assert.deepEqual([...applySelection(base, ["b", "c"], "add")], ["a", "b", "c"]);
  assert.deepEqual([...applySelection(base, ["b", "c"], "toggle")], ["a", "c"]);
  assert.deepEqual([...applySelection(base, ["b", "c"], "toggle")], ["a", "c"], "Each pointer update uses the original base, rather than toggling repeatedly");
  const box = selectionRect(100, 80, 10, 20);
  assert.deepEqual(box, { left: 10, top: 20, width: 90, height: 60 });
  assert.equal(intersectsSelection(box, { left: 20, top: 35, width: 100, height: 20 }), true);
  assert.equal(intersectsSelection(box, { left: 20, top: 85, width: 100, height: 20 }), false);
});
