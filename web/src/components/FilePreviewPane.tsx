import { X } from "lucide-react";
import { useEffect, useState } from "react";
import { filePreviewType, FilePreviewError, readFilePreview } from "../filePreview";
import type { TransferProgress } from "../fileTransfer";
import { useI18n } from "../i18n";
import { DockDragHandle } from "./DockWorkspace";
import "./filePreview.css";

export function FilePreviewPane({ paneID, targetID, filePath, size, active, onActivate, onClose }: {
  paneID: string; targetID: string; filePath: string; size: number; active: boolean; onActivate: () => void; onClose: () => void;
}) {
  const { t } = useI18n();
  const [url, setURL] = useState("");
  const [progress, setProgress] = useState<TransferProgress>({ loaded: 0, total: size, transport: "connecting" });
  const [error, setError] = useState<unknown>(null);
  const type = filePreviewType(filePath);

  useEffect(() => {
    const controller = new AbortController();
    let objectURL = "";
    setURL("");
    setError(null);
    setProgress({ loaded: 0, total: size, transport: "connecting" });
    if (filePreviewType(filePath)?.kind === "pdf" && navigator.pdfViewerEnabled === false) {
      setError(new FilePreviewError("unsupported"));
      return () => controller.abort();
    }
    void readFilePreview(targetID, filePath, size, (value) => {
      if (!controller.signal.aborted) setProgress(value);
    }, controller.signal).then((blob) => {
      if (controller.signal.aborted) return;
      objectURL = URL.createObjectURL(blob);
      setURL(objectURL);
    }).catch((err: unknown) => {
      if (!controller.signal.aborted) setError(err);
    });
    return () => {
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [targetID, filePath, size]);

  const errorText = error instanceof FilePreviewError
    ? t(error.code === "too-large" ? "connectFilePreviewTooLarge" : error.code === "invalid" ? "connectFilePreviewFailed" : "connectFilePreviewUnsupported")
    : error instanceof Error ? error.message : error ? String(error) : "";
  const mediaError = () => setError(new Error(t("connectFilePreviewFailed")));
  const transport = t(progress.transport === "direct" ? "connectFileUploadDirect" : progress.transport === "relay" ? "connectFileUploadRelay" : "connectFileUploadConnecting");

  return <section className={`editor-pane file-preview-pane pane-leaf ${active ? "active" : ""}`} onPointerDown={onActivate} aria-label={`${t("connectFilePreview")}: ${filePath}`}>
    <header className="editor-pane-head">
      <DockDragHandle paneID={paneID}><span title={filePath}>{filePath}</span></DockDragHandle>
      <div className="editor-pane-actions">
        <span className="file-preview-transport" data-transport={progress.transport}>{transport}</span>
        <button type="button" className="icon-button" onClick={onClose} title={t("close")} aria-label={t("close")}><X /></button>
      </div>
    </header>
    {errorText ? <div className="connect-zone-empty" role="alert"><span>{errorText}</span></div>
      : !url ? <div className="connect-zone-empty file-preview-loading" role="status">
        <span>{t("loading")}</span>
        <progress aria-label={t("connectFilePreview")} value={progress.loaded} max={Math.max(1, progress.total)} />
        <span>{progress.loaded.toLocaleString()} / {progress.total.toLocaleString()} B</span>
      </div>
      : <div className="file-preview-content">
        {type?.kind === "image" && <img src={url} alt={filePath} onError={mediaError} />}
        {type?.kind === "video" && <video src={url} controls playsInline onError={mediaError} aria-label={filePath} />}
        {type?.kind === "audio" && <audio src={url} controls onError={mediaError} aria-label={filePath} />}
        {/* Browser PDF viewers are blocked by iframe sandboxing; the reader enforces PDF MIME and signature. */}
        {type?.kind === "pdf" && <iframe src={url} title={filePath} />}
      </div>}
  </section>;
}
