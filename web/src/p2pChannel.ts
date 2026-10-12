// Wire-compatible with internal/tunnel.Conn. Business protocols only read/write
// payloads; this channel owns relay/direct policy and reliable path migration.
export type ChannelTransport = "connecting" | "direct" | "relay";
export const PEER_DATA = 1;
export const PEER_STATUS = 9;
export const MAX_PEER_PAYLOAD = 8192;
const ACK = 2, SIGNAL = 4, PROBE = 6, PROBE_ACK = 7;
const WINDOW = 64, MAX_BUFFERED = 1024 * 1024;
const encoder = new TextEncoder(), decoder = new TextDecoder();

export function peerPacket(kind: number, seq = 0, body = new Uint8Array()): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(9 + body.length);
  bytes[0] = kind;
  new DataView(bytes.buffer).setBigUint64(1, BigInt(seq));
  bytes.set(body, 9);
  return bytes;
}

export type P2PChannelOptions = {
  sendRelay: (bytes: Uint8Array<ArrayBuffer>) => void;
  relayBufferedAmount: () => number;
  // Resolve only after the consumer accepts bytes; ACK backpressure bounds memory.
  onData: (bytes: Uint8Array<ArrayBuffer>, direct: boolean) => Promise<void>;
  onTransport: (transport: ChannelTransport) => void;
  onError: (error: Error) => void;
  onActivity?: () => void;
};

type PeerSignal = { type: string; sdp?: string; candidate?: RTCIceCandidateInit; generation?: number };

export class P2PChannel {
  private options: P2PChannelOptions;
  private peer?: RTCPeerConnection;
  private channel?: RTCDataChannel;
  private started = false;
  private closed = false;
  private direct = false;
  private route: ChannelTransport = "connecting";
  private nextSeq = 1;
  private receiveNext = 1;
  private reading = false;
  private pending = new Map<number, { bytes: Uint8Array<ArrayBuffer>; sent: number }>();
  private buffered = new Map<number, { body: Uint8Array<ArrayBuffer>; direct: boolean }>();
  private delivered = new Map<number, boolean>();
  private waiters = new Set<() => void>();
  private writeTail: Promise<void> = Promise.resolve();
  private timer?: ReturnType<typeof setInterval>;
  private ticks = 0;
  private servers: string[] = [];
  private negotiating = false;
  private nextNegotiation = 0;
  private generation = 0;
  private remoteCandidates: RTCIceCandidateInit[] = [];
  private signaling: Promise<void> = Promise.resolve();

  constructor(options: P2PChannelOptions) { this.options = options; }
  get transport(): ChannelTransport { return this.route; }

  // Called after relay authorization/readiness, never after waiting for ICE.
  start(servers: string[]) {
    if (this.closed || this.started) return;
    this.started = true;
    this.setTransport("relay");
    this.timer = setInterval(() => this.tick(), 250);
    this.servers = servers.slice();
    void this.negotiate();
  }

  async waitWritable(): Promise<void> {
    while (!this.closed && (this.pending.size >= WINDOW || this.options.relayBufferedAmount() > MAX_BUFFERED)) {
      await new Promise<void>((resolve) => this.waiters.add(resolve));
    }
    if (this.closed || !this.started) throw new Error("P2P channel is closed or not ready");
  }

  write(body: Uint8Array<ArrayBuffer>): Promise<void> {
    // Concurrent callers share one sequence stream, not separate directional policies.
    const data = body.slice();
    const operation = this.writeTail.catch(() => {}).then(async () => {
      for (let offset = 0; offset < data.length; offset += MAX_PEER_PAYLOAD) {
        await this.waitWritable();
        const seq = this.nextSeq++;
        const bytes = peerPacket(PEER_DATA, seq, data.subarray(offset, offset + MAX_PEER_PAYLOAD));
        this.pending.set(seq, { bytes, sent: Date.now() });
        this.send(bytes);
      }
    }).catch((error) => { this.fail(error); throw error; });
    this.writeTail = operation;
    return operation;
  }

