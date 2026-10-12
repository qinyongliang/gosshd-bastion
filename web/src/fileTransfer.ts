// File records and operation lifecycle over the shared bidirectional P2P channel.
import { P2PChannel, PEER_STATUS, MAX_PEER_PAYLOAD } from "./p2pChannel.ts";
import type { ChannelTransport } from "./p2pChannel.ts";
export type TransferTransport = ChannelTransport;
export type TransferProgress = { loaded: number; total: number; transport?: TransferTransport };
export class TransferUnavailable extends Error {}

const CHUNK_SIZE = MAX_PEER_PAYLOAD - 9;
const encoder = new TextEncoder();
const decoder = new TextDecoder();
const crcTable = Uint32Array.from({ length: 256 }, (_, index) => {
  let n = index;
  for (let bit = 0; bit < 8; bit++) n = n & 1 ? 0xedb88320 ^ (n >>> 1) : n >>> 1;
  return n >>> 0;
});
function crc32(data: Uint8Array): number {
  let crc = 0xffffffff;
  for (const byte of data) crc = crcTable[(crc ^ byte) & 255] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}
export type DownloadSink = { maxSize: number; write: (data: Uint8Array<ArrayBuffer>) => Promise<void> };

export function uploadFileP2P(targetID: string, path: string, file: File, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<{ path: string }> {
  return transferFileP2P(targetID, path, file, undefined, progress, signal);
}
export function downloadFileP2P(targetID: string, path: string, sink: DownloadSink, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<{ path: string }> {
  return transferFileP2P(targetID, path, undefined, sink, progress, signal);
}

type Session = { next?: (path: string, file?: File, sink?: DownloadSink, progress?: (p: TransferProgress) => void, signal?: AbortSignal) => Promise<{ path: string }>; close?: () => void; closed: boolean; disposed?: boolean; generation?: symbol; tail: Promise<unknown> };
const sessions = new Map<string, Session>();
export function closeFileTransfers(targetID: string) {
  for (const [key, session] of sessions) if (key.startsWith(`${targetID}:`)) { session.disposed = true; session.close?.(); sessions.delete(key); }
}
function transferFileP2P(targetID: string, path: string, file?: File, sink?: DownloadSink, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<{ path: string }> {
  const key = `${targetID}:${file ? "upload" : "download"}`;
  let session = sessions.get(key);
  if (!session) { session = { closed: true, tail: Promise.resolve() }; sessions.set(key, session); }
  const current = session;
  const result = current.tail.catch(() => {}).then(() => {
    if (current.disposed || signal?.aborted) throw new DOMException("The file transfer was aborted", "AbortError");
    if (!current.closed && current.next) return current.next(path, file, sink, progress, signal);
    return openFileSession(current, targetID, path, file, sink, progress, signal);
  });
  current.tail = result;
  return result;
}

function openFileSession(session: Session, targetID: string, path: string, file?: File, sink?: DownloadSink, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<{ path: string }> {
  const action = file ? "upload" : "download";
  let total = file?.size || 0;
  if (signal?.aborted) return Promise.reject(new DOMException("The file transfer was aborted", "AbortError"));
  const generation = Symbol();
  session.generation = generation;
  session.closed = false;
  let closed = false;
  return new Promise((initialResolve, initialReject) => {
    let resolve = initialResolve, reject = initialReject;
    const url = new URL(`/api/targets/${targetID}/files/${action}/ws`, location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    url.search = new URLSearchParams(file ? { path, name: file.name, size: String(file.size), reuse: "1" } : { path, reuse: "1" }).toString();
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    let ready = false, settled = false, loaded = 0;
    let destination = path;
    let recordsComplete = false;
    let recordBuffer = new Uint8Array(0);
    let sinkChunks: Uint8Array<ArrayBuffer>[] = [], sinkBytes = 0;
    let transport: TransferTransport = "connecting";
    let lastActivity = Date.now();
    let interval: ReturnType<typeof setInterval> | undefined;
    let idleTimer: ReturnType<typeof setTimeout> | undefined;
    let reusable = false;
    let setupTimer: ReturnType<typeof setTimeout> | undefined;
    const notify = () => { if (!settled) progress?.({ loaded, total, transport }); };
    const setTransport = (value: TransferTransport) => {
      if (transport !== value) { transport = value; notify(); }
    };
    const close = () => {
      closed = true;
      if (session.generation === generation) session.closed = true;
      clearInterval(interval);
      clearTimeout(setupTimer);
      clearTimeout(idleTimer);
      channel.close(); ws.close();
    };
    session.close = () => { if (!settled) finish(new DOMException("The file transfer was aborted", "AbortError")); else close(); };
    const finish = (error?: Error) => {
      if (settled) { if (error) close(); return; }
      settled = true;
      clearTimeout(setupTimer);
      signal?.removeEventListener("abort", abort);
      if (error || !reusable) close();
      else idleTimer = setTimeout(close, 120_000);
      if (error) reject(error); else resolve({ path: destination });
      file = undefined; sink = undefined; progress = undefined; signal = undefined;
      sinkChunks = []; sinkBytes = 0;
    };
    session.next = (newPath, newFile, newSink, newProgress, newSignal) => new Promise((res, rej) => {
      clearTimeout(idleTimer);
      resolve = res; reject = rej;
      path = newPath; file = newFile; sink = newSink; progress = newProgress; signal = newSignal;
      ready = false; settled = false; loaded = 0; total = file?.size || 0;
      destination = path; recordsComplete = false; recordBuffer = new Uint8Array(0);
      sinkChunks = []; sinkBytes = 0;
      lastActivity = Date.now();
      signal?.addEventListener("abort", abort, { once: true });
      setupTimer = setTimeout(() => finish(new Error("File operation timed out")), 25_000);
      try { ws.send(JSON.stringify(file ? { path, name: file.name, size: file.size } : { path })); }
      catch (error) { finish(error instanceof Error ? error : new Error(String(error))); }
      notify();
    });
    const abort = () => finish(new DOMException("The file transfer was aborted", "AbortError"));
    signal?.addEventListener("abort", abort, { once: true });
    setupTimer = setTimeout(() => finish(new TransferUnavailable("File transfer channel unavailable")), 25_000);
    const flushSink = async (fromDirect: boolean) => {
      if (!sink || !sinkBytes) return;
      const data = new Uint8Array(sinkBytes);
      let offset = 0;
      for (const chunk of sinkChunks) { data.set(chunk, offset); offset += chunk.length; }
      await sink.write(data);
      if (closed) return;
      loaded += data.length;
      sinkChunks = []; sinkBytes = 0;
      setTransport(fromDirect ? "direct" : "relay");
      notify();
    };
    const channel = new P2PChannel({
      sendRelay: (bytes) => {
        if (ws.readyState !== WebSocket.OPEN) throw new Error("File transfer connection closed");
        ws.send(bytes);
      },
      relayBufferedAmount: () => ws.bufferedAmount,
      onTransport: setTransport,
      onActivity: () => { lastActivity = Date.now(); },
      onError: finish,
      onData: async (body, fromDirect) => {
        if (settled || !sink) throw new Error("Unexpected file transfer data");
        const combined = new Uint8Array(recordBuffer.length + body.length);
        combined.set(recordBuffer); combined.set(body, recordBuffer.length);
        recordBuffer = combined;
        while (recordBuffer.length >= 5) {
          const length = new DataView(recordBuffer.buffer).getUint32(1);
          const kind = recordBuffer[0];
          if (recordsComplete || (kind !== 1 && kind !== 2) || length > CHUNK_SIZE + 4 || (kind === 2 && length !== 0) || (kind === 1 && length <= 4)) throw new Error("Invalid download record");
          if (recordBuffer.length < 5 + length) break;
          if (kind === 2) {
            await flushSink(fromDirect);
            if (loaded !== total) throw new Error("Download size mismatch");
            recordsComplete = true;
          } else {
            const data = recordBuffer.slice(9, 5 + length);
            if (loaded + sinkBytes + data.length > total || crc32(data) !== new DataView(recordBuffer.buffer).getUint32(5)) throw new Error("Download checksum or size mismatch");
            // Accept into a bounded staging buffer; the final receipt waits for
            // every byte to reach the sink. Avoid a disk/UI operation per 8 KiB.
            sinkChunks.push(data); sinkBytes += data.length;
            if (sinkBytes >= CHUNK_SIZE * 32) await flushSink(fromDirect);
            if (closed) return;
          }
          recordBuffer = recordBuffer.slice(5 + length);
        }
        if (recordsComplete) {
          if (recordBuffer.length) throw new Error("Trailing download data");
          await channel.write(new Uint8Array([2]));
        }
      },
    });
    const start = async (servers: string[]) => {
      channel.start(servers);
      setTransport(channel.transport);
      try {
        if (!file) {
          await channel.write(new Uint8Array([1]));
          return;
        }
        // Read bounded batches so LAN throughput is not limited by a separate
        // asynchronous File read for every small tunnel packet.
        const batchSize = CHUNK_SIZE * 32;
        for (let offset = 0; offset < file.size; offset += batchSize) {
          await channel.waitWritable();
          if (settled) return;
          const batch = new Uint8Array(await file.slice(offset, offset + batchSize).arrayBuffer());
          for (let start = 0; start < batch.length; start += CHUNK_SIZE) {
            const data = batch.subarray(start, start + CHUNK_SIZE);
            const record = new Uint8Array(9 + data.length);
            record[0] = 1;
            const view = new DataView(record.buffer);
            view.setUint32(1, 4 + data.length);
            view.setUint32(5, crc32(data));
            record.set(data, 9);
            await channel.write(record);
          }
        }
        await channel.write(new Uint8Array([2, 0, 0, 0, 0]));
      } catch (error) { if (!settled) finish(error instanceof Error ? error : new Error(String(error))); }
    };
    ws.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
      if (closed) return;
      try {
        if (typeof event.data === "string") {
          const message = JSON.parse(event.data);
          if (message.type === "unavailable") { finish(new TransferUnavailable("Agent does not support direct file transfers")); return; }
          if (message.type === "error") { finish(new Error(message.error || "File transfer failed")); return; }
          if (message.type === "ready" && !ready) {
            ready = true;
            reusable = message.reuse === true;
            clearTimeout(setupTimer);
            destination = message.path;
            if (sink) {
              total = message.size;
              if (!Number.isSafeInteger(total) || total < 0) throw new Error("Invalid download size");
              if (total > sink.maxSize) { finish(new TransferUnavailable("Download requires native streaming")); return; }
            }
            lastActivity = Date.now();
            if (!interval) interval = setInterval(() => {
              if (!closed && !settled && Date.now() - lastActivity > 60_000) finish(new Error("File transfer timed out"));
            }, 1000);
            void start(message.stun_servers || []);
          }
          return;
        }
        const bytes = new Uint8Array(event.data);
        // Only the authenticated relay can report Agent status.
        if (bytes[0] === PEER_STATUS) {
          if (settled) return;
          const status = JSON.parse(decoder.decode(bytes.subarray(9)));
          lastActivity = Date.now();
          if (status.type === "error") { finish(new Error(status.error || "File transfer failed")); return; }
          if (file) loaded = Math.max(loaded, Math.min(total, status.loaded));
          // Use the trusted Agent receipt for the completed file's transport.
          if (status.type === "complete") transport = status.direct ? "direct" : "relay";
          notify();
          if (status.type === "complete") {
            if (status.loaded !== total || !/^[a-f0-9]{64}$/.test(status.sha256) || (sink && (!recordsComplete || loaded !== total))) throw new Error("Invalid file transfer completion");
            finish();
          }
        } else channel.receive(event.data);
      } catch (error) { finish(error instanceof Error ? error : new Error(String(error))); }
    };
    ws.onerror = ws.onclose = () => finish(ready ? new Error("File transfer connection lost") : new TransferUnavailable("File transfer channel unavailable"));
    notify();
  });
}
