import assert from "node:assert/strict";
import test from "node:test";
import { contentPaneTree, defaultWorkspaceLayout, findPane, findPaneParent, firstLeafID, layoutPaneBounds, movePane, paneDockSide, paneLeaves, removePane, restoreWorkspace, saveWorkspace, splitPane, validateWorkspaceLayout } from "../src/workspaceLayout.ts";

const terminal = () => ({ type: "terminal", id: "shell", targetID: "server", restoreSession: false });
const editor = () => ({ type: "editor", id: "editor", targetID: "server", path: "/etc/app.conf" });

test("collapse directions follow docking edges and interior panels use a fold icon", () => {
  const original = defaultWorkspaceLayout(terminal(), "server");
  const files = paneLeaves(original).find(pane => pane.type === "files");
  const host = paneLeaves(original).find(pane => pane.type === "host");
  const side = (layout, id, collapsed = new Set()) => paneDockSide(layoutPaneBounds(layout, 1440, 800, collapsed).panes.get(id), 1440, 800, findPaneParent(layout, id).direction);
  assert.equal(side(original, host.id), "left");
  assert.equal(side(original, files.id), "right");
  for (const edge of ["left", "right", "up", "down"]) {
    const layout = movePane(original, files.id, null, edge);
    assert.equal(side(layout, files.id), edge);
    assert.equal(side(layout, files.id, new Set([files.id])), edge, "Collapse should not change the panel's docking edge");
  }
  const middle = movePane(original, host.id, "shell", "right");
  assert.equal(side(middle, host.id), null, "A panel between columns has no outward edge, even if it touches the workspace top");
  assert.equal(side(middle, host.id, new Set([host.id])), null);
});

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

test("stored layouts restore component positions with one fresh terminal and no temporary editors", () => {
  const values = new Map();
  globalThis.window = { localStorage: { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) } };
  const original = defaultWorkspaceLayout(splitPane(terminal(), "shell", editor(), "right"), "server");
  const files = paneLeaves(original).find((pane) => pane.type === "files");
  const moved = movePane(original, files.id, null, "down");
  saveWorkspace("layout", "server", moved, { hostOpen: false, filesOpen: true });
  const restored = restoreWorkspace("layout", "server", false, false);
  assert.equal(restored.layout.direction, "column");
  assert.equal(restored.layout.second.type, "files");
  assert.equal(restored.filePath, ".");
  assert.equal(findPane(restored.layout, restored.activePaneID).type, "terminal");
  assert.deepEqual(paneLeaves(restored.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
  assert.ok(!values.get("layout:target:server").includes("app.conf"), "Editor paths should never be persisted");
  assert.equal(paneLeaves(restored.layout).find(pane => pane.type === "terminal").restoreSession, false);
  assert.notEqual(findPane(restored.layout, restored.activePaneID).id, "editor");
  const next = restoreWorkspace("layout", "other", false, false);
  assert.deepEqual(paneLeaves(next.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
  assert.ok(paneLeaves(next.layout).every(pane => pane.targetID === "other"));
  assert.equal(next.layout.second.type, "files");
  values.set("layout:target:server", "invalid json");
  assert.ok(restoreWorkspace("layout", "server", true, false).layout);
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

test("closing the last terminal retains its remembered position instead of restoring editors", () => {
  const values = new Map();
  globalThis.window = { localStorage: { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) } };
  const original = movePane(defaultWorkspaceLayout(splitPane(terminal(), "shell", editor(), "right"), "server"), "shell", null, "down");
  saveWorkspace("editor-only", "server", original, { hostOpen: true, filesOpen: true });
  const savedLayout = restoreWorkspace("editor-only", "server", false, false).layout;
  const savedBounds = layoutPaneBounds(savedLayout, 1440, 800);
  const layout = removePane(original, "shell").node;
  assert.ok(validateWorkspaceLayout(layout));
  saveWorkspace("editor-only", "server", layout, { hostOpen: true, filesOpen: true });
  const restored = restoreWorkspace("editor-only", "server", false, false);
  assert.deepEqual(paneLeaves(restored.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
  const restoredBounds = layoutPaneBounds(restored.layout, 1440, 800);
  const byType = (layout, bounds, type) => bounds.panes.get(paneLeaves(layout).find(pane => pane.type === type).id);
  assert.deepEqual(byType(restored.layout, restoredBounds, "terminal"), byType(savedLayout, savedBounds, "terminal"), "The terminal must return to its full-width bottom slot");
  const next = restoreWorkspace("editor-only", "other", false, false);
  assert.deepEqual(paneLeaves(next.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
});

test("legacy saved editor sessions migrate to layouts without reopening files or extra shells", () => {
  const values = new Map();
  globalThis.window = { localStorage: { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) } };
  let layout = defaultWorkspaceLayout(splitPane(terminal(), "shell", editor(), "right"), "server");
  layout = splitPane(layout, "shell", { ...terminal(), id: "another-shell", targetID: "old-server" }, "down");
  values.set("legacy:target:server", JSON.stringify({ version: 1, layout, filePath: "/private", activePaneID: "editor", hostOpen: true, filesOpen: true }));
  let restored = restoreWorkspace("legacy", "server", true, false);
  assert.deepEqual(paneLeaves(restored.layout).map(pane => pane.type).sort(), ["files", "host", "terminal"]);
  assert.ok(paneLeaves(restored.layout).every(pane => pane.targetID === "server"));
  assert.equal(findPane(restored.layout, restored.activePaneID).restoreSession, true);
  layout = removePane(removePane(layout, "shell").node, "another-shell").node;
  values.set("legacy:target:server", JSON.stringify({ version: 1, layout, activePaneID: "editor" }));
  restored = restoreWorkspace("legacy", "server", false, false);
  assert.equal(findPane(restored.layout, restored.activePaneID).type, "terminal");
  assert.equal(paneLeaves(restored.layout).filter(pane => pane.type === "editor").length, 0);
  saveWorkspace("legacy", "server", restored.layout, { hostOpen: false, filesOpen: true });
  const stored = JSON.parse(values.get("legacy:target:server"));
  assert.equal(stored.version, 2);
  assert.equal(stored.filePath, undefined);
  assert.equal(stored.activePaneID, undefined);
});

test("tool movements after closing the last terminal keep a terminal slot", () => {
  const values = new Map();
  globalThis.window = { localStorage: { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) } };
  const original = defaultWorkspaceLayout(terminal(), "server");
  saveWorkspace("tools", "server", original, { hostOpen: true, filesOpen: true });
  const files = paneLeaves(original).find(pane => pane.type === "files");
  const withoutTerminal = removePane(original, "shell").node;
  const changed = movePane(withoutTerminal, files.id, null, "down");
  saveWorkspace("tools", "server", changed, { hostOpen: true, filesOpen: true });
  const restored = restoreWorkspace("tools", "server", false, false);
  assert.equal(restored.layout.second.type, "files", "The latest full-width bottom file panel should be remembered");
  assert.equal(findPane(restored.layout, restored.activePaneID).type, "terminal");
});
