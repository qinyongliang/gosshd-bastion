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

function transferFileP2P(targetID: string, path: string, file?: File, sink?: DownloadSink, progress?: (p: TransferProgress) => void, signal?: AbortSignal): Promise<{ path: string }> {
  const action = file ? "upload" : "download";
  let total = file?.size || 0;
  if (signal?.aborted) return Promise.reject(new DOMException("The file transfer was aborted", "AbortError"));
  return new Promise((resolve, reject) => {
    const url = new URL(`/api/targets/${targetID}/files/${action}/ws`, location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    url.search = new URLSearchParams(file ? { path, name: file.name, size: String(file.size) } : { path }).toString();
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
    let setupTimer: ReturnType<typeof setTimeout> | undefined;
    const notify = () => progress?.({ loaded, total, transport });
    const setTransport = (value: TransferTransport) => {
      if (transport !== value) { transport = value; notify(); }
    };
    const finish = (error?: Error) => {
      if (settled) return;
      settled = true;
      clearInterval(interval);
      clearTimeout(negotiationTimer);
      clearTimeout(setupTimer);
      signal?.removeEventListener("abort", abort);
      releaseWindow?.();
      releaseNegotiation?.();
      channel?.close();
      peer?.close();
      ws.close();
      if (error) reject(error); else resolve({ path: destination });
    };
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
      if (settled) return;
      if (buffer.byteLength < 9 || buffer.byteLength > 65536) { finish(new Error("Invalid file transfer response")); return; }
      const bytes = new Uint8Array(buffer);
      const seq = Number(new DataView(buffer).getBigUint64(1));
      lastActivity = Date.now();
      if (bytes[0] === ACK) {
        if (pending.delete(seq)) releaseWindow?.();
      } else if (bytes[0] === DATA && sink) {
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
      } else if (bytes[0] === PROBE_ACK && channel?.readyState === "open") {
        direct = true;
        setTransport("direct");
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
              if (settled) return;
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
          if (settled) return;
          direct = true;
          setTransport("direct");
          releaseNegotiation?.();
        };
        channel.onclose = channel.onerror = () => {
          if (!settled) { direct = false; setTransport("relay"); }
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
      void negotiate(servers);
      await new Promise<void>((resolve) => {
        releaseNegotiation = resolve;
        // Both peers gather candidates before exchanging SDP. Allow the Agent's
        // five-second STUN timeout before falling back, even on a small file.
        negotiationTimer = setTimeout(resolve, 8000);
        if (direct || transport === "relay") resolve();
      });
      if (!direct) setTransport("relay");
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
      if (settled) return;
      try {
        if (typeof event.data === "string") {
          const message = JSON.parse(event.data);
          if (message.type === "unavailable") { finish(new TransferUnavailable("Agent does not support direct file transfers")); return; }
          if (message.type === "error") { finish(new Error(message.error || "File transfer failed")); return; }
          if (message.type === "ready" && !ready) {
            ready = true;
            clearTimeout(setupTimer);
            destination = message.path;
            if (sink) {
              total = message.size;
              if (!Number.isSafeInteger(total) || total < 0) throw new Error("Invalid download size");
              if (total > sink.maxSize) { finish(new TransferUnavailable("Download requires native streaming")); return; }
            }
            lastActivity = Date.now();
            interval = setInterval(() => {
              if (settled) return;
              const now = Date.now();
              if (now - lastActivity > 60_000) { finish(new Error("File transfer timed out")); return; }
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
          const status = JSON.parse(decoder.decode(bytes.subarray(9)));
          lastActivity = Date.now();
          if (status.type === "error") { finish(new Error(status.error || "File transfer failed")); return; }
          if (file) loaded = Math.max(loaded, Math.min(total, status.loaded));
          // The Agent closes WebRTC after sending the receipt. Use its transport
          // state even when onclose arrives before the receipt.
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
