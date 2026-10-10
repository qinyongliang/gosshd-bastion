// Wire-compatible file transport for internal/tunnel.Conn. Sequence ACKs allow the same
// file stream to move between WebRTC and the authenticated WebSocket relay.
export type TransferTransport = "connecting" | "direct" | "relay";
export type TransferProgress = { loaded: number; total: number; transport?: TransferTransport };
export class TransferUnavailable extends Error {}

const DATA = 1, ACK = 2, SIGNAL = 4, PROBE = 6, PROBE_ACK = 7, STATUS = 9;
const CHUNK_SIZE = 8183;
const WINDOW = 64;
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
function packet(kind: number, seq = 0, body = new Uint8Array()): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(9 + body.length);
  bytes[0] = kind;
  new DataView(bytes.buffer).setBigUint64(1, BigInt(seq));
  bytes.set(body, 9);
  return bytes;
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
    let peer: RTCPeerConnection | undefined;
    let channel: RTCDataChannel | undefined;
    let ready = false, settled = false, direct = false, nextSeq = 1, loaded = 0;
    let destination = path;
    let receiveNext = 1, receiving = false, recordsComplete = false;
    let recordBuffer = new Uint8Array(0);
    const buffered = new Map<number, { body: Uint8Array<ArrayBuffer>; direct: boolean }>();
    const delivered = new Map<number, boolean>();
    let transport: TransferTransport = "connecting";
    let lastActivity = Date.now();
    let releaseWindow: (() => void) | undefined;
    let releaseNegotiation: (() => void) | undefined;
    const pending = new Map<number, { bytes: Uint8Array<ArrayBuffer>; sent: number }>();
    let interval: ReturnType<typeof setInterval> | undefined;
    let negotiationTimer: ReturnType<typeof setTimeout> | undefined;
    let idleTimer: ReturnType<typeof setTimeout> | undefined;
    let reusable = false, negotiated = false;
    let setupTimer: ReturnType<typeof setTimeout> | undefined;
    const notify = () => { if (!settled) progress?.({ loaded, total, transport }); };
    const setTransport = (value: TransferTransport) => {
      if (transport !== value) { transport = value; notify(); }
    };
    const close = () => {
      closed = true;
      if (session.generation === generation) session.closed = true;
      clearInterval(interval);
      clearTimeout(negotiationTimer);
      clearTimeout(setupTimer);
      clearTimeout(idleTimer);
      channel?.close(); peer?.close(); ws.close();
    };
    session.close = () => { if (!settled) finish(new DOMException("The file transfer was aborted", "AbortError")); else close(); };
    const finish = (error?: Error) => {
      if (settled) { if (error) close(); return; }
      settled = true;
      clearTimeout(negotiationTimer);
      clearTimeout(setupTimer);
      signal?.removeEventListener("abort", abort);
      releaseWindow?.(); releaseNegotiation?.();
      if (error || !reusable) close();
      else idleTimer = setTimeout(close, 120_000);
      if (error) reject(error); else resolve({ path: destination });
      file = undefined; sink = undefined; progress = undefined; signal = undefined;
    };
    session.next = (newPath, newFile, newSink, newProgress, newSignal) => new Promise((res, rej) => {
      clearTimeout(idleTimer);
      resolve = res; reject = rej;
      path = newPath; file = newFile; sink = newSink; progress = newProgress; signal = newSignal;
      ready = false; settled = false; loaded = 0; total = file?.size || 0;
      destination = path; recordsComplete = false; recordBuffer = new Uint8Array(0);
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
    const relay = (bytes: Uint8Array<ArrayBuffer>) => {
      if (ws.readyState !== WebSocket.OPEN) throw new Error("File transfer connection closed");
      ws.send(bytes);
    };
    const send = (bytes: Uint8Array<ArrayBuffer>) => {
      if (direct && channel?.readyState === "open" && channel.bufferedAmount < 1024 * 1024) {
        try { channel.send(bytes); return; } catch { /* Retry the identical sequence over relay. */ }
      }
      direct = false;
      setTransport("relay");
      relay(bytes);
    };
    const receive = (buffer: ArrayBuffer, fromDirect = false) => {
      if (closed) return;
      if (buffer.byteLength < 9 || buffer.byteLength > 65536) { finish(new Error("Invalid file transfer response")); return; }
      const bytes = new Uint8Array(buffer);
      const seq = Number(new DataView(buffer).getBigUint64(1));
      lastActivity = Date.now();
      if (bytes[0] === ACK) {
        if (pending.delete(seq)) releaseWindow?.();
      } else if (bytes[0] === DATA && action === "download") {
        if (!Number.isSafeInteger(seq) || seq < 1 || bytes.length > 8201) { finish(new Error("Invalid download sequence")); return; }
        if (seq < receiveNext) { acknowledge(seq, delivered.get(seq) || false, fromDirect); return; }
        if (seq >= receiveNext + WINDOW) return;
        if (!buffered.has(seq)) buffered.set(seq, { body: bytes.slice(9), direct: fromDirect });
        void drain();
      } else if (bytes[0] === SIGNAL) {
        void (async () => {
          try {
            const description = JSON.parse(decoder.decode(bytes.subarray(9))) as RTCSessionDescriptionInit;
            if (description.type === "answer") await peer?.setRemoteDescription(description);
          } catch { direct = false; setTransport("relay"); }
        })();
      } else if (bytes[0] === PROBE && channel?.readyState === "open") {
        try { channel.send(packet(PROBE_ACK)); } catch { direct = false; }
      } else if (bytes[0] === PROBE_ACK && fromDirect && channel?.readyState === "open") {
        direct = true;
        setTransport("direct");
        releaseNegotiation?.();
      }
    };
    // ACK after the sink accepts each packet. This bounds queued disk writes to
    // the same 64-packet window and deduplicates relay retransmissions.
    const acknowledge = (seq: number, deliveredDirect: boolean, fromDirect: boolean) => {
      const ack = packet(ACK, seq, new Uint8Array([deliveredDirect ? 1 : 0]));
      if (fromDirect && deliveredDirect && channel?.readyState === "open") {
        try { channel.send(ack); return; } catch { /* Account retries on relay. */ }
      }
      relay(ack);
    };
    const drain = async () => {
      if (receiving || !sink) return;
      receiving = true;
      try {
        while (!settled) {
          const item = buffered.get(receiveNext);
          if (!item) break;
          const combined = new Uint8Array(recordBuffer.length + item.body.length);
          combined.set(recordBuffer); combined.set(item.body, recordBuffer.length);
          recordBuffer = combined;
          while (recordBuffer.length >= 5) {
            const length = new DataView(recordBuffer.buffer).getUint32(1);
            const kind = recordBuffer[0];
            if (recordsComplete || (kind !== 1 && kind !== 2) || length > CHUNK_SIZE + 4 || (kind === 2 && length !== 0) || (kind === 1 && length <= 4)) throw new Error("Invalid download record");
            if (recordBuffer.length < 5 + length) break;
            if (kind === 2) {
              if (loaded !== total) throw new Error("Download size mismatch");
              recordsComplete = true;
            } else {
              const data = recordBuffer.slice(9, 5 + length);
              if (loaded + data.length > total || crc32(data) !== new DataView(recordBuffer.buffer).getUint32(5)) throw new Error("Download checksum or size mismatch");
              await sink.write(data);
              if (closed) return;
              loaded += data.length;
              setTransport(item.direct ? "direct" : "relay");
              notify();
            }
            recordBuffer = recordBuffer.slice(5 + length);
          }
          buffered.delete(receiveNext);
          delivered.set(receiveNext, item.direct);
          delivered.delete(receiveNext - WINDOW * 2);
          acknowledge(receiveNext++, item.direct, item.direct);
          if (recordsComplete) {
            if (recordBuffer.length) throw new Error("Trailing download data");
            await queue(new Uint8Array([2]));
          }
        }
      } catch (error) { finish(error instanceof Error ? error : new Error(String(error))); }
      finally { receiving = false; }
    };
    const waitForWindow = async () => {
      while (!settled && (pending.size >= WINDOW || ws.bufferedAmount > 1024 * 1024)) {
        await new Promise<void>((resolve) => { releaseWindow = resolve; });
      }
      if (settled) throw new Error("File transfer closed");
    };
    const queue = async (body: Uint8Array<ArrayBuffer>) => {
      await waitForWindow();
      const seq = nextSeq++;
      const bytes = packet(DATA, seq, body);
      pending.set(seq, { bytes, sent: Date.now() });
      send(bytes);
    };
    const negotiate = async (servers: string[]) => {
      if (typeof RTCPeerConnection === "undefined") { setTransport("relay"); return; }
      try {
        peer = new RTCPeerConnection({ iceServers: servers.length ? [{ urls: servers }] : [] });
        channel = peer.createDataChannel("tcp-tunnel");
        channel.binaryType = "arraybuffer";
        channel.onmessage = (event: MessageEvent<ArrayBuffer>) => receive(event.data, true);
        channel.onopen = () => {
          if (closed) return;
          // Confirm that the remote peer installed its direct path before
          // requesting file data, so the first download packet is also direct.
          try { channel?.send(packet(PROBE)); }
          catch { setTransport("relay"); releaseNegotiation?.(); }
        };
        channel.onclose = channel.onerror = () => {
          if (!closed) { direct = false; setTransport("relay"); releaseNegotiation?.(); }
        };
        await peer.setLocalDescription(await peer.createOffer());
        await new Promise<void>((resolve) => {
          if (!peer || peer.iceGatheringState === "complete") { resolve(); return; }
          const timer = setTimeout(done, 2500);
          function done() { clearTimeout(timer); peer?.removeEventListener("icegatheringstatechange", changed); resolve(); }
          function changed() { if (peer?.iceGatheringState === "complete") done(); }
          peer.addEventListener("icegatheringstatechange", changed);
        });
        if (!settled && peer.localDescription) relay(packet(SIGNAL, 0, encoder.encode(JSON.stringify(peer.localDescription))));
      } catch { if (!settled) { setTransport("relay"); releaseNegotiation?.(); } }
    };
    const start = async (servers: string[]) => {
      if (!negotiated) {
        negotiated = true;
        void negotiate(servers);
        await new Promise<void>((resolve) => {
          releaseNegotiation = resolve;
          // Allow both peers to gather ICE before the first file starts.
          negotiationTimer = setTimeout(resolve, 8000);
          if (direct || transport === "relay") resolve();
        });
        if (!direct) setTransport("relay");
      }
      try {
        if (!file) {
          await queue(new Uint8Array([1]));
          return;
        }
        // Read bounded batches so LAN throughput is not limited by a separate
        // asynchronous File read for every small tunnel packet.
        const batchSize = CHUNK_SIZE * 32;
        for (let offset = 0; offset < file.size; offset += batchSize) {
          await waitForWindow();
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
            await queue(record);
          }
        }
        await queue(new Uint8Array([2, 0, 0, 0, 0]));
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
              if (closed) return;
              const now = Date.now();
              if (!settled && now - lastActivity > 60_000) { finish(new Error("File transfer timed out")); return; }
              try {
                for (const value of pending.values()) {
                  if (now - value.sent > 1500) {
                    direct = false; setTransport("relay");
                    value.sent = now;
                    if (ws.bufferedAmount < 1024 * 1024) relay(value.bytes);
                  }
                }
                if (channel?.readyState === "open") channel.send(packet(PROBE));
                // Keep the control session alive during a direct transfer.
                relay(packet(PROBE));
                releaseWindow?.();
              } catch (error) { finish(error instanceof Error ? error : new Error(String(error))); }
            }, 1000);
            void start(message.stun_servers || []);
          }
          return;
        }
        const bytes = new Uint8Array(event.data);
        // Only the authenticated relay can report Agent status.
        if (bytes[0] === STATUS) {
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
        } else receive(event.data);
      } catch (error) { finish(error instanceof Error ? error : new Error(String(error))); }
    };
    ws.onerror = ws.onclose = () => finish(ready ? new Error("File transfer connection lost") : new TransferUnavailable("File transfer channel unavailable"));
    notify();
  });
}
