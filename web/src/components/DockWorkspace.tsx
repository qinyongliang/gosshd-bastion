import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, GripVertical, PanelTopClose, PanelTopOpen } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { createContext, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { findPane, findPaneParent, layoutPaneBounds, movePane, paneDockSide, paneLeaves, resizeSplit } from "../workspaceLayout";
import type { PaneBounds, PaneLeaf, PaneNode, PaneSide, SplitBounds } from "../workspaceLayout";
import "./dockWorkspace.css";

type DropPlacement = { sourceID: string; destinationID: string | null; side: PaneSide; bounds: PaneBounds };
type DockDragContext = { startDrag: (id: string, event: React.PointerEvent<HTMLElement>) => void; collapseSide: (id: string) => PaneSide | null };
const DragContext = createContext<DockDragContext | null>(null);

export function DockDragHandle({ paneID, children }: { paneID: string; children?: ReactNode }) {
  const context = useContext(DragContext);
  const { t } = useI18n();
  return <button type="button" className={`dock-drag-handle ${children ? "with-title" : ""}`} title={t("connectDragView")} aria-label={t("connectDragView")} onPointerDown={(event) => context?.startDrag(paneID, event)}>
    <GripVertical />{children && <span>{children}</span>}
  </button>;
}

export function DockPanelToggle({ paneID, open, onToggle, children }: { paneID: string; open: boolean; onToggle: () => void; children?: ReactNode }) {
  const context = useContext(DragContext);
  const { t } = useI18n();
  const side = context?.collapseSide(paneID) || null;
  const opposite: Record<PaneSide, PaneSide> = { left: "right", right: "left", up: "down", down: "up" };
  const direction = side && (open ? side : opposite[side]);
  const Icon = direction ? { left: ChevronLeft, right: ChevronRight, up: ChevronUp, down: ChevronDown }[direction] : open ? PanelTopClose : PanelTopOpen;
  const label = t(open ? "connectCollapseSidebar" : "connectExpandSidebar");
  return <button type="button" className={`dock-panel-toggle ${open ? "icon-button" : "collapsed-zone-button"}`} onClick={onToggle} title={label} aria-label={label} aria-expanded={open}>
    <Icon />{!open && children && <span>{children}</span>}
  </button>;
}

export function DockWorkspace({ layout, collapsed, fullscreenPaneID, active, onLayoutChange, renderPane }: {
  layout: PaneNode;
  collapsed: ReadonlySet<string>;
  fullscreenPaneID?: string;
  active: boolean;
  onLayoutChange: (layout: PaneNode) => void;
  renderPane: (pane: PaneLeaf) => ReactNode;
}) {
  const { t } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  const cleanupRef = useRef<(() => void) | null>(null);
  const layoutRef = useRef(layout);
  layoutRef.current = layout;
  const changeRef = useRef(onLayoutChange);
  changeRef.current = onLayoutChange;
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [drag, setDrag] = useState<{ sourceID: string; placement: DropPlacement | null } | null>(null);
  const bounds = useMemo(() => layoutPaneBounds(layout, size.width, size.height, collapsed), [layout, size, collapsed]);

  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const measure = () => setSize({ width: root.clientWidth, height: root.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(root);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!active || fullscreenPaneID) cleanupRef.current?.();
  }, [active, fullscreenPaneID]);
  useEffect(() => () => cleanupRef.current?.(), []);

  const startDrag = (sourceID: string, event: React.PointerEvent<HTMLElement>) => {
    if (!active || fullscreenPaneID || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    cleanupRef.current?.();
    const root = rootRef.current;
    if (!root) return;
    const startX = event.clientX, startY = event.clientY, pointerID = event.pointerId;
    let dragging = false;
    let placement: DropPlacement | null = null;
    const onMove = (move: PointerEvent) => {
      if (move.pointerId !== pointerID) return;
      if (!dragging && Math.hypot(move.clientX - startX, move.clientY - startY) < 6) return;
      dragging = true;
      document.body.classList.add("is-dragging-dock");
      const rect = root.getBoundingClientRect();
      const x = move.clientX - rect.left, y = move.clientY - rect.top;
      placement = null;
      if (x >= 0 && y >= 0 && x <= rect.width && y <= rect.height) {
        const edges: Array<[PaneSide, number]> = [["left", x], ["right", rect.width - x], ["up", y], ["down", rect.height - y]];
        const [edge, distance] = edges.sort((a, b) => a[1] - b[1])[0];
        if (distance <= 24) placement = { sourceID, destinationID: null, side: edge, bounds: { left: 0, top: 0, width: rect.width, height: rect.height } };
        else {
          for (const [id, pane] of bounds.panes) {
            if (id === sourceID || x < pane.left || x > pane.left + pane.width || y < pane.top || y > pane.top + pane.height) continue;
            const distances: Array<[PaneSide, number]> = [["left", (x - pane.left) / pane.width], ["right", (pane.left + pane.width - x) / pane.width], ["up", (y - pane.top) / pane.height], ["down", (pane.top + pane.height - y) / pane.height]];
            placement = { sourceID, destinationID: id, side: distances.sort((a, b) => a[1] - b[1])[0][0], bounds: pane };
            break;
          }
        }
      }
      setDrag({ sourceID, placement });
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", cleanup);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("blur", cleanup);
      document.body.classList.remove("is-dragging-dock");
      setDrag(null);
      cleanupRef.current = null;
    };
    const onUp = (up: PointerEvent) => {
      if (up.pointerId !== pointerID) return;
      if (placement) changeRef.current(movePane(layoutRef.current, sourceID, placement.destinationID, placement.side));
      cleanup();
    };
    const onKey = (key: KeyboardEvent) => { if (key.key === "Escape") { key.preventDefault(); cleanup(); } };
    cleanupRef.current = cleanup;
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", cleanup);
    window.addEventListener("keydown", onKey);
    window.addEventListener("blur", cleanup);
  };

  const startResize = (split: SplitBounds, event: React.PointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    cleanupRef.current?.();
    const node = findPane(layoutRef.current, split.node.id);
    if (!node || node.type !== "split") return;
    // The split's total bounds come from its children's union, including nested views.
    const leaves = paneLeaves(node).map((pane) => bounds.panes.get(pane.id)!);
    const left = Math.min(...leaves.map((pane) => pane.left)), top = Math.min(...leaves.map((pane) => pane.top));
    const width = Math.max(...leaves.map((pane) => pane.left + pane.width)) - left;
    const height = Math.max(...leaves.map((pane) => pane.top + pane.height)) - top;
    const pointerID = event.pointerId;
    const move = (next: PointerEvent) => {
      if (next.pointerId !== pointerID) return;
      const rect = rootRef.current?.getBoundingClientRect();
      if (!rect) return;
      const raw = node.direction === "row" ? (next.clientX - rect.left - left) / width : (next.clientY - rect.top - top) / height;
      changeRef.current(resizeSplit(layoutRef.current, node.id, raw));
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", cleanup);
      window.removeEventListener("pointercancel", cleanup);
      window.removeEventListener("blur", cleanup);
      document.body.classList.remove("is-resizing-dock");
      document.body.style.removeProperty("--dock-resize-cursor");
      cleanupRef.current = null;
    };
    cleanupRef.current = cleanup;
    document.body.style.setProperty("--dock-resize-cursor", node.direction === "row" ? "col-resize" : "row-resize");
    document.body.classList.add("is-resizing-dock");
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", cleanup);
    window.addEventListener("pointercancel", cleanup);
    window.addEventListener("blur", cleanup);
  };

  let preview: PaneBounds | null = null;
  if (drag?.placement) {
    const { sourceID, destinationID, side } = drag.placement;
    const moved = movePane(layout, sourceID, destinationID, side);
    preview = layoutPaneBounds(moved, size.width, size.height, collapsed).panes.get(sourceID) || null;
  }
  return <DragContext.Provider value={{ startDrag, collapseSide: (id) => {
    const rect = bounds.panes.get(id);
    const parent = findPaneParent(layout, id);
    return rect && parent ? paneDockSide(rect, size.width, size.height, parent.direction) : null;
  } }}>
    <div ref={rootRef} className={`dock-workspace ${drag ? "dragging" : ""} ${fullscreenPaneID ? "dock-fullscreen" : ""}`}>
      {/* Keep DOM order stable as docking changes tree order; Monaco's input context must stay attached. */}
      {paneLeaves(layout).sort((a, b) => a.id.localeCompare(b.id)).map((pane) => {
        const paneBounds = fullscreenPaneID === pane.id ? { left: 0, top: 0, ...size } : bounds.panes.get(pane.id)!;
        return <div key={pane.id} className={`dock-view dock-view-${pane.type} ${collapsed.has(pane.id) ? `dock-collapsed ${paneBounds.width > paneBounds.height ? "dock-collapsed-horizontal" : ""}` : ""} ${drag?.sourceID === pane.id ? "drag-source" : ""}`} data-pane-id={pane.id} data-pane-type={pane.type} style={{ ...paneBounds, visibility: fullscreenPaneID && fullscreenPaneID !== pane.id ? "hidden" : undefined } as CSSProperties}>
          {renderPane(pane)}
        </div>;
      })}
      {!fullscreenPaneID && bounds.splits.map((split) => <button key={split.node.id} type="button" className={`dock-splitter ${split.node.direction}`} style={{ left: split.left, top: split.top, width: split.width, height: split.height }} role="separator" aria-orientation={split.node.direction === "row" ? "vertical" : "horizontal"} aria-label={t("connectResizeView")} aria-valuenow={Math.round(split.node.ratio * 100)} onPointerDown={(event) => startResize(split, event)} onKeyDown={(event) => {
        const delta = event.key === "ArrowLeft" || event.key === "ArrowUp" ? -0.025 : event.key === "ArrowRight" || event.key === "ArrowDown" ? 0.025 : 0;
        if (delta) { event.preventDefault(); onLayoutChange(resizeSplit(layout, split.node.id, split.node.ratio + delta)); }
      }} />)}
      {drag && <div className="dock-drag-overlay" />}
      {preview && drag?.placement && <div className="dock-drop-preview" style={preview}><span>{t(`connectDock${drag.placement.side}`)}{drag.placement.destinationID === null ? ` · ${t("connectEntireWorkspace")}` : ""}</span></div>}
    </div>
  </DragContext.Provider>;
}
