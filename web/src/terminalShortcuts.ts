export const MOBILE_TERMINAL_KEY_ROWS = [
  ["escape", "tab", "ctrl", "alt", "home", "end"],
  ["pageUp", "left", "up", "down", "right", "pageDown"],
] as const;

export type TerminalShortcutKey = (typeof MOBILE_TERMINAL_KEY_ROWS)[number][number];
export type TerminalModifier = Extract<TerminalShortcutKey, "ctrl" | "alt">;
export type TerminalShortcutOptions = {
  applicationCursorMode?: boolean;
  modifier?: TerminalModifier | null;
};

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

const CURSOR_KEY_FINALS = {
  home: "H",
  end: "F",
  left: "D",
  up: "A",
  down: "B",
  right: "C",
} as const;

export function terminalShortcutSequence(
  key: TerminalShortcutKey,
  { applicationCursorMode = false, modifier = null }: TerminalShortcutOptions = {},
): string | null {
  if (key === "ctrl" || key === "alt") return null;
  if (key === "escape") return modifier === "alt" ? "\x1b\x1b" : "\x1b";
  if (key === "tab") return "\t";
  if (key === "pageUp" || key === "pageDown") {
    if (modifier === "ctrl") return `\x1b[${key === "pageUp" ? 5 : 6};5~`;
    return TERMINAL_SHORTCUT_SEQUENCES[key];
  }

  const final = CURSOR_KEY_FINALS[key];
  if (modifier) {
    const modifierCode = modifier === "alt" && (key === "home" || key === "end") ? 3 : 5;
    return `\x1b[1;${modifierCode}${final}`;
  }
  if (applicationCursorMode) return `\x1bO${final}`;
  return TERMINAL_SHORTCUT_SEQUENCES[key];
}

export function applyTerminalModifier(value: string, modifier: TerminalModifier | null): string {
  if (modifier === "alt") return `\x1b${value}`;
  if (modifier !== "ctrl" || value.length !== 1) return value;

  const inputCode = value.charCodeAt(0);
  const code = inputCode >= 97 && inputCode <= 122 ? inputCode - 32 : inputCode;
  return code >= 64 && code <= 95 ? String.fromCharCode(code - 64) : value;
}
