import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, MouseEvent, PointerEvent } from "react";
import { applySelection, intersectsSelection, selectEntry, selectionMode, selectionRect } from "../fileSelection";
import type { SelectionRect } from "../fileSelection";

export function useFileSelection(scope: string, paths: string[]) {
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [rectangle, setRectangle] = useState<SelectionRect | null>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const anchorRef = useRef<string | null>(null);
  const cleanupRef = useRef<(() => void) | null>(null);
  const suppressClickRef = useRef(false);

  useEffect(() => {
    cleanupRef.current?.();
    setSelectedPaths(new Set());
    anchorRef.current = null;
  }, [scope]);
  const pathsKey = JSON.stringify(paths);
  useEffect(() => {
    const visible = new Set(paths);
    setSelectedPaths((current) => new Set([...current].filter((path) => visible.has(path))));
    if (anchorRef.current && !visible.has(anchorRef.current)) anchorRef.current = null;
  }, [pathsKey]);
  useEffect(() => () => cleanupRef.current?.(), []);

  const click = (path: string, event: MouseEvent) => {
    if (suppressClickRef.current) { suppressClickRef.current = false; return; }
    setSelectedPaths((current) => selectEntry(current, paths, anchorRef.current, path, event));
    if (!event.shiftKey || !anchorRef.current) anchorRef.current = path;
  };
  const selectOnly = (path: string) => {
    setSelectedPaths(new Set([path]));
    anchorRef.current = path;
  };
  const contextSelect = (path: string) => {
    if (!selectedPaths.has(path)) selectOnly(path);
  };
  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    suppressClickRef.current = false;
    if (event.pointerType === "touch") return;
    const element = event.target as HTMLElement;
    if (element.closest("thead, input, .file-parent-row")) return;
    const body = event.currentTarget;
    const row = element.closest("[data-file-path]");
    // Start rubber-band selection in the blank area or non-interactive row cells.
    if (element.closest("button") && row) return;
    cleanupRef.current?.();
    event.preventDefault();
    body.focus({ preventScroll: true });
    const startBounds = body.getBoundingClientRect();
    const startX = event.clientX - startBounds.left + body.scrollLeft;
    const startY = event.clientY - startBounds.top + body.scrollTop;
    const base = new Set(selectedPaths), mode = selectionMode(event), pointerID = event.pointerId;
    let dragging = false, lastX = event.clientX, lastY = event.clientY, frame = 0;
    const update = () => {
      const bounds = body.getBoundingClientRect();
      const x = Math.max(0, Math.min(bounds.width, lastX - bounds.left)) + body.scrollLeft;
      const y = Math.max(0, Math.min(bounds.height, lastY - bounds.top)) + body.scrollTop;
      const rect = selectionRect(startX, startY, x, y);
      setRectangle(rect);
      const hits = [...body.querySelectorAll<HTMLElement>("[data-file-path]")].filter((item) => {
        const r = item.getBoundingClientRect();
        return intersectsSelection(rect, { left: r.left - bounds.left + body.scrollLeft, top: r.top - bounds.top + body.scrollTop, width: r.width, height: r.height });
      }).map((item) => item.dataset.filePath!);
      setSelectedPaths(applySelection(base, hits, mode));
    };
    const scroll = () => {
      if (!dragging) return;
      const bounds = body.getBoundingClientRect();
      const delta = lastY < bounds.top + 24 ? -12 : lastY > bounds.bottom - 24 ? 12 : 0;
      if (delta) { body.scrollTop += delta; update(); }
      frame = requestAnimationFrame(scroll);
    };
    const move = (e: globalThis.PointerEvent) => {
      if (e.pointerId !== pointerID) return;
      lastX = e.clientX; lastY = e.clientY;
      if (!dragging && Math.hypot(lastX - event.clientX, lastY - event.clientY) < 5) return;
      if (!dragging) { dragging = true; suppressClickRef.current = true; frame = requestAnimationFrame(scroll); }
      update();
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("keydown", escape);
      window.removeEventListener("blur", cancel);
      cancelAnimationFrame(frame);
      setRectangle(null);
      cleanupRef.current = null;
    };
    const finish = (e: globalThis.PointerEvent) => {
      if (e.pointerId !== pointerID) return;
      if (!dragging && !row && mode === "replace") { setSelectedPaths(new Set()); anchorRef.current = null; }
      cleanup();
    };
    const cancel = () => { setSelectedPaths(base); cleanup(); };
    const escape = (e: globalThis.KeyboardEvent) => { if (e.key === "Escape") { e.preventDefault(); cancel(); } };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", finish);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("keydown", escape);
    window.addEventListener("blur", cancel);
    cleanupRef.current = cleanup;
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "a") {
      event.preventDefault(); setSelectedPaths(new Set(paths));
    } else if (event.key === "Escape" && !cleanupRef.current) {
      setSelectedPaths(new Set()); anchorRef.current = null;
    }
  };
  return { selectedPaths, rectangle, bodyRef, click, selectOnly, contextSelect, onPointerDown, onKeyDown, suppressClickRef };
}
