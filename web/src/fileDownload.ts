import { downloadFileP2P, TransferUnavailable } from "./fileTransfer";
import type { DownloadSink, TransferProgress } from "./fileTransfer";

// Browsers without a streaming save API retain their native HTTP download for
// large files rather than buffering an unbounded Blob in the page.
const MAX_BLOB_SIZE = 256 * 1024 * 1024;
type Sink = DownloadSink & { finish: () => Promise<void>; abort: () => Promise<void> };
type SaveHandle = { createWritable: () => Promise<FileSystemWritableFileStream> };
type SaveWindow = Window & { showSaveFilePicker?: (options: { suggestedName: string }) => Promise<SaveHandle> };

function saveURL(url: string, name: string) {
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

async function createSink(name: string): Promise<Sink> {
  const picker = (window as SaveWindow).showSaveFilePicker;
  if (picker) {
    let handle: SaveHandle | undefined;
    try { handle = await picker.call(window, { suggestedName: name }); }
    catch (error) {
      if (!(error instanceof DOMException) || !["SecurityError", "NotAllowedError"].includes(error.name)) throw error;
    }
    if (handle) {
      const writable = await handle.createWritable();
      return { maxSize: Number.MAX_SAFE_INTEGER, write: (bytes) => writable.write(bytes), finish: () => writable.close(), abort: () => writable.abort() };
    }
  }
  let chunks: Uint8Array<ArrayBuffer>[] = [];
  return {
    maxSize: MAX_BLOB_SIZE,
    write: async (bytes) => { chunks.push(bytes); },
    finish: async () => {
      const url = URL.createObjectURL(new Blob(chunks, { type: "application/octet-stream" }));
      chunks = [];
      saveURL(url, name);
      // The browser may consume the URL after the click handler has returned.
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    },
    abort: async () => { chunks = []; },
  };
}

export async function downloadFile(targetID: string, path: string, name: string, size: number, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<"saved" | "browser"> {
  // Invoke the picker before awaiting network work, preserving user activation.
  const sink = await createSink(name);
  const httpURL = `/api/targets/${targetID}/files/download?${new URLSearchParams({ path })}`;
  try {
    if (signal?.aborted) throw new DOMException("Download aborted", "AbortError");
    try {
      await downloadFileP2P(targetID, path, sink, progress, signal);
    } catch (error) {
      if (!(error instanceof TransferUnavailable)) throw error;
      if (signal?.aborted) throw new DOMException("Download aborted", "AbortError");
      if (size > sink.maxSize || error.message === "Download requires native streaming") {
        await sink.abort();
        progress?.({ loaded: 0, total: size, transport: "relay" });
        saveURL(httpURL, name);
        return "browser";
      }
      await downloadHTTP(httpURL, size, sink, progress, signal);
    }
    if (signal?.aborted) throw new DOMException("Download aborted", "AbortError");
    await sink.finish();
    return "saved";
  } catch (error) {
    await sink.abort().catch(() => {});
    throw error;
  }
}

async function downloadHTTP(url: string, size: number, sink: DownloadSink, progress?: (p: TransferProgress) => void, signal?: AbortSignal) {
  const response = await fetch(url, { credentials: "same-origin", signal });
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new Error(data?.error || `${response.status} ${response.statusText}`);
  }
  if (!response.body) throw new Error("Download body unavailable");
  const reader = response.body.getReader();
  let loaded = 0;
  try {
    progress?.({ loaded, total: size, transport: "relay" });
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      loaded += value.length;
      if (loaded > sink.maxSize) throw new Error("File exceeds this browser's download buffer limit");
      await sink.write(value);
      progress?.({ loaded, total: Math.max(size, loaded), transport: "relay" });
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
