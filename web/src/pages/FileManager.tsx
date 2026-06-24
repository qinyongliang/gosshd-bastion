import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { Copy, Download, ExternalLink, FolderOpen, FolderPlus, HardDrive, Info, Move, RefreshCw, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
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
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setPathDraft(path);
  }, [path]);

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

  const submitMkdir = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    if (!name) return;
    mkdir.mutate(remoteJoin(path, name), { onSuccess: () => setMkdirModal(false) });
  };

  const submitTransfer = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!transfer) return;
    const form = new FormData(event.currentTarget);
    const destination = String(form.get("destination") || "").trim();
    if (!destination) return;
    const mutation = transfer.action === "move" ? move : copy;
    mutation.mutate({ entry: transfer.entry, destination }, { onSuccess: () => setTransfer(null) });
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

  const parentPath = () => {
    const trimmed = path.replace(/\/$/, "");
    const index = trimmed.lastIndexOf("/");
    if (index <= 0) return "/";
    return trimmed.slice(0, index) || "/";
  };

  const fileMenu = (entry: FileEntry | null) => (
    <ContextMenu.Portal>
      <ContextMenu.Content className="file-context-menu" collisionPadding={8}>
        {entry?.type === "dir" && (
          <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("open", entry)}>
            <FolderOpen />{t("connectFileOpenDir")}
          </ContextMenu.Item>
        )}
        {entry?.type === "file" && nativeOpen && (
          <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("open", entry)}>
            <ExternalLink />{t("connectFileOpenDir")}
          </ContextMenu.Item>
        )}
        {entry?.type === "file" && (
          <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("download", entry)}>
            <Download />{t("connectFileDownload")}
          </ContextMenu.Item>
        )}
        <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("mkdir", entry)}>
          <FolderPlus />{t("connectFileNewFolder")}
        </ContextMenu.Item>
        {entry && (
          <>
            <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("move", entry)}>
              <Move />{t("connectFileMove")}
            </ContextMenu.Item>
            <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("copy", entry)}>
              <Copy />{t("connectFileCopy")}
            </ContextMenu.Item>
            <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("properties", entry)}>
              <Info />{t("connectFileProperties")}
            </ContextMenu.Item>
            <ContextMenu.Item className="file-context-menu-item danger" onSelect={() => void handleMenuAction("delete", entry)}>
              <Trash2 />{t("commonDelete")}
            </ContextMenu.Item>
            <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("copy-path", entry)}>
              <Copy />{t("connectFileCopyPath")}
            </ContextMenu.Item>
          </>
        )}
        <ContextMenu.Item className="file-context-menu-item" onSelect={() => void handleMenuAction("refresh", entry)}>
          <RefreshCw />{t("commonRefresh")}
        </ContextMenu.Item>
        {!entry && (
          <ContextMenu.Item className="file-context-menu-item" disabled={uploading} onSelect={() => void handleMenuAction("upload", entry)}>
            <Upload />{uploading ? t("connectFileUploading") : t("connectFileUpload")}
          </ContextMenu.Item>
        )}
      </ContextMenu.Content>
    </ContextMenu.Portal>
  );

  const withFileMenu = (entry: FileEntry | null, children: ReactNode, key?: string) => (
    <ContextMenu.Root key={key} onOpenChange={(open) => { if (open) setSelected(entry); }}>
      <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
      {fileMenu(entry)}
    </ContextMenu.Root>
  );

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
      {withFileMenu(null, <div className="file-manager-body">
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
                  <button type="button" className="file-name" onClick={() => setPath(parentPath())} onDoubleClick={() => setPath(parentPath())}>
                    <FolderOpen />{t("connectFileParentDir")}
                  </button>
                </td>
                <td>-</td><td>-</td><td>-</td>
              </tr>
            )}
            {entries.map((entry) => (
              withFileMenu(entry, (
                <tr
                  className={`file-row ${entry.type} ${selected?.path === entry.path ? "selected" : ""}`}
                  onClick={() => setSelected(entry)}
                  onDoubleClick={() => activateEntry(entry)}
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
              ), entry.path)
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
      </div>)}
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
        <Modal title={transfer.action === "move" ? t("connectFileMove") : t("connectFileCopy")} onClose={() => setTransfer(null)}>
          <form className="stack" onSubmit={submitTransfer}>
            <p className="muted">{transfer.entry.path}</p>
            <label className="field">
              <span>{t("connectFileDestination")}</span>
              <input name="destination" defaultValue={remoteJoin(path, transfer.entry.name)} autoFocus required />
            </label>
            <ModalActions onCancel={() => setTransfer(null)} submit={transfer.action === "move" ? t("connectFileMove") : t("connectFileCopy")} />
          </form>
        </Modal>
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

function remoteJoin(dir: string, name: string) {
  const trimmedName = name.trim();
  if (!trimmedName) return dir;
  if (trimmedName.startsWith("/")) return normalizeRemotePath(trimmedName);
  if (!dir || dir === ".") return normalizeRemotePath(trimmedName);
  return normalizeRemotePath(`${dir.replace(/\/+$/, "")}/${trimmedName}`);
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
