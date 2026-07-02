import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Copy, Download, Edit3, ExternalLink, FilePlus, FolderOpen, FolderPlus, HardDrive, Info, Move, RefreshCw, Search, Trash2, Upload } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api";
import { Modal, ModalActions } from "../components/ui";
import { useI18n } from "../i18n";
import type { FileEntry, FileProperties, Target, TargetSystemFilesystem, TargetSystemSnapshot } from "../types";
import { copyText } from "../utils";

type FileSortKey = "name" | "size" | "mode" | "modified";
type SortOrder = "asc" | "desc";
type BreadcrumbItem = { key: string; label: string; kind: "drives" | "dirs"; menuPath: string };
type BreadcrumbMenuState = { kind: "drives" | "dirs"; path: string; left: number; top: number; width: number };

export function FileManager({ target, system, nativeOpen = false, onEditFile }: { target: Target; system?: TargetSystemSnapshot; nativeOpen?: boolean; onEditFile?: (path: string) => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [path, setPath] = useState(".");
  const [pathDraft, setPathDraft] = useState(".");
  const [selected, setSelected] = useState<FileEntry | null>(null);
  const [uploading, setUploading] = useState(false);
  const [mkdirModal, setMkdirModal] = useState(false);
  const [touchModal, setTouchModal] = useState(false);
  const [pathEditing, setPathEditing] = useState(false);
  const [sort, setSort] = useState<{ key: FileSortKey; order: SortOrder }>({ key: "name", order: "asc" });
  const [transfer, setTransfer] = useState<{ action: "move" | "copy"; entry: FileEntry } | null>(null);
  const [properties, setProperties] = useState<FileProperties | null>(null);
  const [contextMenu, setContextMenu] = useState<{ entry: FileEntry | null; x: number; y: number; left: number; top: number } | null>(null);
  const [crumbMenu, setCrumbMenu] = useState<BreadcrumbMenuState | null>(null);
  const [crumbFilter, setCrumbFilter] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const contextMenuRef = useRef<HTMLDivElement>(null);
  const pathInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setPathDraft(path);
  }, [path]);

  useEffect(() => {
    setPath(".");
    setPathDraft(".");
    setSelected(null);
    setTransfer(null);
    setProperties(null);
    setContextMenu(null);
    setCrumbMenu(null);
    setPathEditing(false);
  }, [target.id]);

  useEffect(() => {
    if (pathEditing) pathInputRef.current?.focus();
  }, [pathEditing]);

  useLayoutEffect(() => {
    if (!contextMenu) return;
    const menu = contextMenuRef.current;
    if (!menu) return;
    const rect = menu.getBoundingClientRect();
    const next = contextMenuPositionInViewport(contextMenu.x, contextMenu.y, rect.width, rect.height);
    if (next.left === contextMenu.left && next.top === contextMenu.top) return;
    setContextMenu((current) => current && current.x === contextMenu.x && current.y === contextMenu.y ? { ...current, ...next } : current);
  }, [contextMenu, selected?.path, selected?.type, uploading, nativeOpen]);

  useEffect(() => {
    if (!contextMenu) return;
    const close = () => setContextMenu(null);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("pointerdown", close);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", close);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [contextMenu]);

  useEffect(() => {
    if (!crumbMenu) return;
    const close = () => setCrumbMenu(null);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("pointerdown", close);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", close);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [crumbMenu]);

  const listing = useQuery({
    queryKey: ["target-files", target.id, path, sort.key, sort.order],
    queryFn: () => api.listFiles(target.id, path, sort.key, sort.order),
  });
  const breadcrumbListing = useQuery({
    queryKey: ["target-files", target.id, crumbMenu?.path || "", "name", "asc", "breadcrumb"],
    queryFn: () => api.listFiles(target.id, crumbMenu?.path || ".", "name", "asc"),
    enabled: crumbMenu?.kind === "dirs",
  });

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadFile(target.id, path, file),
    onMutate: () => setUploading(true),
    onSettled: () => {
      setUploading(false);
      void queryClient.invalidateQueries({ queryKey: ["target-files", target.id, path] });
    },
  });

  const openNative = useMutation({
    mutationFn: (entry: FileEntry) => api.openFile(target.id, entry.path),
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });
  const refreshFiles = async () => {
    await queryClient.invalidateQueries({ queryKey: ["target-files", target.id] });
  };
  const mkdir = useMutation({
    mutationFn: (nextPath: string) => api.mkdirFile(target.id, nextPath),
    onSuccess: refreshFiles,
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });
  const touch = useMutation({
    mutationFn: (nextPath: string) => api.touchFile(target.id, nextPath),
    onSuccess: refreshFiles,
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });
  const remove = useMutation({
    mutationFn: (entry: FileEntry) => api.deleteFile(target.id, entry.path),
    onSuccess: refreshFiles,
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });
  const move = useMutation({
    mutationFn: ({ entry, destination }: { entry: FileEntry; destination: string }) => api.moveFile(target.id, entry.path, destination),
    onSuccess: refreshFiles,
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });
  const copy = useMutation({
    mutationFn: ({ entry, destination }: { entry: FileEntry; destination: string }) => api.copyFile(target.id, entry.path, destination),
    onSuccess: refreshFiles,
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });
  const stat = useMutation({
    mutationFn: (entry: FileEntry) => api.fileProperties(target.id, entry.path),
    onSuccess: setProperties,
    onError: (error) => window.alert(error instanceof Error ? error.message : String(error)),
  });

  const entries = listing.data?.entries || [];
  const drives = system?.os === "windows" ? windowsDrives(system.filesystems || []) : [];
  const breadcrumbDirectories = (breadcrumbListing.data?.entries || []).filter((entry) => entry.type === "dir" && entry.name !== "." && entry.name !== "..");
  const canOpenParent = path !== "/" && !isWindowsDriveRoot(path);

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    upload.mutate(file);
    event.target.value = "";
  };

  const activateEntry = (entry: FileEntry) => {
    if (entry.type === "dir") {
      setPath(entry.path);
      setSelected(null);
    } else {
      setSelected(entry);
      if (nativeOpen) {
        openNative.mutate(entry);
      } else {
        downloadEntry(entry);
      }
    }
  };

  const downloadEntry = (entry: FileEntry) => {
    const anchor = document.createElement("a");
    anchor.href = api.downloadFile(target.id, entry.path);
    anchor.download = entry.name;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  };

  const copyEntryPath = async (entry: FileEntry | null) => {
    await copyText(entry?.path || path);
  };

  const handleMenuAction = async (action: "open" | "download" | "edit" | "copy-path" | "refresh" | "upload" | "mkdir" | "touch" | "delete" | "properties" | "move" | "copy", entry: FileEntry | null) => {
    if (action === "open" && entry) {
      activateEntry(entry);
    } else if (action === "download" && entry && entry.type === "file") {
      downloadEntry(entry);
    } else if (action === "edit" && entry && entry.type === "file") {
      onEditFile?.(entry.path);
    } else if (action === "copy-path") {
      await copyEntryPath(entry);
    } else if (action === "refresh") {
      await listing.refetch();
    } else if (action === "upload") {
      fileInputRef.current?.click();
    } else if (action === "mkdir") {
      setMkdirModal(true);
    } else if (action === "touch") {
      setTouchModal(true);
    } else if (action === "delete" && entry) {
      if (window.confirm(t("connectFileDeleteConfirm", `Delete ${entry.path}?`))) {
        remove.mutate(entry);
      }
    } else if (action === "properties" && entry) {
      stat.mutate(entry);
    } else if ((action === "move" || action === "copy") && entry) {
      setTransfer({ action, entry });
    }
  };

  const openFileMenu = (entry: FileEntry | null, event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    setSelected(entry);
    setContextMenu({ entry, x: event.clientX, y: event.clientY, left: event.clientX, top: event.clientY });
  };

  const runMenuAction = (action: "open" | "download" | "edit" | "copy-path" | "refresh" | "upload" | "mkdir" | "touch" | "delete" | "properties" | "move" | "copy", entry: FileEntry | null) => {
    setContextMenu(null);
    void handleMenuAction(action, entry);
  };

  const changeSort = (key: FileSortKey) => {
    setSort((current) => current.key === key ? { key, order: current.order === "asc" ? "desc" : "asc" } : { key, order: "asc" });
  };

  const submitMkdir = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    if (!name) return;
    mkdir.mutate(remoteJoin(path, name), { onSuccess: () => setMkdirModal(false) });
  };

  const submitTouch = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    if (!name) return;
    touch.mutate(remoteJoin(path, name), { onSuccess: () => setTouchModal(false) });
  };

  const submitPath = () => {
    const nextPath = normalizeRemotePath(pathDraft.trim());
    if (!nextPath || nextPath === path) {
      setPathDraft(path);
      setPathEditing(false);
      return;
    }
    setPath(nextPath);
    setSelected(null);
    setPathEditing(false);
  };

  const fileMenu = (entry: FileEntry | null) => contextMenu ? createPortal(
    <div
      ref={contextMenuRef}
      className="file-context-menu"
      style={{ left: contextMenu.left, top: contextMenu.top }}
      role="menu"
      onPointerDown={(event) => event.stopPropagation()}
      onContextMenu={(event) => {
        event.preventDefault();
        event.stopPropagation();
      }}
    >
        {entry?.type === "dir" && (
          <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("open", entry)}>
            <FolderOpen />{t("connectFileOpenDir")}
          </button>
        )}
        {entry?.type === "file" && nativeOpen && (
          <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("open", entry)}>
            <ExternalLink />{t("connectFileOpenDir")}
          </button>
        )}
        {entry?.type === "file" && (
          <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("download", entry)}>
            <Download />{t("connectFileDownload")}
          </button>
        )}
        {entry?.type === "file" && onEditFile && (
          <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("edit", entry)}>
            <Edit3 />{t("connectFileEdit")}
          </button>
        )}
        <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("mkdir", entry)}>
          <FolderPlus />{t("connectFileNewFolder")}
        </button>
        <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("touch", entry)}>
          <FilePlus />{t("connectFileNewFile")}
        </button>
        {entry && (
          <>
            <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("move", entry)}>
              <Move />{t("connectFileMove")}
            </button>
            <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("copy", entry)}>
              <Copy />{t("connectFileCopy")}
            </button>
            <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("properties", entry)}>
              <Info />{t("connectFileProperties")}
            </button>
            <button type="button" role="menuitem" className="file-context-menu-item danger" onClick={() => runMenuAction("delete", entry)}>
              <Trash2 />{t("commonDelete")}
            </button>
            <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("copy-path", entry)}>
              <Copy />{t("connectFileCopyPath")}
            </button>
          </>
        )}
        <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("refresh", entry)}>
          <RefreshCw />{t("commonRefresh")}
        </button>
        {!entry && (
          <button type="button" role="menuitem" className="file-context-menu-item" disabled={uploading} onClick={() => runMenuAction("upload", entry)}>
            <Upload />{uploading ? t("connectFileUploading") : t("connectFileUpload")}
          </button>
        )}
    </div>,
    document.body
  ) : null;

  const breadcrumbMenu = crumbMenu ? createPortal(
    <div
      className="file-breadcrumb-menu"
      style={{ left: crumbMenu.left, top: crumbMenu.top, minWidth: crumbMenu.width }}
      onPointerDown={(event) => event.stopPropagation()}
    >
      <label className="file-breadcrumb-search">
        <Search />
        <input value={crumbFilter} onChange={(event) => setCrumbFilter(event.target.value)} autoFocus />
      </label>
      <div className="file-breadcrumb-options">
        {crumbMenu.kind === "drives" ? (
          filterDrives(drives, crumbFilter).map((drive) => (
            <button key={drive.path} type="button" onClick={() => {
              setPath(windowsDriveRoot(drive.path));
              setSelected(null);
              setCrumbMenu(null);
            }}>
              <HardDrive />{drive.path}
            </button>
          ))
        ) : (
          filterEntries(breadcrumbDirectories, crumbFilter).map((entry) => (
            <button key={entry.path} type="button" onClick={() => {
              setPath(entry.path);
              setSelected(null);
              setCrumbMenu(null);
            }}>
              <FolderOpen />{entry.name}
            </button>
          ))
        )}
        {crumbMenu.kind === "dirs" && breadcrumbListing.isLoading && <div className="file-breadcrumb-empty">{t("loading")}</div>}
        {crumbMenu.kind === "dirs" && !breadcrumbListing.isLoading && !filterEntries(breadcrumbDirectories, crumbFilter).length && <div className="file-breadcrumb-empty">{t("connectFileEmpty")}</div>}
        {crumbMenu.kind === "drives" && !filterDrives(drives, crumbFilter).length && <div className="file-breadcrumb-empty">{t("connectSystemNoData")}</div>}
      </div>
    </div>,
    document.body
  ) : null;

  const openBreadcrumbMenu = (item: BreadcrumbItem, event: React.MouseEvent<HTMLElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    setCrumbFilter("");
    setCrumbMenu({
      kind: item.kind,
      path: item.menuPath,
      left: rect.left,
      top: rect.bottom + 4,
      width: Math.max(180, rect.width),
    });
  };

  return (
    <section className="file-manager">
      <header className="file-manager-head">
        <div className="file-manager-path" title={path} onDoubleClick={() => setPathEditing(true)}>
          <HardDrive />
          {pathEditing ? (
            <input
              ref={pathInputRef}
              value={pathDraft}
              onChange={(event) => setPathDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  submitPath();
                } else if (event.key === "Escape") {
                  setPathDraft(path);
                  setPathEditing(false);
                }
              }}
              onBlur={() => {
                setPathDraft(path);
                setPathEditing(false);
              }}
              aria-label="File path"
            />
          ) : (
            <PathBreadcrumb path={path} drives={drives} driveLabel={t("connectSystemFilesystems")} onOpenMenu={openBreadcrumbMenu} />
          )}
        </div>
        <div className="file-manager-actions">
          <button type="button" className="icon-button" onClick={() => listing.refetch()} disabled={listing.isFetching} title={t("commonRefresh")}>
            <RefreshCw />
          </button>
          <input
            ref={fileInputRef}
            type="file"
            className="file-upload-input"
            onChange={handleFileChange}
            disabled={uploading}
          />
          <button type="button" onClick={() => fileInputRef.current?.click()} disabled={uploading} title={uploading ? t("connectFileUploading") : t("connectFileUpload")}>
            <Upload />{uploading ? t("connectFileUploading") : t("connectFileUpload")}
          </button>
        </div>
      </header>
      {breadcrumbMenu}
      <div className="file-manager-body" onContextMenu={(event) => openFileMenu(null, event)}>
        <table>
          <thead>
            <tr>
              <th><SortButton active={sort.key === "name"} order={sort.order} onClick={() => changeSort("name")}>{t("connectFileName")}</SortButton></th>
              <th><SortButton active={sort.key === "size"} order={sort.order} onClick={() => changeSort("size")}>{t("connectFileSize")}</SortButton></th>
              <th><SortButton active={sort.key === "mode"} order={sort.order} onClick={() => changeSort("mode")}>{t("connectFileMode")}</SortButton></th>
              <th><SortButton active={sort.key === "modified"} order={sort.order} onClick={() => changeSort("modified")}>{t("connectFileModified")}</SortButton></th>
            </tr>
          </thead>
          <tbody>
            {canOpenParent && (
              <tr className="file-row directory">
                <td>
                  <button type="button" className="file-name" onClick={() => setPath(remoteParent(path))} onDoubleClick={() => setPath(remoteParent(path))}>
                    <FolderOpen />{t("connectFileParentDir")}
                  </button>
                </td>
                <td>-</td><td>-</td><td>-</td>
              </tr>
            )}
            {entries.map((entry) => (
              <tr
                key={entry.path}
                className={`file-row ${entry.type} ${selected?.path === entry.path ? "selected" : ""}`}
                onClick={() => entry.type === "dir" ? activateEntry(entry) : setSelected(entry)}
                onDoubleClick={() => activateEntry(entry)}
                onContextMenu={(event) => openFileMenu(entry, event)}
              >
                  <td>
                    <button type="button" className="file-name" onClick={() => entry.type === "dir" ? activateEntry(entry) : setSelected(entry)} onDoubleClick={() => activateEntry(entry)} title={entry.name}>
                      {entry.type === "dir" ? <FolderOpen /> : <HardDrive />}
                      <span>{entry.name}</span>
                    </button>
                  </td>
                  <td>{entry.type === "dir" ? "-" : formatBytes(entry.size)}</td>
                  <td><code>{entry.mode}</code></td>
                  <td>{formatDate(entry.modified_at)}</td>
                </tr>
            ))}
            {!entries.length && (
              <tr>
                <td colSpan={4} className="file-empty">
                  {listing.isLoading ? t("loading") : t("connectFileEmpty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {contextMenu && fileMenu(contextMenu.entry)}
      {mkdirModal && (
        <Modal title={t("connectFileNewFolder")} onClose={() => setMkdirModal(false)}>
          <form className="stack" onSubmit={submitMkdir}>
            <label className="field">
              <span>{t("connectFileFolderName")}</span>
              <input name="name" autoFocus required />
            </label>
            <ModalActions onCancel={() => setMkdirModal(false)} submit={t("connectFileNewFolder")} />
          </form>
        </Modal>
      )}
      {touchModal && (
        <Modal title={t("connectFileNewFile")} onClose={() => setTouchModal(false)}>
          <form className="stack" onSubmit={submitTouch}>
            <label className="field">
              <span>{t("connectFileFileName")}</span>
              <input name="name" autoFocus required />
            </label>
            <ModalActions onCancel={() => setTouchModal(false)} submit={t("connectFileNewFile")} />
          </form>
        </Modal>
      )}
      {transfer && (
        <TransferModal
          target={target}
          transfer={transfer}
          initialDir={path}
          initialSort={sort}
          onClose={() => setTransfer(null)}
          onSubmit={(entry, destination) => {
            const mutation = transfer.action === "move" ? move : copy;
            mutation.mutate({ entry, destination }, { onSuccess: () => setTransfer(null) });
          }}
        />
      )}
      {properties && (
        <Modal title={t("connectFileProperties")} onClose={() => setProperties(null)}>
          <dl className="file-properties">
            <div><dt>{t("connectFileName")}</dt><dd>{properties.name}</dd></div>
            <div><dt>{t("connectFilePath")}</dt><dd>{properties.path}</dd></div>
            <div><dt>{t("connectFileType")}</dt><dd>{properties.type}</dd></div>
            <div><dt>{t("connectFileSize")}</dt><dd>{formatBytes(properties.size)}</dd></div>
            <div><dt>{t("connectFileDiskUsage")}</dt><dd>{formatBytes(properties.disk_usage)}</dd></div>
            <div><dt>{t("connectFileItems")}</dt><dd>{properties.items}</dd></div>
            <div><dt>{t("connectFileMode")}</dt><dd><code>{properties.mode}</code></dd></div>
            <div><dt>{t("connectFileModified")}</dt><dd>{formatDate(properties.modified_at)}</dd></div>
          </dl>
        </Modal>
      )}
    </section>
  );
}

function SortButton({ active, order, onClick, children }: { active: boolean; order: SortOrder; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" className={`file-sort-button ${active ? "active" : ""}`} onClick={onClick}>
      <span>{children}</span>
      <small>{active ? (order === "asc" ? "▲" : "▼") : ""}</small>
    </button>
  );
}

function PathBreadcrumb({ path, drives, driveLabel, onOpenMenu }: { path: string; drives: TargetSystemFilesystem[]; driveLabel: string; onOpenMenu: (item: BreadcrumbItem, event: React.MouseEvent<HTMLElement>) => void }) {
  const items = breadcrumbItems(path, drives, driveLabel);
  return (
    <nav className="file-breadcrumb" aria-label="File path">
      {items.map((item, index) => (
        <span key={item.key} className="file-breadcrumb-part">
          {index > 0 && <ChevronRight />}
          <button type="button" onClick={(event) => onOpenMenu(item, event)} title={item.label}>
            {item.label}
          </button>
        </span>
      ))}
    </nav>
  );
}

function breadcrumbItems(path: string, drives: TargetSystemFilesystem[], driveLabel: string): BreadcrumbItem[] {
  const normalized = normalizeRemotePath(path || ".");
  const driveMatch = normalized.match(/^([A-Za-z]:)(?:\/(.*))?$/);
  if (driveMatch) {
    const drive = `${driveMatch[1].toUpperCase()}/`;
    const items: BreadcrumbItem[] = [{ key: drive, label: driveMatch[1].toUpperCase(), kind: drives.length ? "drives" : "dirs", menuPath: drive }];
    let current = drive;
    for (const segment of (driveMatch[2] || "").split("/").filter(Boolean)) {
      const parent = current;
      current = remoteJoin(current, segment);
      items.push({ key: current, label: segment, kind: "dirs", menuPath: parent });
    }
    return items;
  }
  if (drives.length) {
    return [{ key: "drives", label: driveLabel, kind: "drives", menuPath: "" }];
  }
  if (normalized.startsWith("/")) {
    const items: BreadcrumbItem[] = [{ key: "/", label: "/", kind: "dirs", menuPath: "/" }];
    let current = "/";
    for (const segment of normalized.split("/").filter(Boolean)) {
      const parent = current;
      current = remoteJoin(current, segment);
      items.push({ key: current, label: segment, kind: "dirs", menuPath: parent });
    }
    return items;
  }
  const parts = normalized.split("/").filter(Boolean);
  if (!parts.length || normalized === ".") {
    return [{ key: ".", label: ".", kind: "dirs", menuPath: "." }];
  }
  const items: BreadcrumbItem[] = [];
  let current = "";
  for (const segment of parts) {
    const parent = current || ".";
    current = current ? remoteJoin(current, segment) : segment;
    items.push({ key: current, label: segment, kind: "dirs", menuPath: parent });
  }
  return items;
}

function windowsDrives(filesystems: TargetSystemFilesystem[]) {
  return filesystems
    .filter((item) => /^[A-Za-z]:\\?$/.test(item.path.trim()))
    .sort((a, b) => a.path.localeCompare(b.path));
}

function windowsDriveRoot(path: string) {
  const match = path.trim().match(/^([A-Za-z]):/);
  return match ? `${match[1].toUpperCase()}:/` : path;
}

function isWindowsDriveRoot(path: string) {
  return /^[A-Za-z]:[\/\\]?$/.test(path.trim());
}

function filterDrives(drives: TargetSystemFilesystem[], filter: string) {
  const needle = filter.trim().toLowerCase();
  return needle ? drives.filter((drive) => drive.path.toLowerCase().includes(needle)) : drives;
}

function filterEntries(entries: FileEntry[], filter: string) {
  const needle = filter.trim().toLowerCase();
  return needle ? entries.filter((entry) => entry.name.toLowerCase().includes(needle)) : entries;
}

function TransferModal({
  target,
  transfer,
  initialDir,
  initialSort,
  onClose,
  onSubmit,
}: {
  target: Target;
  transfer: { action: "move" | "copy"; entry: FileEntry };
  initialDir: string;
  initialSort: { key: FileSortKey; order: SortOrder };
  onClose: () => void;
  onSubmit: (entry: FileEntry, destination: string) => void;
}) {
  const { t } = useI18n();
  const [browsePath, setBrowsePath] = useState(initialDir || ".");
  const [browseDraft, setBrowseDraft] = useState(initialDir || ".");
  const [destination, setDestination] = useState(remoteJoin(initialDir || ".", transfer.entry.name));
  const [sort, setSort] = useState<{ key: FileSortKey; order: SortOrder }>(initialSort);
  const listing = useQuery({
    queryKey: ["target-files", target.id, browsePath, sort.key, sort.order, "transfer"],
    queryFn: () => api.listFiles(target.id, browsePath, sort.key, sort.order),
  });
  const directories = (listing.data?.entries || []).filter((entry) => entry.type === "dir");
  const canOpenParent = browsePath !== "/" && !isWindowsDriveRoot(browsePath);

  useEffect(() => {
    setBrowsePath(initialDir || ".");
    setBrowseDraft(initialDir || ".");
    setDestination(remoteJoin(initialDir || ".", transfer.entry.name));
  }, [initialDir, transfer.entry.name, transfer.entry.path]);

  useEffect(() => {
    setBrowseDraft(browsePath);
    setDestination(remoteJoin(browsePath, transfer.entry.name));
  }, [browsePath, transfer.entry.name]);

  const submitBrowsePath = () => {
    const nextPath = browseDraft.trim();
    if (!nextPath || nextPath === browsePath) {
      setBrowseDraft(browsePath);
      return;
    }
    setBrowsePath(nextPath);
  };

  const submitTransfer = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const nextDestination = destination.trim();
    if (!nextDestination) return;
    onSubmit(transfer.entry, nextDestination);
  };

  const changeSort = (key: FileSortKey) => {
    setSort((current) => current.key === key ? { key, order: current.order === "asc" ? "desc" : "asc" } : { key, order: "asc" });
  };

  return (
    <Modal title={transfer.action === "move" ? t("connectFileMove") : t("connectFileCopy")} onClose={onClose} wide>
      <form className="stack transfer-modal" onSubmit={submitTransfer}>
        <p className="muted">{transfer.entry.path}</p>
        <label className="field">
          <span>{t("connectFileDestination")}</span>
          <input value={destination} onChange={(event) => setDestination(event.target.value)} autoFocus required />
        </label>
        <div className="transfer-browser">
          <div className="file-manager-head">
            <div className="file-manager-path" title={browsePath}>
              <HardDrive />
              <input
                value={browseDraft}
                onChange={(event) => setBrowseDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    submitBrowsePath();
                  } else if (event.key === "Escape") {
                    setBrowseDraft(browsePath);
                  }
                }}
                onBlur={() => setBrowseDraft(browsePath)}
                aria-label="Transfer browser path"
              />
            </div>
            <div className="file-manager-actions">
              <button type="button" className="icon-button" onClick={() => listing.refetch()} disabled={listing.isFetching} title={t("commonRefresh")}>
                <RefreshCw />
              </button>
            </div>
          </div>
          <div className="file-manager-body transfer-browser-body">
            <table>
              <thead>
                <tr>
                  <th><SortButton active={sort.key === "name"} order={sort.order} onClick={() => changeSort("name")}>{t("connectFileName")}</SortButton></th>
                  <th><SortButton active={sort.key === "size"} order={sort.order} onClick={() => changeSort("size")}>{t("connectFileSize")}</SortButton></th>
                  <th><SortButton active={sort.key === "mode"} order={sort.order} onClick={() => changeSort("mode")}>{t("connectFileMode")}</SortButton></th>
                  <th><SortButton active={sort.key === "modified"} order={sort.order} onClick={() => changeSort("modified")}>{t("connectFileModified")}</SortButton></th>
                </tr>
              </thead>
              <tbody>
                {canOpenParent && (
                  <tr className="file-row directory">
                    <td>
                      <button type="button" className="file-name" onClick={() => setBrowsePath(remoteParent(browsePath))}>
                        <FolderOpen />{t("connectFileParentDir")}
                      </button>
                    </td>
                    <td>-</td><td>-</td><td>-</td>
                  </tr>
                )}
                {directories.map((entry) => (
                  <tr key={entry.path} className="file-row directory">
                    <td>
                      <button type="button" className="file-name" onClick={() => setBrowsePath(entry.path)} title={entry.name}>
                        <FolderOpen />
                        <span>{entry.name}</span>
                      </button>
                    </td>
                    <td>-</td>
                    <td><code>{entry.mode}</code></td>
                    <td>{formatDate(entry.modified_at)}</td>
                  </tr>
                ))}
                {!directories.length && (
                  <tr>
                    <td colSpan={4} className="file-empty">{listing.isLoading ? t("loading") : t("connectFileEmpty")}</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
        <ModalActions onCancel={onClose} submit={transfer.action === "move" ? t("connectFileMove") : t("connectFileCopy")} />
      </form>
    </Modal>
  );
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatDate(value?: string) {
  if (!value || value === "-" || value.trim() === "") return "-";
  try {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "-";
    return date.toLocaleString();
  } catch {
    return "-";
  }
}

function contextMenuPositionInViewport(x: number, y: number, width: number, height: number) {
  const margin = 8;
  return {
    left: clampNumber(x, margin, Math.max(margin, window.innerWidth - width - margin)),
    top: clampNumber(y, margin, Math.max(margin, window.innerHeight - height - margin)),
  };
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function remoteJoin(dir: string, name: string) {
  const trimmedName = name.trim();
  if (!trimmedName) return dir;
  if (/^[A-Za-z]:[/\\]?/.test(trimmedName)) return normalizeRemotePath(trimmedName.replace(/\\/g, "/"));
  if (trimmedName.startsWith("/")) return normalizeRemotePath(trimmedName);
  if (!dir || dir === ".") return normalizeRemotePath(trimmedName);
  return normalizeRemotePath(`${dir.replace(/\/+$/, "")}/${trimmedName}`);
}

function remoteParent(path: string) {
  const trimmed = path.replace(/\\/g, "/").replace(/\/$/, "");
  if (/^[A-Za-z]:$/.test(trimmed)) return `${trimmed.toUpperCase()}/`;
  const index = trimmed.lastIndexOf("/");
  if (index === 2 && /^[A-Za-z]:/.test(trimmed)) return `${trimmed.slice(0, 2).toUpperCase()}/`;
  if (index <= 0) return "/";
  return trimmed.slice(0, index) || "/";
}

function normalizeRemotePath(value: string): string {
  value = value.replace(/\\/g, "/");
  const drive = value.match(/^([A-Za-z]:)(?:\/|$)(.*)$/);
  if (drive) {
    const normalized = normalizeRemotePath(drive[2] || ".");
    const suffix = normalized === "." ? "" : normalized.replace(/^\//, "");
    return suffix ? `${drive[1].toUpperCase()}/${suffix}` : `${drive[1].toUpperCase()}/`;
  }
  const absolute = value.startsWith("/");
  const parts: string[] = [];
  for (const part of value.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  const next = parts.join("/");
  return absolute ? `/${next}` || "/" : next || ".";
}
