import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
const require = createRequire(import.meta.url);
import ts from "typescript";
import hljs from "highlight.js/lib/common";

const source = await readFile(new URL("../src/components/HighlightedCommand.tsx", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, jsx: ts.JsxEmit.ReactJSX } }).outputText;
const { highlightCommand } = await import(`data:text/javascript;base64,${Buffer.from(compiled.replace(/from "([^"]+)"/g, (_, name) => `from "${pathToFileURL(require.resolve(name)).href}"`)).toString("base64")}`);

test("long scripts are escaped without invoking a syntax grammar", () => {
  const command = "python3 - <<'PY'\n" + '<script>alert("x")</script> & '.repeat(400) + "\nPY";
  const html = highlightCommand(command);
  assert.ok(!html.includes("<span"), "oversized scripts must bypass syntax highlighting");
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(html.endsWith("\nPY"), "full source must remain available");
});

test("ordinary shell commands do not use multi-language auto detection", () => {
  assert.match(highlightCommand("echo hello # comment"), /hljs-comment/);
});

test("multi-language detection is opt-in for expanded details", () => {
  const original = hljs.highlightAuto;
  let calls = 0;
  hljs.highlightAuto = (...args) => { calls++; return original(...args); };
  try {
    highlightCommand("echo hello");
    assert.equal(calls, 0);
    highlightCommand("const answer = 42;", true);
    assert.equal(calls, 1);
    highlightCommand("x".repeat(5000), true);
    assert.equal(calls, 1, "oversized expanded scripts must skip expensive detection");
  } finally { hljs.highlightAuto = original; }
});
