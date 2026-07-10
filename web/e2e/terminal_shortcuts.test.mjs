import assert from "node:assert/strict";

import {
  MOBILE_TERMINAL_KEY_ROWS,
  applyTerminalModifier,
  terminalShortcutSequence,
} from "../src/terminalShortcuts.ts";

assert.deepEqual(MOBILE_TERMINAL_KEY_ROWS, [
  ["escape", "tab", "ctrl", "alt", "home", "end"],
  ["pageUp", "left", "up", "down", "right", "pageDown"],
]);

assert.equal(terminalShortcutSequence("escape"), "\x1b");
assert.equal(terminalShortcutSequence("tab"), "\t");
assert.equal(terminalShortcutSequence("ctrl"), null);
assert.equal(terminalShortcutSequence("alt"), null);
assert.equal(terminalShortcutSequence("home"), "\x1b[H");
assert.equal(terminalShortcutSequence("end"), "\x1b[F");
assert.equal(terminalShortcutSequence("pageUp"), "\x1b[5~");
assert.equal(terminalShortcutSequence("left"), "\x1b[D");
assert.equal(terminalShortcutSequence("up"), "\x1b[A");
assert.equal(terminalShortcutSequence("down"), "\x1b[B");
assert.equal(terminalShortcutSequence("right"), "\x1b[C");
assert.equal(terminalShortcutSequence("pageDown"), "\x1b[6~");
assert.equal(terminalShortcutSequence("up", { applicationCursorMode: true }), "\x1bOA");
assert.equal(terminalShortcutSequence("home", { applicationCursorMode: true }), "\x1bOH");
assert.equal(terminalShortcutSequence("left", { modifier: "ctrl" }), "\x1b[1;5D");
assert.equal(terminalShortcutSequence("right", { modifier: "alt" }), "\x1b[1;5C");
assert.equal(terminalShortcutSequence("home", { modifier: "ctrl" }), "\x1b[1;5H");
assert.equal(terminalShortcutSequence("home", { modifier: "alt" }), "\x1b[1;3H");
assert.equal(terminalShortcutSequence("pageUp", { modifier: "ctrl" }), "\x1b[5;5~");
assert.equal(terminalShortcutSequence("pageDown", { modifier: "alt" }), "\x1b[6~");
assert.equal(terminalShortcutSequence("escape", { modifier: "alt" }), "\x1b\x1b");
assert.equal(terminalShortcutSequence("tab", { modifier: "ctrl" }), "\t");

assert.equal(applyTerminalModifier("c", "ctrl"), "\x03");
assert.equal(applyTerminalModifier("[", "ctrl"), "\x1b");
assert.equal(applyTerminalModifier("x", "alt"), "\x1bx");
assert.equal(applyTerminalModifier("paste", "ctrl"), "paste");
assert.equal(applyTerminalModifier("x", null), "x");

console.log("terminal shortcut checks passed");
