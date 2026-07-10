export const MOBILE_TERMINAL_KEY_ROWS = [
  ["escape", "tab", "ctrl", "alt", "home", "end"],
  ["pageUp", "left", "up", "down", "right", "pageDown"],
] as const;

export type TerminalShortcutKey = (typeof MOBILE_TERMINAL_KEY_ROWS)[number][number];
export type TerminalModifier = "ctrl" | "alt" | null;

export const TERMINAL_SHORTCUT_LABELS: Record<TerminalShortcutKey, string> = {
  escape: "Esc",
  tab: "Tab",
  ctrl: "Ctrl",
  alt: "Alt",
  home: "Home",
  end: "End",
  pageUp: "PgUp",
  left: "←",
  up: "↑",
  down: "↓",
  right: "→",
  pageDown: "PgDn",
};

const TERMINAL_SHORTCUT_SEQUENCES: Record<TerminalShortcutKey, string | null> = {
  escape: "\x1b",
  tab: "\t",
  ctrl: null,
  alt: null,
  home: "\x1b[H",
  end: "\x1b[F",
  pageUp: "\x1b[5~",
  left: "\x1b[D",
  up: "\x1b[A",
  down: "\x1b[B",
  right: "\x1b[C",
  pageDown: "\x1b[6~",
};

export function terminalShortcutSequence(key: TerminalShortcutKey): string | null {
  return TERMINAL_SHORTCUT_SEQUENCES[key];
}

export function applyTerminalModifier(input: string | null, modifier: TerminalModifier): string | null {
  if (input === null) return null;
  if (modifier === "alt") return `\x1b${input}`;
  if (modifier !== "ctrl" || input.length !== 1) return input;

  const inputCode = input.charCodeAt(0);
  const code = inputCode >= 97 && inputCode <= 122 ? inputCode - 32 : inputCode;
  return code >= 64 && code <= 95 ? String.fromCharCode(code - 64) : input;
}
