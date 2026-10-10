export type PaneSide = "left" | "right" | "up" | "down";
export type TerminalPaneNode = { type: "terminal"; id: string; targetID: string; restoreSession: boolean };
export type EditorPaneNode = { type: "editor"; id: string; targetID: string; path: string };
export type ToolPaneNode = { type: "host" | "files"; id: string; targetID: string };
export type PaneLeaf = TerminalPaneNode | EditorPaneNode | ToolPaneNode;
export type SplitPaneNode = { type: "split"; id: string; direction: "row" | "column"; ratio: number; first: PaneNode; second: PaneNode };
export type PaneNode = PaneLeaf | SplitPaneNode;
export type PaneBounds = { left: number; top: number; width: number; height: number };
export type SplitBounds = PaneBounds & { node: SplitPaneNode };
export type WorkspacePreferences = { hostOpen: boolean; filesOpen: boolean };
type StoredWorkspace = WorkspacePreferences & { version: 2; layout: PaneNode };

export function paneDockSide(rect: PaneBounds, width: number, height: number, direction: SplitPaneNode["direction"]): PaneSide | null {
  const left = rect.left <= 1, right = rect.left + rect.width >= width - 1;
  const up = rect.top <= 1, down = rect.top + rect.height >= height - 1;
  const horizontal: PaneSide | null = left !== right ? left ? "left" : "right" : null;
  const vertical: PaneSide | null = up !== down ? up ? "up" : "down" : null;
  return direction === "row" ? horizontal : vertical;
}

