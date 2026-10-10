export type SelectionMode = "replace" | "add" | "toggle";
export type SelectionModifiers = { shiftKey: boolean; altKey: boolean; ctrlKey: boolean; metaKey: boolean };
export type SelectionRect = { left: number; top: number; width: number; height: number };

export function applySelection(current: ReadonlySet<string>, paths: string[], mode: SelectionMode): Set<string> {
  const next = new Set(mode === "replace" ? [] : current);
  for (const path of paths) {
    if (mode === "toggle" && next.has(path)) next.delete(path);
    else next.add(path);
  }
  return next;
}

export function selectionMode(modifiers: SelectionModifiers): SelectionMode {
  return modifiers.altKey || modifiers.ctrlKey || modifiers.metaKey ? "toggle" : modifiers.shiftKey ? "add" : "replace";
}

export function selectEntry(current: ReadonlySet<string>, orderedPaths: string[], anchor: string | null, path: string, modifiers: SelectionModifiers): Set<string> {
  if (modifiers.shiftKey && anchor && orderedPaths.includes(anchor)) {
    const start = orderedPaths.indexOf(anchor), end = orderedPaths.indexOf(path);
    if (end >= 0) return applySelection(current, orderedPaths.slice(Math.min(start, end), Math.max(start, end) + 1), modifiers.altKey || modifiers.ctrlKey || modifiers.metaKey ? "add" : "replace");
  }
  return applySelection(current, [path], selectionMode(modifiers) === "add" ? "replace" : selectionMode(modifiers));
}

export function selectionRect(x1: number, y1: number, x2: number, y2: number): SelectionRect {
  return { left: Math.min(x1, x2), top: Math.min(y1, y2), width: Math.abs(x2 - x1), height: Math.abs(y2 - y1) };
}

export function intersectsSelection(a: SelectionRect, b: SelectionRect): boolean {
  return a.left < b.left + b.width && a.left + a.width > b.left && a.top < b.top + b.height && a.top + a.height > b.top;
}
