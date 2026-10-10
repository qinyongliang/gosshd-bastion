// Wire-compatible sender for internal/tunnel.Conn. Sequence ACKs allow the same
// upload stream to move between WebRTC and the authenticated WebSocket relay.
export type UploadTransport = "connecting" | "direct" | "relay";
export type UploadProgress = { loaded: number; total: number; transport?: UploadTransport };
export class UploadUnavailable extends Error {}

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

export function uploadFileP2P(targetID: string, path: string, file: File, progress?: (p: UploadProgress) => void, signal?: AbortSignal): Promise<{ path: string }> {
  if (signal?.aborted) return Promise.reject(new DOMException("The upload was aborted", "AbortError"));
  return new Promise((resolve, reject) => {
    const url = new URL(`/api/targets/${targetID}/files/upload/ws`, location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    url.search = new URLSearchParams({ path, name: file.name, size: String(file.size) }).toString();
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    let peer: RTCPeerConnection | undefined;
    let channel: RTCDataChannel | undefined;
    let ready = false, settled = false, direct = false, nextSeq = 1, loaded = 0;
    let destination = path;
    let transport: UploadTransport = "connecting";
    let lastActivity = Date.now();
    let releaseWindow: (() => void) | undefined;
    let releaseNegotiation: (() => void) | undefined;
    const pending = new Map<number, { bytes: Uint8Array<ArrayBuffer>; sent: number }>();
    let interval: ReturnType<typeof setInterval> | undefined;
    let negotiationTimer: ReturnType<typeof setTimeout> | undefined;
    let setupTimer: ReturnType<typeof setTimeout> | undefined;
    const notify = () => progress?.({ loaded, total: file.size, transport });
    const setTransport = (value: UploadTransport) => {
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
    const abort = () => finish(new DOMException("The upload was aborted", "AbortError"));
    signal?.addEventListener("abort", abort, { once: true });
    setupTimer = setTimeout(() => finish(new UploadUnavailable("Upload channel unavailable")), 25_000);
    const relay = (bytes: Uint8Array<ArrayBuffer>) => {
      if (ws.readyState !== WebSocket.OPEN) throw new Error("Upload connection closed");
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
    const receive = (buffer: ArrayBuffer) => {
      if (settled) return;
      if (buffer.byteLength < 9 || buffer.byteLength > 65536) { finish(new Error("Invalid upload response")); return; }
      const bytes = new Uint8Array(buffer);
      const seq = Number(new DataView(buffer).getBigUint64(1));
      lastActivity = Date.now();
      if (bytes[0] === ACK) {
        if (pending.delete(seq)) releaseWindow?.();
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
    const waitForWindow = async () => {
      while (!settled && (pending.size >= WINDOW || ws.bufferedAmount > 1024 * 1024)) {
        await new Promise<void>((resolve) => { releaseWindow = resolve; });
      }
      if (settled) throw new Error("Upload closed");
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
        channel.onmessage = (event: MessageEvent<ArrayBuffer>) => receive(event.data);
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
        for (let offset = 0; offset < file.size; offset += CHUNK_SIZE) {
          await waitForWindow();
          if (settled) return;
          const data = new Uint8Array(await file.slice(offset, offset + CHUNK_SIZE).arrayBuffer());
          const record = new Uint8Array(9 + data.length);
          record[0] = 1;
          const view = new DataView(record.buffer);
          view.setUint32(1, 4 + data.length);
          view.setUint32(5, crc32(data));
          record.set(data, 9);
          await queue(record);
        }
        await queue(new Uint8Array([2, 0, 0, 0, 0]));
      } catch (error) { if (!settled) finish(error instanceof Error ? error : new Error(String(error))); }
    };
    ws.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
      if (settled) return;
      try {
        if (typeof event.data === "string") {
          const message = JSON.parse(event.data);
          if (message.type === "unavailable") { finish(new UploadUnavailable("Agent does not support direct uploads")); return; }
          if (message.type === "error") { finish(new Error(message.error || "Upload failed")); return; }
          if (message.type === "ready" && !ready) {
            ready = true;
            clearTimeout(setupTimer);
            destination = message.path;
            lastActivity = Date.now();
            interval = setInterval(() => {
              if (settled) return;
              const now = Date.now();
              if (now - lastActivity > 60_000) { finish(new Error("Upload timed out")); return; }
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
        // Only the authenticated relay can report disk progress and completion.
        if (bytes[0] === STATUS) {
          const status = JSON.parse(decoder.decode(bytes.subarray(9)));
          lastActivity = Date.now();
          if (status.type === "error") { finish(new Error(status.error || "Upload failed")); return; }
          loaded = Math.max(loaded, Math.min(file.size, status.loaded));
          notify();
          if (status.type === "complete") {
            if (status.loaded !== file.size || !/^[a-f0-9]{64}$/.test(status.sha256)) throw new Error("Invalid upload completion");
            finish();
          }
        } else receive(event.data);
      } catch (error) { finish(error instanceof Error ? error : new Error(String(error))); }
    };
    ws.onerror = ws.onclose = () => finish(ready ? new Error("Upload connection lost") : new UploadUnavailable("Upload channel unavailable"));
    notify();
  });
}