  receive(buffer: ArrayBuffer, fromDirect = false) {
    if (this.closed) return;
    if (buffer.byteLength < 9 || buffer.byteLength > 65536) { this.fail(new Error("Invalid P2P packet")); return; }
    const bytes = new Uint8Array(buffer);
    const seq = Number(new DataView(buffer).getBigUint64(1));
    this.options.onActivity?.();
    if (bytes[0] === ACK) {
      if (this.pending.delete(seq)) this.wakeWriters();
    } else if (bytes[0] === PEER_DATA) {
      if (!Number.isSafeInteger(seq) || seq < 1 || bytes.length > MAX_PEER_PAYLOAD + 9) { this.fail(new Error("Invalid P2P sequence")); return; }
      if (seq < this.receiveNext) { this.acknowledge(seq, this.delivered.get(seq) || false, fromDirect); return; }
      if (seq >= this.receiveNext + WINDOW) return;
      if (!this.buffered.has(seq)) this.buffered.set(seq, { body: bytes.slice(9), direct: fromDirect });
      void this.drain();
    } else if (bytes[0] === SIGNAL) {
      this.signaling = this.signaling.then(() => this.answer(bytes.slice(9)));
    } else if (bytes[0] === PROBE && this.channel?.readyState === "open") {
      try {
        this.channel.send(peerPacket(PROBE_ACK));
        // Our first probe may have arrived before the remote installed its path.
        if (fromDirect && !this.direct) this.channel.send(peerPacket(PROBE));
      } catch { this.disableDirect(); }
    } else if (bytes[0] === PROBE_ACK && fromDirect && this.channel?.readyState === "open") {
      this.direct = true;
      this.setTransport("direct");
    }
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    clearInterval(this.timer);
    this.wakeWriters();
    this.channel?.close();
    this.peer?.close();
    this.pending.clear(); this.buffered.clear(); this.delivered.clear();
    this.remoteCandidates = [];
  }

