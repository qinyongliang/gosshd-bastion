import { downloadHTTP } from "./fileDownload.ts";
import { downloadFileP2P, TransferUnavailable } from "./fileTransfer.ts";
import type { DownloadSink, TransferProgress } from "./fileTransfer.ts";

export type FilePreviewType = { kind: "image" | "audio" | "video" | "pdf"; mime: string };
const previewTypes: Record<string, FilePreviewType> = {
  png: { kind: "image", mime: "image/png" },
  jpg: { kind: "image", mime: "image/jpeg" }, jpeg: { kind: "image", mime: "image/jpeg" },
  gif: { kind: "image", mime: "image/gif" }, webp: { kind: "image", mime: "image/webp" },
  avif: { kind: "image", mime: "image/avif" }, bmp: { kind: "image", mime: "image/bmp" },
  ico: { kind: "image", mime: "image/x-icon" }, svg: { kind: "image", mime: "image/svg+xml" },
  mp4: { kind: "video", mime: "video/mp4" }, webm: { kind: "video", mime: "video/webm" },
  ogv: { kind: "video", mime: "video/ogg" }, mov: { kind: "video", mime: "video/quicktime" },
  mp3: { kind: "audio", mime: "audio/mpeg" }, wav: { kind: "audio", mime: "audio/wav" },
  ogg: { kind: "audio", mime: "audio/ogg" }, oga: { kind: "audio", mime: "audio/ogg" },
  m4a: { kind: "audio", mime: "audio/mp4" }, aac: { kind: "audio", mime: "audio/aac" },
  flac: { kind: "audio", mime: "audio/flac" }, opus: { kind: "audio", mime: "audio/ogg" },
  pdf: { kind: "pdf", mime: "application/pdf" },
};

// Do not embed arbitrary HTML/XML as same-origin documents. SVG is image-only.
export function filePreviewType(name: string): FilePreviewType | undefined {
  const extension = name.split(".").pop()?.toLowerCase();
  return name.includes(".") && extension && Object.hasOwn(previewTypes, extension) ? previewTypes[extension] : undefined;
}

export const MAX_PREVIEW_SIZE = 256 * 1024 * 1024;
export class FilePreviewError extends Error {
  readonly code: "unsupported" | "too-large" | "invalid";
  constructor(code: "unsupported" | "too-large" | "invalid") { super(code); this.code = code; }
}

export async function readFilePreview(targetID: string, path: string, size: number, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<Blob> {
  const type = filePreviewType(path);
  if (!type) throw new FilePreviewError("unsupported");
  if (!Number.isSafeInteger(size) || size < 0 || size > MAX_PREVIEW_SIZE) throw new FilePreviewError("too-large");
  if (signal?.aborted) throw new DOMException("Preview aborted", "AbortError");
  let chunks: Uint8Array<ArrayBuffer>[] = [];
  let loaded = 0;
  const sink: DownloadSink = {
    maxSize: MAX_PREVIEW_SIZE,
    write: async (bytes) => {
      if (loaded + bytes.length > MAX_PREVIEW_SIZE) throw new FilePreviewError("too-large");
      loaded += bytes.length;
      chunks.push(bytes);
    },
  };
  try {
    try {
      await downloadFileP2P(targetID, path, sink, progress, signal);
    } catch (error) {
      if (!(error instanceof TransferUnavailable)) throw error;
      if (signal?.aborted) throw new DOMException("Preview aborted", "AbortError");
      if (error.message === "Download requires native streaming") throw new FilePreviewError("too-large");
      chunks = [];
      loaded = 0;
      await downloadHTTP(`/api/targets/${encodeURIComponent(targetID)}/files/download?${new URLSearchParams({ path })}`, size, sink, progress, signal);
    }
    if (signal?.aborted) throw new DOMException("Preview aborted", "AbortError");
    const blob = new Blob(chunks, { type: type.mime });
    // Only PDFs may become documents; never load an HTML file renamed to .pdf.
    if (type.kind === "pdf" && await blob.slice(0, 5).text() !== "%PDF-") throw new FilePreviewError("invalid");
    return blob;
  } finally {
    chunks = [];
  }
}
