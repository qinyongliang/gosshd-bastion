import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Download, ExternalLink, FolderOpen, FolderPlus, HardDrive, Info, Move, RefreshCw, Trash2, Upload } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api";
import { Modal, ModalActions } from "../components/ui";
import { useI18n } from "../i18n";
import type { FileEntry, FileProperties, Target } from "../types";
import { copyText } from "../utils";

export function FileManager({ target, nativeOpen = false }: { target: Target; nativeOpen?: boolean }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [path, setPath] = useState(".");
  const [pathDraft, setPathDraft] = useState(".");
  const [selected, setSelected] = useState<FileEntry | null>(null);
  const [uploading, setUploading] = useState(false);
  const [mkdirModal, setMkdirModal] = useState(false);
  const [transfer, setTransfer] = useState<{ action: "move" | "copy"; entry: FileEntry } | null>(null);
  const [properties, setProperties] = useState<FileProperties | null>(null);
  const [contextMenu, setContextMenu] = useState<{ entry: FileEntry | null; x: number; y: number; left: number; top: number } | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const contextMenuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setPathDraft(path);
  }, [path]);

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

  const listing = useQuery({
    queryKey: ["target-files", target.id, path],
    queryFn: () => api.listFiles(target.id, path),
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

  const handleMenuAction = async (action: "open" | "download" | "copy-path" | "refresh" | "upload" | "mkdir" | "delete" | "properties" | "move" | "copy", entry: FileEntry | null) => {
    if (action === "open" && entry) {
      activateEntry(entry);
    } else if (action === "download" && entry && entry.type === "file") {
      downloadEntry(entry);
    } else if (action === "copy-path") {
      await copyEntryPath(entry);
    } else if (action === "refresh") {
      await listing.refetch();
    } else if (action === "upload") {
      fileInputRef.current?.click();
    } else if (action === "mkdir") {
      setMkdirModal(true);
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

  const runMenuAction = (action: "open" | "download" | "copy-path" | "refresh" | "upload" | "mkdir" | "delete" | "properties" | "move" | "copy", entry: FileEntry | null) => {
    setContextMenu(null);
    void handleMenuAction(action, entry);
  };

  const submitMkdir = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    if (!name) return;
    mkdir.mutate(remoteJoin(path, name), { onSuccess: () => setMkdirModal(false) });
  };

  const submitPath = () => {
    const nextPath = pathDraft.trim();
    if (!nextPath || nextPath === path) {
      setPathDraft(path);
      return;
    }
    setPath(nextPath);
    setSelected(null);
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
        <button type="button" role="menuitem" className="file-context-menu-item" onClick={() => runMenuAction("mkdir", entry)}>
          <FolderPlus />{t("connectFileNewFolder")}
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

  return (
    <section className="file-manager">
      <header className="file-manager-head">
        <div className="file-manager-path" title={path}>
          <HardDrive />
          <input
            value={pathDraft}
            onChange={(event) => setPathDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitPath();
              } else if (event.key === "Escape") {
                setPathDraft(path);
              }
            }}
            onBlur={() => setPathDraft(path)}
            aria-label="File path"
          />
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
      <div className="file-manager-body" onContextMenu={(event) => openFileMenu(null, event)}>
        <table>
          <thead>
            <tr>
              <th>{t("connectFileName")}</th>
              <th>{t("connectFileSize")}</th>
              <th>{t("connectFileMode")}</th>
              <th>{t("connectFileModified")}</th>
            </tr>
          </thead>
          <tbody>
            {path !== "/" && (
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
                onClick={() => setSelected(entry)}
                onDoubleClick={() => activateEntry(entry)}
                onContextMenu={(event) => openFileMenu(entry, event)}
              >
                  <td>
                    <button type="button" className="file-name" onClick={() => setSelected(entry)} onDoubleClick={() => activateEntry(entry)} title={entry.name}>
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
      {transfer && (
        <TransferModal
          target={target}
          transfer={transfer}
          initialDir={path}
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

function TransferModal({
  target,
  transfer,
  initialDir,
  onClose,
  onSubmit,
}: {
  target: Target;
  transfer: { action: "move" | "copy"; entry: FileEntry };
  initialDir: string;
  onClose: () => void;
  onSubmit: (entry: FileEntry, destination: string) => void;
}) {
  const { t } = useI18n();
  const [browsePath, setBrowsePath] = useState(initialDir || ".");
  const [browseDraft, setBrowseDraft] = useState(initialDir || ".");
  const [destination, setDestination] = useState(remoteJoin(initialDir || ".", transfer.entry.name));
  const listing = useQuery({
    queryKey: ["target-files", target.id, browsePath],
    queryFn: () => api.listFiles(target.id, browsePath),
  });
  const directories = (listing.data?.entries || []).filter((entry) => entry.type === "dir");

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
              <tbody>
                {browsePath !== "/" && (
                  <tr className="file-row directory">
                    <td>
                      <button type="button" className="file-name" onClick={() => setBrowsePath(remoteParent(browsePath))}>
                        <FolderOpen />{t("connectFileParentDir")}
                      </button>
                    </td>
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
                  </tr>
                ))}
                {!directories.length && (
                  <tr>
                    <td className="file-empty">{listing.isLoading ? t("loading") : t("connectFileEmpty")}</td>
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
  if (trimmedName.startsWith("/")) return normalizeRemotePath(trimmedName);
  if (!dir || dir === ".") return normalizeRemotePath(trimmedName);
  return normalizeRemotePath(`${dir.replace(/\/+$/, "")}/${trimmedName}`);
}

function remoteParent(path: string) {
  const trimmed = path.replace(/\/$/, "");
  const index = trimmed.lastIndexOf("/");
  if (index <= 0) return "/";
  return trimmed.slice(0, index) || "/";
}

function normalizeRemotePath(value: string) {
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
