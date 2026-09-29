import hljs from "highlight.js/lib/common";
import clsx from "clsx";

const LANGUAGES = [
  "bash",
  "shell",
  "python",
  "sql",
  "javascript",
  "typescript",
  "json",
  "yaml",
  "powershell",
  "xml",
  "css",
  "markdown",
  "go",
  "java",
  "ruby",
  "php",
];

/** Syntax highlighting is performed by highlight.js, which escapes source text before emitting spans. */
export function HighlightedCommand({ command, className = "" }: { command: string; className?: string }) {
  return <code className={clsx("highlighted-command hljs", className)} aria-label={command} dangerouslySetInnerHTML={{ __html: highlightCommand(command) }} />;
}

export function highlightCommand(command: string): string {
  const source = command || "";
  const language = detectCommandLanguage(source);
  try {
    return language
      ? hljs.highlight(source, { language, ignoreIllegals: true }).value
      : hljs.highlightAuto(source, LANGUAGES).value;
  } catch {
    return escapeHTML(source);
  }
}

function detectCommandLanguage(command: string): string | undefined {
  const trimmed = command.trimStart();
  if (/^(?:sudo\s+)?(?:ba|z|k|d)?sh(?:\s|$)/i.test(trimmed) || /(?:^|\s)(?:set\s+-[eux]|export\s+\w+|if\s+\[|for\s+\w+\s+in\s+)/i.test(trimmed)) return "bash";
  if (/^(?:sudo\s+)?python(?:3(?:\.\d+)?)?(?:\s|$)/i.test(trimmed) || /<<['"]?(?:PY|PYTHON)['"]?/i.test(trimmed)) return "python";
  if (/^(?:sudo\s+)?(?:psql|mysql|sqlite3)(?:\s|$)/i.test(trimmed) || /\b(?:select|insert|update|delete|create|alter|drop)\s+/i.test(trimmed)) return "sql";
  if (/^(?:sudo\s+)?(?:node|deno|bun)(?:\s|$)/i.test(trimmed)) return "javascript";
  if (/^(?:sudo\s+)?(?:pwsh|powershell)(?:\s|$)/i.test(trimmed)) return "powershell";
  return undefined;
}

function escapeHTML(value: string) {
  return value.replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char] || char);
}