export function newPaneID(prefix: string) {
  return `${prefix}:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
}

export function paneLeaves(node: PaneNode): PaneLeaf[] {
  return node.type === "split" ? [...paneLeaves(node.first), ...paneLeaves(node.second)] : [node];
}

export function firstLeafID(node: PaneNode): string {
  return paneLeaves(node).find((pane) => pane.type === "terminal" || pane.type === "editor")?.id || node.id;
}

export function findPane(node: PaneNode, paneID: string): PaneNode | null {
  if (node.id === paneID) return node;
  return node.type === "split" ? findPane(node.first, paneID) || findPane(node.second, paneID) : null;
}

export function findPaneParent(node: PaneNode, paneID: string): SplitPaneNode | null {
  if (node.type !== "split") return null;
  if (node.first.id === paneID || node.second.id === paneID) return node;
  return findPaneParent(node.first, paneID) || findPaneParent(node.second, paneID);
}

export function splitPane(node: PaneNode, paneID: string, nextPane: PaneNode, side: PaneSide, fraction = 0.5): PaneNode {
  if (node.id === paneID) {
    const before = side === "left" || side === "up";
    return { type: "split", id: newPaneID("split"), direction: side === "up" || side === "down" ? "column" : "row", ratio: before ? fraction : 1 - fraction, first: before ? nextPane : node, second: before ? node : nextPane };
  }
  return node.type === "split" ? { ...node, first: splitPane(node.first, paneID, nextPane, side, fraction), second: splitPane(node.second, paneID, nextPane, side, fraction) } : node;
}

export function removePane(node: PaneNode, paneID: string): { node: PaneNode | null; activePaneID: string } {
  if (node.id === paneID) return { node: null, activePaneID: "" };
  if (node.type !== "split") return { node, activePaneID: firstLeafID(node) };
  const first = removePane(node.first, paneID).node;
  const second = removePane(node.second, paneID).node;
  const result = first && second ? { ...node, first, second } : first || second;
  return { node: result, activePaneID: result ? firstLeafID(result) : "" };
}

export function movePane(node: PaneNode, sourceID: string, destinationID: string | null, side: PaneSide): PaneNode {
  const source = findPane(node, sourceID);
  if (!source || source.type === "split" || sourceID === destinationID) return node;
  const remaining = removePane(node, sourceID).node;
  if (!remaining || (destinationID && !findPane(remaining, destinationID))) return node;
  return splitPane(remaining, destinationID || remaining.id, source, side, source.type === "host" || source.type === "files" ? 0.3 : 0.5);
}

export function resizeSplit(node: PaneNode, splitID: string, ratio: number): PaneNode {
  if (node.type !== "split") return node;
  if (node.id === splitID) return { ...node, ratio: Math.min(0.95, Math.max(0.05, ratio)) };
  return { ...node, first: resizeSplit(node.first, splitID, ratio), second: resizeSplit(node.second, splitID, ratio) };
}

export function contentPaneTree(node: PaneNode): PaneNode | null {
  if (node.type === "host" || node.type === "files") return null;
  if (node.type !== "split") return node;
  const first = contentPaneTree(node.first), second = contentPaneTree(node.second);
  return first && second ? { ...node, first, second } : first || second;
}

// Wrap the whole content/file split so host information occupies a complete edge.
export function defaultWorkspaceLayout(content: PaneNode, targetID: string, mobile = false): PaneNode {
  const files: ToolPaneNode = { type: "files", id: newPaneID("files"), targetID };
  const host: ToolPaneNode = { type: "host", id: newPaneID("host"), targetID };
  const main = splitPane(content, content.id, files, mobile ? "down" : "right", 0.28);
  return splitPane(main, main.id, host, mobile ? "up" : "left", mobile ? 0.22 : 0.19);
}

export function layoutPaneBounds(node: PaneNode, width: number, height: number, collapsed: ReadonlySet<string> = new Set()) {
  const panes = new Map<string, PaneBounds>();
  const splits: SplitBounds[] = [];
  const gap = 5;
  const fixed = (item: PaneNode, horizontal: boolean): number | null => {
    if (item.type !== "split") return collapsed.has(item.id) ? 32 : null;
    const a = fixed(item.first, horizontal), b = fixed(item.second, horizontal);
    if (a === null || b === null) return null;
    return (item.direction === "row") === horizontal ? a + b + gap : Math.max(a, b);
  };
  const minimum = (item: PaneNode, horizontal: boolean): number => {
    if (item.type !== "split") return collapsed.has(item.id) ? 32 : horizontal ? (item.type === "files" ? 220 : 160) : 100;
    const a = minimum(item.first, horizontal), b = minimum(item.second, horizontal);
    return (item.direction === "row") === horizontal ? a + b + gap : Math.max(a, b);
  };
  const visit = (item: PaneNode, rect: PaneBounds) => {
    if (item.type !== "split") { panes.set(item.id, rect); return; }
    const horizontal = item.direction === "row";
    const size = horizontal ? rect.width : rect.height;
    const divider = Math.min(gap, size);
    const available = Math.max(0, size - divider);
    const aFixed = fixed(item.first, horizontal), bFixed = fixed(item.second, horizontal);
    const aMin = minimum(item.first, horizontal), bMin = minimum(item.second, horizontal);
    let aSize = available * item.ratio;
    if (aFixed !== null) aSize = Math.min(aFixed, available);
    else if (bFixed !== null) aSize = Math.max(0, available - bFixed);
    else if (aMin + bMin > available) aSize = available * aMin / (aMin + bMin);
    else aSize = Math.max(aMin, Math.min(available - bMin, aSize));
    const bSize = available - aSize;
    visit(item.first, { ...rect, width: horizontal ? aSize : rect.width, height: horizontal ? rect.height : aSize });
    splits.push({ node: item, left: rect.left + (horizontal ? aSize : 0), top: rect.top + (horizontal ? 0 : aSize), width: horizontal ? divider : rect.width, height: horizontal ? rect.height : divider });
    visit(item.second, { left: rect.left + (horizontal ? aSize + divider : 0), top: rect.top + (horizontal ? 0 : aSize + divider), width: horizontal ? bSize : rect.width, height: horizontal ? rect.height : bSize });
  };
  visit(node, { left: 0, top: 0, width: Math.max(0, width), height: Math.max(0, height) });
  return { panes, splits };
}

export function validateWorkspaceLayout(value: unknown, allowedTargets?: ReadonlySet<string>): PaneNode | null {
  const ids = new Set<string>();
  let count = 0;
  const parse = (raw: unknown, depth: number): PaneNode | null => {
    if (!raw || typeof raw !== "object" || depth > 16 || ++count > 127) return null;
    const item = raw as Record<string, unknown>;
    if (typeof item.id !== "string" || !item.id || ids.has(item.id)) return null;
    ids.add(item.id);
    if (item.type === "split") {
      if ((item.direction !== "row" && item.direction !== "column") || typeof item.ratio !== "number" || !Number.isFinite(item.ratio) || item.ratio < 0.05 || item.ratio > 0.95) return null;
      const first = parse(item.first, depth + 1), second = parse(item.second, depth + 1);
      return first && second ? { type: "split", id: item.id, direction: item.direction, ratio: item.ratio, first, second } : null;
    }
    if (typeof item.targetID !== "string" || !item.targetID || (allowedTargets && !allowedTargets.has(item.targetID))) return null;
    if (item.type === "terminal") return { type: "terminal", id: item.id, targetID: item.targetID, restoreSession: false };
    if (item.type === "editor" && typeof item.path === "string" && item.path) return { type: "editor", id: item.id, targetID: item.targetID, path: item.path };
    if (item.type === "host" || item.type === "files") return { type: item.type, id: item.id, targetID: item.targetID };
    return null;
  };
  const node = parse(value, 0);
  if (!node) return null;
  const leaves = paneLeaves(node);
  return leaves.filter((item) => item.type === "host").length === 1 && leaves.filter((item) => item.type === "files").length === 1 && leaves.some((item) => item.type === "terminal" || item.type === "editor") ? node : null;
}

function workspaceTemplate(layout: PaneNode, previous?: PaneNode): PaneNode | null {
  const leaves = paneLeaves(layout);
  const terminal = leaves.find(pane => pane.type === "terminal");
  const keep = terminal || (!previous ? leaves.find(pane => pane.type === "editor") : undefined);
  let template: PaneNode | null = layout;
  for (const pane of leaves) {
    if ((pane.type === "terminal" || pane.type === "editor") && pane.id !== keep?.id) {
      template = template && removePane(template, pane.id).node;
    }
  }
  if (!template) return null;
  if (!keep) {
    const remembered = previous && paneLeaves(previous).find(pane => pane.type === "terminal");
    const parent = remembered && findPaneParent(previous!, remembered.id);
    if (!remembered || !parent) return null;
    const before = parent.first.id === remembered.id;
    const sibling = before ? parent.second : parent.first;
    let anchor = template;
    if (sibling.type !== "split") {
      const previousBounds = layoutPaneBounds(previous!, 1000, 1000).panes.get(remembered.id)!;
      const centerX = previousBounds.left + previousBounds.width / 2;
      const centerY = previousBounds.top + previousBounds.height / 2;
      const currentBounds = layoutPaneBounds(template, 1000, 1000).panes;
      anchor = paneLeaves(template).find(pane => {
        const rect = currentBounds.get(pane.id)!;
        return centerX >= rect.left && centerX <= rect.left + rect.width && centerY >= rect.top && centerY <= rect.top + rect.height;
      }) || paneLeaves(template).find(pane => pane.type === sibling.type) || template;
    }
    const side = parent.direction === "row" ? before ? "left" : "right" : before ? "up" : "down";
    template = splitPane(template, anchor.id, remembered, side, before ? parent.ratio : 1 - parent.ratio);
  }
  // Keep geometry and component roles, without target/session state or editor paths.
  const sanitize = (node: PaneNode): PaneNode => node.type === "split"
    ? { ...node, first: sanitize(node.first), second: sanitize(node.second) }
    : node.type === "host" || node.type === "files"
      ? { type: node.type, id: node.id, targetID: "layout" }
      : { type: "terminal", id: node.id, targetID: "layout", restoreSession: false };
  return sanitize(template);
}

function readStoredWorkspace(key: string): StoredWorkspace | null {
  try {
    const raw = JSON.parse(window.localStorage.getItem(key) || "null");
    if (raw?.version !== 1 && raw?.version !== 2) return null;
    const valid = validateWorkspaceLayout(raw.layout);
    const layout = valid && workspaceTemplate(valid);
    return layout ? { version: 2, layout, hostOpen: Boolean(raw.hostOpen), filesOpen: Boolean(raw.filesOpen) } : null;
  } catch { return null; }
}

export function readWorkspacePreferences(key: string): WorkspacePreferences | null {
  const stored = readStoredWorkspace(key);
  return stored ? { hostOpen: stored.hostOpen, filesOpen: stored.filesOpen } : null;
}

export function restoreWorkspace(key: string, targetID: string, restoreSession: boolean, mobile: boolean) {
  const template = readStoredWorkspace(`${key}:target:${targetID}`) || readStoredWorkspace(key);
  const terminal: TerminalPaneNode = { type: "terminal", id: newPaneID("terminal"), targetID, restoreSession };
  if (!template) return { layout: defaultWorkspaceLayout(terminal, targetID, mobile), filePath: ".", activePaneID: terminal.id };
  const clone = (node: PaneNode): PaneNode => {
    if (node.type === "split") return { ...node, id: newPaneID("split"), first: clone(node.first), second: clone(node.second) };
    if (node.type === "terminal") return terminal;
    return { ...node, id: newPaneID(node.type), targetID };
  };
  const layout = clone(template.layout);
  return { layout, filePath: ".", activePaneID: terminal.id };
}

export function saveWorkspace(key: string, targetID: string, layout: PaneNode, preferences: WorkspacePreferences) {
  try {
    const previous = readStoredWorkspace(`${key}:target:${targetID}`) || readStoredWorkspace(key);
    const template = workspaceTemplate(layout, previous?.layout);
    if (!template) return;
    const stored: StoredWorkspace = { version: 2, layout: template, ...preferences };
    const serialized = JSON.stringify(stored);
    window.localStorage.setItem(`${key}:target:${targetID}`, serialized);
    window.localStorage.setItem(key, serialized);
  } catch { /* The workspace remains usable when browser storage is unavailable or full. */ }
}