  private setTransport(transport: ChannelTransport) {
    if (this.route !== transport) { this.route = transport; this.options.onTransport(transport); }
  }
  private disableDirect() { this.direct = false; this.setTransport("relay"); }
  private wakeWriters() { for (const resolve of this.waiters) resolve(); this.waiters.clear(); }
  private fail(error: unknown) {
    if (this.closed) return;
    this.options.onError(error instanceof Error ? error : new Error(String(error)));
    this.close();
  }
  private send(bytes: Uint8Array<ArrayBuffer>) {
    if (this.direct && this.channel?.readyState === "open" && this.channel.bufferedAmount < MAX_BUFFERED) {
      try { this.channel.send(bytes); return; } catch { /* Retry the same sequence on relay. */ }
    }
    this.disableDirect();
    this.options.sendRelay(bytes);
  }
  private acknowledge(seq: number, deliveredDirect: boolean, fromDirect: boolean) {
    const bytes = peerPacket(ACK, seq, new Uint8Array([deliveredDirect ? 1 : 0]));
    if (fromDirect && deliveredDirect && this.channel?.readyState === "open") {
      try { this.channel.send(bytes); return; } catch { /* Relay preserves receipt accounting. */ }
    }
    try { this.options.sendRelay(bytes); } catch (error) { this.fail(error); }
  }
  private async drain() {
    if (this.reading) return;
    this.reading = true;
    try {
      while (!this.closed) {
        const item = this.buffered.get(this.receiveNext);
        if (!item) break;
        await this.options.onData(item.body, item.direct);
        if (this.closed) return;
        this.buffered.delete(this.receiveNext);
        this.delivered.set(this.receiveNext, item.direct);
        this.delivered.delete(this.receiveNext - WINDOW * 2);
        this.acknowledge(this.receiveNext++, item.direct, item.direct);
      }
    } catch (error) { this.fail(error); }
    finally { this.reading = false; }
  }
  private tick() {
    if (this.closed) return;
    try {
      const now = Date.now();
      for (const value of this.pending.values()) {
        if (now - value.sent > 1000) {
          this.disableDirect();
          if (this.options.relayBufferedAmount() < MAX_BUFFERED) {
            this.options.sendRelay(value.bytes);
            value.sent = now;
          }
        }
      }
      this.ticks++;
      if (this.ticks % 4 === 0) {
        if (this.channel?.readyState === "open") {
          try { this.channel.send(peerPacket(PROBE)); } catch { this.disableDirect(); }
        }
        // Keep the authenticated control path online even while data is direct.
        this.options.sendRelay(peerPacket(PROBE));
      }
      if (!this.direct && !this.negotiating && now >= this.nextNegotiation && typeof RTCPeerConnection !== "undefined") void this.negotiate();
      this.wakeWriters();
    } catch (error) { this.fail(error); }
  }
  private async answer(body: Uint8Array<ArrayBuffer>) {
    try {
      if (this.closed || !this.peer) return;
      const message = JSON.parse(decoder.decode(body)) as PeerSignal;
      const peer = this.peer;
      if (message.generation !== undefined && message.generation !== this.generation) return;
      if (message.type === "candidate" && message.candidate) {
        if (peer.remoteDescription) await peer.addIceCandidate(message.candidate);
        else if (this.remoteCandidates.length < WINDOW) this.remoteCandidates.push(message.candidate);
      } else if (message.type === "answer") {
        await peer.setRemoteDescription({ type: "answer", sdp: message.sdp });
        if (this.closed || this.peer !== peer) return;
        const candidates = this.remoteCandidates;
        this.remoteCandidates = [];
        for (const candidate of candidates) await peer.addIceCandidate(candidate);
      }
    } catch { if (!this.closed) this.disableDirect(); }
  }
  private signal(message: PeerSignal) {
    this.options.sendRelay(peerPacket(SIGNAL, 0, encoder.encode(JSON.stringify(message))));
  }
  private async negotiate() {
    if (this.closed || this.negotiating || typeof RTCPeerConnection === "undefined") return;
    this.negotiating = true;
    // Match the Agent negotiator: failed direct paths retry without blocking relay.
    this.nextNegotiation = Date.now() + 20_000;
    try {
      const old = this.peer;
      const generation = ++this.generation;
      this.remoteCandidates = [];
      const peer = this.peer = new RTCPeerConnection({ iceServers: this.servers.length ? [{ urls: this.servers }] : [] });
      this.disableDirect();
      old?.close();
      peer.onicecandidate = (event) => {
        if (this.closed || this.peer !== peer || !event.candidate) return;
        try { this.signal({ type: "candidate", candidate: event.candidate.toJSON(), generation }); }
        catch (error) { this.fail(error); }
      };
      const channel = this.channel = peer.createDataChannel("tcp-tunnel");
      channel.binaryType = "arraybuffer";
      channel.onmessage = (event: MessageEvent<ArrayBuffer>) => { if (this.peer === peer) this.receive(event.data, true); };
      channel.onopen = () => {
        if (this.closed || this.peer !== peer) return;
        // Only a direct probe ACK confirms the remote installed its path.
        try { channel.send(peerPacket(PROBE)); } catch { this.disableDirect(); }
      };
      channel.onclose = channel.onerror = () => { if (!this.closed && this.peer === peer) this.disableDirect(); };
      const offer = await peer.createOffer();
      if (this.closed || this.peer !== peer) return;
      await peer.setLocalDescription(offer);
      // SDP goes now; candidate events follow independently, including while idle.
      if (!this.closed && this.peer === peer) this.signal({ type: "offer", sdp: offer.sdp, generation });
    } catch { if (!this.closed) this.disableDirect(); }
    finally { this.negotiating = false; }
  }
}
