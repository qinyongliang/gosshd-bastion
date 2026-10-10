import assert from "node:assert/strict";
import test from "node:test";
import { contentPaneTree, defaultWorkspaceLayout, findPane, firstLeafID, layoutPaneBounds, movePane, paneLeaves, removePane, restoreWorkspace, saveWorkspace, splitPane, validateWorkspaceLayout } from "../src/workspaceLayout.ts";

const terminal = () => ({ type: "terminal", id: "shell", targetID: "server", restoreSession: false });
const editor = () => ({ type: "editor", id: "editor", targetID: "server", path: "/etc/app.conf" });

test("docking files below the entire workspace spans both host and content", () => {
  const original = defaultWorkspaceLayout(terminal(), "server");
  const files = paneLeaves(original).find((pane) => pane.type === "files");
  const moved = movePane(original, files.id, null, "down");
  assert.equal(moved.direction, "column");
  assert.equal(moved.second.id, files.id);
  const bounds = layoutPaneBounds(moved, 1440, 800);
  assert.equal(bounds.panes.get(files.id).width, 1440);
  assert.equal(bounds.panes.get(files.id).left, 0);
  assert.ok(bounds.panes.get(files.id).top > bounds.panes.get("shell").top);
  assert.equal(paneLeaves(original).find((pane) => pane.id === files.id), files, "Original tree is not mutated");
});

test("moving an editor preserves every view and its identifiers", () => {
  let layout = defaultWorkspaceLayout(terminal(), "server");
  layout = splitPane(layout, "shell", editor(), "up");
  const moved = movePane(layout, "editor", "shell", "right");
  const panes = paneLeaves(moved);
  assert.deepEqual(panes.map((pane) => pane.id).sort(), paneLeaves(layout).map((pane) => pane.id).sort());
  assert.equal(findPane(moved, "editor").path, "/etc/app.conf");
  const bounds = layoutPaneBounds(moved, 1800, 900);
  assert.ok(bounds.panes.get("editor").left > bounds.panes.get("shell").left);
  assert.equal(bounds.panes.get("editor").top, bounds.panes.get("shell").top);
  assert.equal(movePane(layout, "editor", "editor", "down"), layout);
  assert.equal(movePane(layout, "editor", "missing", "down"), layout);
});

test("collapsed tools use a compact strip and tiny viewports never overflow", () => {
  const layout = defaultWorkspaceLayout(terminal(), "server");
  const tools = new Set(paneLeaves(layout).filter((pane) => pane.type === "host" || pane.type === "files").map((pane) => pane.id));
  const bounds = layoutPaneBounds(layout, 1440, 800, tools);
  for (const id of tools) assert.equal(bounds.panes.get(id).width, 32);
  for (const width of [20, 280, 720, 1440]) {
    for (const pane of layoutPaneBounds(layout, width, 300).panes.values()) {
      assert.ok(pane.left >= 0 && pane.top >= 0 && pane.width >= 0 && pane.height >= 0);
      assert.ok(pane.left + pane.width <= width + 0.001);
      assert.ok(pane.top + pane.height <= 300 + 0.001);
    }
  }
});

test("tab merging strips tool panels and focuses a content view", () => {
  const original = defaultWorkspaceLayout(splitPane(terminal(), "shell", editor(), "right"), "server");
  assert.equal(firstLeafID(original), "shell");
  assert.deepEqual(paneLeaves(contentPaneTree(original)).map((pane) => pane.type), ["terminal", "editor"]);
});

test("stored geometry, editors and file paths restore while fresh shells do not reuse a session", () => {
  const values = new Map();
  globalThis.window = { localStorage: { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) } };
  const original = defaultWorkspaceLayout(splitPane(terminal(), "shell", editor(), "right"), "server");
  const files = paneLeaves(original).find((pane) => pane.type === "files");
  const moved = movePane(original, files.id, null, "down");
  saveWorkspace("layout", "server", moved, "/tmp", "editor", { hostOpen: false, filesOpen: true });
  const restored = restoreWorkspace("layout", "server", false, false, new Set(["server"]));
  assert.equal(restored.layout.direction, "column");
  assert.equal(restored.layout.second.type, "files");
  assert.equal(restored.filePath, "/tmp");
  assert.equal(findPane(restored.layout, restored.activePaneID).type, "editor");
  assert.equal(paneLeaves(restored.layout).find(pane => pane.type === "terminal").restoreSession, false);
  assert.notEqual(findPane(restored.layout, restored.activePaneID).id, "editor");
  const next = restoreWorkspace("layout", "other", false, false, new Set(["other"]));
  assert.deepEqual(paneLeaves(next.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
  assert.ok(paneLeaves(next.layout).every(pane => pane.targetID === "other"));
  assert.equal(next.layout.second.type, "files");
  values.set("layout:target:server", "invalid json");
  assert.ok(restoreWorkspace("layout", "server", true, false, new Set(["server"])).layout);
});

test("malformed, duplicate, stale and excessive layouts are rejected", () => {
  const layout = defaultWorkspaceLayout(terminal(), "server");
  assert.ok(validateWorkspaceLayout(layout));
  assert.equal(validateWorkspaceLayout({ ...layout, ratio: Infinity }), null);
  assert.equal(validateWorkspaceLayout({ ...layout, ratio: -1 }), null);
  assert.equal(validateWorkspaceLayout({ ...layout, direction: "diagonal" }), null);
  assert.equal(validateWorkspaceLayout({ ...layout, second: layout.first }), null);
  assert.equal(validateWorkspaceLayout(layout, new Set(["other"])), null);
  assert.equal(validateWorkspaceLayout(terminal()), null);
  let deep = layout;
  for (let index = 0; index < 20; index++) deep = splitPane(deep, "shell", { ...editor(), id: `editor-${index}` }, "right");
  assert.equal(validateWorkspaceLayout(deep), null);
});

test("an editor-only workspace restores after closing its last shell", () => {
  const values = new Map();
  globalThis.window = { localStorage: { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) } };
  const layout = removePane(defaultWorkspaceLayout(splitPane(terminal(), "shell", editor(), "right"), "server"), "shell").node;
  assert.ok(validateWorkspaceLayout(layout));
  saveWorkspace("editor-only", "server", layout, "/tmp", "editor", { hostOpen: true, filesOpen: true });
  const restored = restoreWorkspace("editor-only", "server", false, false, new Set(["server"]));
  assert.deepEqual(paneLeaves(restored.layout).map(pane => pane.type).sort(), ["editor", "files", "host"]);
  const next = restoreWorkspace("editor-only", "other", false, false, new Set(["other"]));
  assert.deepEqual(paneLeaves(next.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
});
