import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FolderOpen, HardDrive, RefreshCw, Upload } from "lucide-react";
import { useRef, useState } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { FileEntry, Target } from "../types";

export function FileManager({ target }: { target: Target }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [path, setPath] = useState(".");
  const [selected, setSelected] = useState<FileEntry | null>(null);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

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

  const entries = listing.data?.entries || [];

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    upload.mutate(file);
    event.target.value = "";
  };

  const openEntry = (entry: FileEntry) => {
    if (entry.type === "dir") {
      setPath(entry.path);
      setSelected(null);
    } else {
      setSelected(entry);
    }
  };

  const parentPath = () => {
    const trimmed = path.replace(/\/$/, "");
    const index = trimmed.lastIndexOf("/");
    if (index <= 0) return "/";
    return trimmed.slice(0, index) || "/";
  };

  const downloadLink = selected && selected.type === "file" ? api.downloadFile(target.id, selected.path) : "";

  return (
    <section className="file-manager">
      <header className="file-manager-head">
        <div className="file-manager-path" title={path}>
          <HardDrive />
          <code>{path}</code>
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
          {downloadLink && (
            <a className="button-link small" href={downloadLink} download={selected?.name} title={t("connectFileDownload")}>
              <Download />{t("connectFileDownload")}
            </a>
          )}
        </div>
      </header>
      <div className="file-manager-body">
        <table>
          <thead>
            <tr>
              <th>{t("connectFileName")}</th>
              <th>{t("connectFileSize")}</th>
              <th>{t("connectFileMode")}</th>
              <th>{t("connectFileModified")}</th>
              <th>{t("commonActions")}</th>
            </tr>
          </thead>
          <tbody>
            {path !== "/" && (
              <tr className="file-row directory">
                <td>
                  <button type="button" className="file-name" onClick={() => setPath(parentPath())}>
                    <FolderOpen />{t("connectFileParentDir")}
                  </button>
                </td>
                <td>-</td><td>-</td><td>-</td><td>-</td>
              </tr>
            )}
            {entries.map((entry) => (
              <tr key={entry.path} className={`file-row ${entry.type} ${selected?.path === entry.path ? "selected" : ""}`}>
                <td>
                  <button type="button" className="file-name" onClick={() => openEntry(entry)} title={entry.name}>
                    {entry.type === "dir" ? <FolderOpen /> : <HardDrive />}
                    <span>{entry.name}</span>
                  </button>
                </td>
                <td>{entry.type === "dir" ? "-" : formatBytes(entry.size)}</td>
                <td><code>{entry.mode}</code></td>
                <td>{formatDate(entry.modified_at)}</td>
                <td>
                  {entry.type === "dir" ? (
                    <button type="button" className="icon-button" onClick={() => openEntry(entry)} title={t("connectFileOpenDir")}>
                      <FolderOpen />
                    </button>
                  ) : (
                    <a className="button-link small" href={api.downloadFile(target.id, entry.path)} download={entry.name}>
                      <Download />{t("connectFileDownload")}
                    </a>
                  )}
                </td>
              </tr>
            ))}
            {!entries.length && (
              <tr>
                <td colSpan={5} className="file-empty">
                  {listing.isLoading ? t("loading") : t("connectFileEmpty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
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
