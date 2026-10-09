import {
  ChevronDown,
  ChevronRight,
  Folder,
  Globe,
  HardDrive,
  Search,
  Server,
} from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import type { ConsoleData, Target } from "../types";
import { tagColor, targetEndpoint } from "../utils";

const noAdditionalOptions: { value: string; label: string }[] = [];

export function MachinePicker({
  targets,
  folders,
  currentTargetID,
  openSignal = 0,
  label,
  variant = "icon",
  additionalOptions = noAdditionalOptions,
  onOpenTarget,
}: {
  targets: Target[];
  folders: ConsoleData["targetFolders"];
  currentTargetID: string;
  openSignal?: number;
  label?: string;
  variant?: "icon" | "field";
  additionalOptions?: { value: string; label: string }[];
  onOpenTarget: (targetID: string) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [collapsedFolders, setCollapsedFolders] = useState<Set<string>>(
    () => new Set(),
  );
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLElement>(null);
  const [position, setPosition] = useState<CSSProperties>({});
  const inputRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const currentTarget = targets.find((item) => item.id === currentTargetID);
  const currentOption = additionalOptions.find(
    (option) => option.value === currentTargetID,
  );
  const currentTitle = currentTarget
    ? serverTitle(currentTarget)
    : currentOption?.label || label || t("connectSwitchServer");
  const accessibleLabel = label || t("connectSwitchServer");
  const folderPathByTarget = useMemo(
    () =>
      Object.fromEntries(
        targets.map((item) => [item.id, targetFolderPath(item, folders)]),
      ),
    [targets, folders],
  );
  const filteredTargets = useMemo(() => {
    const text = query.trim().toLowerCase();
    if (!text) return targets;
    return targets.filter((item) =>
      [
        folderPathByTarget[item.id],
        folderPathByTarget[item.id]
          ? `${folderPathByTarget[item.id]}/${item.alias}`
          : item.alias,
        folderPathByTarget[item.id]
          ? `${folderPathByTarget[item.id]}/${item.name}`
          : item.name,
        item.name,
        item.alias,
        targetEndpoint(item),
        item.remote_username,
        ...(item.tags || []),
      ]
        .join(" ")
        .toLowerCase()
        .includes(text),
    );
  }, [folderPathByTarget, query, targets]);
  const toggleFolder = (folderID: string) => {
    setCollapsedFolders((current) => {
      const next = new Set(current);
      if (next.has(folderID)) next.delete(folderID);
      else next.add(folderID);
      return next;
    });
  };
  const treeItems = useMemo(
    () => [
      ...additionalOptions
        .filter(
          (option) =>
            !query.trim() ||
            option.label.toLowerCase().includes(query.trim().toLowerCase()),
        )
        .map((option) => ({
          type: "option" as const,
          id: option.value,
          name: option.label,
          depth: 0,
        })),
      ...buildSwitcherTree(
        filteredTargets,
        folders,
        folderPathByTarget,
        query.trim() ? new Set() : collapsedFolders,
        toggleFolder,
      ),
    ],
    [
      additionalOptions,
      collapsedFolders,
      filteredTargets,
      folderPathByTarget,
      folders,
      query,
    ],
  );

  useEffect(() => {
    if (openSignal <= 0) return;
    setOpen(true);
    setSelectedIndex(0);
    if (isMobileViewport()) blurActiveElement();
    else window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [openSignal]);

  useEffect(() => {
    if (!open) return;
    setSelectedIndex(0);
    if (isMobileViewport()) blurActiveElement();
    else window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  useEffect(() => {
    if (!treeItems.length) {
      setSelectedIndex(0);
      return;
    }
    setSelectedIndex((index) =>
      nextSelectableSwitcherIndex(
        treeItems,
        Math.max(0, Math.min(index, treeItems.length - 1)),
        1,
      ),
    );
  }, [treeItems]);

  useEffect(() => {
    if (!open) return;
    itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
  }, [open, selectedIndex]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (
        !rootRef.current?.contains(event.target as Node) &&
        !menuRef.current?.contains(event.target as Node)
      )
        setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const openTarget = (targetID: string) => {
    onOpenTarget(targetID);
    setOpen(false);
    setQuery("");
  };

  const onMenuKeyDown = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelectedIndex((index) =>
        nextSelectableSwitcherIndex(treeItems, index + 1, 1),
      );
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelectedIndex((index) =>
        nextSelectableSwitcherIndex(treeItems, index - 1, -1),
      );
    } else if (event.key === "Home") {
      event.preventDefault();
      setSelectedIndex(nextSelectableSwitcherIndex(treeItems, 0, 1));
    } else if (event.key === "End") {
      event.preventDefault();
      setSelectedIndex(
        nextSelectableSwitcherIndex(treeItems, treeItems.length - 1, -1),
      );
    } else if (event.key === "Enter") {
      const selected =
        treeItems[
          nextSelectableSwitcherIndex(
            treeItems,
            Math.max(0, Math.min(selectedIndex, treeItems.length - 1)),
            1,
          )
        ];
      if (!selected || selected.type === "folder") return;
      event.preventDefault();
      openTarget(selected.type === "target" ? selected.target.id : selected.id);
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
      if (variant === "field")
        rootRef.current?.querySelector("button")?.focus();
      else blurActiveElement();
    }
  };

  useLayoutEffect(() => {
    if (!open) return;
    const update = () => {
      const rect = rootRef.current?.getBoundingClientRect();
      if (!rect) return;
      const width = Math.min(420, window.innerWidth - 16);
      const below = window.innerHeight - rect.bottom;
      const above = below < 240 && rect.top > below;
      setPosition({
        "--picker-left": `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`,
        "--picker-width": `${width}px`,
        "--picker-top": above ? "auto" : `${rect.bottom + 8}px`,
        "--picker-bottom": above
          ? `${window.innerHeight - rect.top + 8}px`
          : "auto",
        "--picker-height": `${Math.max(120, Math.min(480, (above ? rect.top : below) - 24))}px`,
      } as CSSProperties);
    };
    update();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [open, variant]);
  const menu = open ? (
    <section
      ref={menuRef}
      style={position}
      className="server-switcher-menu machine-picker-menu"
      role="menu"
      aria-label={accessibleLabel}
      onKeyDown={onMenuKeyDown}
    >
      <label className="server-switcher-search">
        <Search />
        <input
          ref={inputRef}
          data-connect-switcher-search
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t("connectSwitchSearchPlaceholder")}
          aria-label={t("connectSwitchSearchPlaceholder")}
        />
      </label>
      <div className="server-switcher-list">
        {treeItems.map((treeItem, index) =>
          treeItem.type === "folder" ? (
            <button
              type="button"
              key={treeItem.id}
              className="server-switcher-folder"
              style={{ "--tree-depth": treeItem.depth } as CSSProperties}
              onClick={() => treeItem.onToggle()}
            >
              {treeItem.collapsed ? <ChevronRight /> : <ChevronDown />}
              <FolderIcon />
              <strong>{treeItem.name}</strong>
            </button>
          ) : treeItem.type === "option" ? (
            <button
              type="button"
              key={`option-${treeItem.id}`}
              ref={(element) => {
                itemRefs.current[index] = element;
              }}
              className={`server-switcher-item ${treeItem.id === currentTargetID ? "active" : ""} ${index === selectedIndex ? "selected" : ""}`}
              role="menuitem"
              onClick={() => openTarget(treeItem.id)}
              onPointerMove={() => setSelectedIndex(index)}
            >
              <span className="server-switcher-icon">
                <Globe />
              </span>
              <span className="server-switcher-main">
                <strong>{treeItem.name}</strong>
              </span>
            </button>
          ) : (
            <button
              type="button"
              key={treeItem.target.id}
              ref={(element) => {
                itemRefs.current[index] = element;
              }}
              className={`server-switcher-item ${treeItem.target.id === currentTargetID ? "active" : ""} ${index === selectedIndex ? "selected" : ""}`}
              style={{ "--tree-depth": treeItem.depth } as CSSProperties}
              onClick={() => openTarget(treeItem.target.id)}
              onPointerMove={() => setSelectedIndex(index)}
              role="menuitem"
              title={
                variant === "icon"
                  ? serverTitle(treeItem.target)
                  : treeItem.target.name
              }
            >
              <span className="server-switcher-icon">
                {treeItem.target.target_type === "agent" ? (
                  <Server />
                ) : (
                  <HardDrive />
                )}
              </span>
              <span className="server-switcher-main">
                <strong>{treeItem.target.name}</strong>
                <code>{treeItem.target.alias}</code>
                {variant === "icon" ? (
                  <small>{targetEndpoint(treeItem.target)}</small>
                ) : treeItem.target.host ? (
                  <small>{treeItem.target.host}</small>
                ) : null}
                {variant === "icon" && (
                  <span className="server-switcher-tags">
                    {(treeItem.target.tags || []).map((tag) => (
                      <span
                        key={tag}
                        className={`tag-chip tag-color-${tagColor(tag, treeItem.target.tag_colors)}`}
                      >
                        {tag}
                      </span>
                    ))}
                  </span>
                )}
              </span>
            </button>
          ),
        )}
        {!treeItems.length && (
          <div className="server-switcher-empty">{t("serviceEmptyTitle")}</div>
        )}
      </div>
    </section>
  ) : null;
  return (
    <div
      className={`server-switcher ${variant === "field" ? "machine-picker" : ""}`}
      ref={rootRef}
    >
      <button
        type="button"
        className={
          variant === "icon"
            ? `icon-button connect-server-switcher ${open ? "active" : ""}`
            : `machine-picker-trigger ${open ? "active" : ""}`
        }
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label={accessibleLabel}
        title={
          variant === "icon"
            ? currentTitle
            : currentTarget?.name || currentOption?.label || accessibleLabel
        }
      >
        {variant === "icon" ? (
          <Server />
        ) : (
          <>
            {currentOption ? (
              <Globe />
            ) : currentTarget?.target_type === "direct" ? (
              <HardDrive />
            ) : (
              <Server />
            )}
            <span>
              {currentTarget?.name || currentOption?.label || accessibleLabel}
            </span>
            <ChevronDown />
          </>
        )}
      </button>
      {menu ? createPortal(menu, document.body) : null}
    </div>
  );
}
type SwitcherTreeItem =
  | {
      type: "folder";
      id: string;
      name: string;
      depth: number;
      collapsed: boolean;
      onToggle: () => void;
    }
  | { type: "target"; target: Target; depth: number }
  | { type: "option"; id: string; name: string; depth: number };

function buildSwitcherTree(
  targets: Target[],
  folders: ConsoleData["targetFolders"],
  folderPathByTarget: Record<string, string>,
  collapsedFolders: Set<string>,
  onToggleFolder: (folderID: string) => void,
): SwitcherTreeItem[] {
  const out: SwitcherTreeItem[] = [];
  const targetIDs = new Set(targets.map((item) => item.id));
  const children = new Map<string, typeof folders>();
  for (const folder of folders) {
    const key = folder.parent_id || "";
    children.set(key, [...(children.get(key) || []), folder]);
  }
  const walkFolder = (parentID: string, depth: number) => {
    for (const folder of (children.get(parentID) || []).sort((a, b) =>
      a.name.localeCompare(b.name),
    )) {
      const folderPath = folderPathFromFolder(folder, folders);
      const descendants = targets.filter(
        (target) =>
          target.folder_id === folder.id ||
          folderPathByTarget[target.id]?.startsWith(`${folderPath}/`),
      );
      if (!descendants.length) continue;
      const collapsed = collapsedFolders.has(folder.id);
      out.push({
        type: "folder",
        id: folder.id,
        name: folder.name,
        depth,
        collapsed,
        onToggle: () => onToggleFolder(folder.id),
      });
      if (collapsed) continue;
      for (const target of targets
        .filter(
          (item) => item.folder_id === folder.id && targetIDs.has(item.id),
        )
        .sort((a, b) => a.name.localeCompare(b.name))) {
        out.push({ type: "target", target, depth: depth + 1 });
      }
      walkFolder(folder.id, depth + 1);
    }
  };
  for (const target of targets
    .filter((item) => !item.folder_id)
    .sort((a, b) => a.name.localeCompare(b.name))) {
    out.push({ type: "target", target, depth: 0 });
  }
  walkFolder("", 0);
  return out;
}

function nextSelectableSwitcherIndex(
  items: SwitcherTreeItem[],
  start: number,
  direction: 1 | -1,
) {
  if (!items.length) return 0;
  let index = wrapIndex(start, items.length);
  for (let i = 0; i < items.length; i += 1) {
    if (items[index]?.type !== "folder") return index;
    index = wrapIndex(index + direction, items.length);
  }
  return 0;
}

function FolderIcon() {
  return <Folder />;
}

export function serverTitle(target: Target) {
  const endpoint = targetEndpoint(target);
  const tags = (target.tags || []).join(", ");
  return [target.name, target.alias, endpoint, tags]
    .filter(Boolean)
    .join(" · ");
}

function targetFolderPath(
  target: Target,
  folders: ConsoleData["targetFolders"],
) {
  const byID = new Map(folders.map((folder) => [folder.id, folder]));
  const names: string[] = [];
  const seen = new Set<string>();
  for (let folderID = target.folder_id || ""; folderID; ) {
    if (seen.has(folderID)) break;
    seen.add(folderID);
    const folder = byID.get(folderID);
    if (!folder) break;
    names.unshift(folder.name);
    folderID = folder.parent_id || "";
  }
  return names.join("/");
}

function folderPathFromFolder(
  folder: ConsoleData["targetFolders"][number],
  folders: ConsoleData["targetFolders"],
) {
  const byID = new Map(folders.map((item) => [item.id, item]));
  const names: string[] = [];
  const seen = new Set<string>();
  for (let current: typeof folder | undefined = folder; current; ) {
    if (seen.has(current.id)) break;
    seen.add(current.id);
    names.unshift(current.name);
    current = current.parent_id ? byID.get(current.parent_id) : undefined;
  }
  return names.join("/");
}

function wrapIndex(value: number, length: number) {
  return length <= 0 ? 0 : (value + length) % length;
}
function isMobileViewport() {
  return window.matchMedia("(max-width:760px)").matches;
}
function blurActiveElement() {
  if (document.activeElement instanceof HTMLElement)
    document.activeElement.blur();
}
