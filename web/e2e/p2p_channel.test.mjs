import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { P2PChannel, PEER_DATA, MAX_PEER_PAYLOAD, peerPacket } from "../src/p2pChannel.ts";

const flush = () => new Promise((resolve) => setImmediate(resolve));
const sequence = (bytes) => Number(new DataView(bytes.buffer, bytes.byteOffset).getBigUint64(1));
async function fixture(t, { rtc = false, onData = async () => {} } = {}) {
  const saved = Object.getOwnPropertyDescriptor(globalThis, "RTCPeerConnection");
  const relay = [], direct = [], routes = [], errors = [];
  let rtcChannel, peer, offers = 0;
  class Peer {
    iceGatheringState = "gathering";
    added = [];
    constructor() { peer = this; }
    createDataChannel() {
      rtcChannel = {
        readyState: "connecting", bufferedAmount: 0, failProbe: false,
        send(bytes) { if (this.failProbe && bytes[0] === 6) throw new Error("Direct probe failed"); direct.push(bytes.slice()); },
        close() { this.readyState = "closed"; },
      };
      return rtcChannel;
    }
    async createOffer() { offers++; return { type: "offer", sdp: "test" }; }
    async setLocalDescription(value) { this.localDescription = value; }
    async setRemoteDescription(value) { this.remoteDescription = value; }
    async addIceCandidate(value) { this.added.push(value); }
    close() {}
  }
  Object.defineProperty(globalThis, "RTCPeerConnection", { configurable: true, value: rtc ? Peer : undefined });
  const channel = new P2PChannel({
    sendRelay: (bytes) => relay.push(bytes.slice()), relayBufferedAmount: () => 0,
    onData, onTransport: (route) => routes.push(route), onError: (error) => errors.push(error),
  });
  t.after(() => {
    channel.close();
    if (saved) Object.defineProperty(globalThis, "RTCPeerConnection", saved);
    else delete globalThis.RTCPeerConnection;
  });
  channel.start([]);
  await flush();
  return { channel, relay, direct, routes, errors, get rtcChannel() { return rtcChannel; }, get offers() { return offers; }, get peer() { return peer; } };
}

test("shared channel starts on relay and splits arbitrary business payloads into the same sequence stream", async (t) => {
  const f = await fixture(t);
  const data = new Uint8Array(MAX_PEER_PAYLOAD * 2 + 31).map((_, i) => i % 251);
  await f.channel.write(data);
  const packets = f.relay.filter((bytes) => bytes[0] === PEER_DATA);
  assert.deepEqual(packets.map(sequence), [1, 2, 3]);
  assert.deepEqual(Buffer.concat(packets.map((bytes) => bytes.subarray(9))), Buffer.from(data));
  assert.deepEqual(f.routes, ["relay"]);
});

test("shared channel orders and deduplicates data, acknowledging only after its consumer accepts it", async (t) => {
  const received = [];
  let release;
  const blocked = new Promise((resolve) => { release = resolve; });
  const f = await fixture(t, { onData: async (bytes) => { await blocked; received.push(bytes[0]); } });
  f.channel.receive(peerPacket(PEER_DATA, 2, new Uint8Array([22])).buffer);
  f.channel.receive(peerPacket(PEER_DATA, 1, new Uint8Array([11])).buffer);
  await flush();
  assert.equal(f.relay.length, 0, "Slow consumers must apply ACK backpressure");
  release();
  await flush();
  f.channel.receive(peerPacket(PEER_DATA, 1, new Uint8Array([11])).buffer);
  assert.deepEqual(received, [11, 22]);
  assert.deepEqual(f.relay.map(sequence), [1, 2, 1]);
});

test("shared channel never treats a locally open channel or relayed probe ACK as direct confirmation", async (t) => {
  const f = await fixture(t, { rtc: true });
  f.rtcChannel.readyState = "open";
  f.rtcChannel.onopen();
  f.channel.receive(peerPacket(7).buffer);
  await f.channel.write(new Uint8Array([11]));
  assert.equal(f.direct.filter((bytes) => bytes[0] === PEER_DATA).length, 0);
  f.channel.receive(peerPacket(7).buffer, true);
  await f.channel.write(new Uint8Array([22]));
  assert.equal(f.direct.filter((bytes) => bytes[0] === PEER_DATA).length, 1);
  assert.equal(f.channel.transport, "direct");
});

test("shared channel retransmits silent direct loss over relay without replacing sequence numbers", async (t) => {
  t.mock.timers.enable({ apis: ["setInterval", "Date"], now: 0 });
  const f = await fixture(t, { rtc: true });
  f.rtcChannel.readyState = "open";
  f.channel.receive(peerPacket(7).buffer, true);
  await f.channel.write(new Uint8Array([11, 22]));
  const sent = f.direct.find((bytes) => bytes[0] === PEER_DATA);
  t.mock.timers.tick(1250);
  const retried = f.relay.find((bytes) => bytes[0] === PEER_DATA);
  assert.deepEqual(retried, sent);
  assert.equal(f.channel.transport, "relay");
  f.channel.receive(peerPacket(2, sequence(sent)).buffer);
  f.channel.receive(peerPacket(7).buffer, true);
  await f.channel.write(new Uint8Array([33]));
  assert.equal(sequence(f.direct.filter((bytes) => bytes[0] === PEER_DATA).at(-1)), 2);
  assert.deepEqual(f.errors, []);
});

test("shared channel falls back instead of closing the stream when a direct health probe throws", async (t) => {
  t.mock.timers.enable({ apis: ["setInterval", "Date"], now: 0 });
  const f = await fixture(t, { rtc: true });
  f.rtcChannel.readyState = "open";
  f.channel.receive(peerPacket(7).buffer, true);
  f.rtcChannel.failProbe = true;
  t.mock.timers.tick(1000);
  await f.channel.write(new Uint8Array([44]));
  assert.ok(f.relay.some((bytes) => bytes[0] === PEER_DATA));
  assert.deepEqual(f.errors, []);
});

test("shared channel bounds the send window and rejects blocked writers when closed", async (t) => {
  const f = await fixture(t);
  const operation = f.channel.write(new Uint8Array(MAX_PEER_PAYLOAD * 65));
  operation.catch(() => {});
  await flush();
  assert.equal(f.relay.filter((bytes) => bytes[0] === PEER_DATA).length, 64);
  f.channel.close();
  await assert.rejects(operation, /closed/);
});

test("shared channel retries ICE in the background while retaining relay and the existing sequence stream", async (t) => {
  t.mock.timers.enable({ apis: ["setInterval", "Date"], now: 0 });
  const f = await fixture(t, { rtc: true });
  await f.channel.write(new Uint8Array([11]));
  f.channel.receive(peerPacket(2, 1).buffer);
  assert.equal(f.offers, 1);
  t.mock.timers.tick(20_000);
  await flush();
  assert.equal(f.offers, 2, "Failed ICE must not leave a reusable channel permanently relayed");
  await f.channel.write(new Uint8Array([22]));
  assert.deepEqual(f.relay.filter((bytes) => bytes[0] === PEER_DATA).map(sequence), [1, 2]);
  f.rtcChannel.readyState = "open";
  f.channel.receive(peerPacket(7).buffer, true);
  await f.channel.write(new Uint8Array([33]));
  assert.equal(sequence(f.direct.find((bytes) => bytes[0] === PEER_DATA)), 3);
});

test("shared channel re-probes immediately when the remote path opens after the first local probe", async (t) => {
  const f = await fixture(t, { rtc: true });
  f.rtcChannel.readyState = "open";
  f.rtcChannel.onopen(); // The remote may not have installed its path yet.
  f.channel.receive(peerPacket(6).buffer, true); // Now the remote announces its ready path.
  assert.deepEqual(f.direct.slice(-2).map((bytes) => bytes[0]), [7, 6], "Do not wait for the periodic timer to confirm the now-ready peer");
  assert.equal(f.channel.transport, "relay", "A probe alone does not replace the direct ACK requirement");
  f.channel.receive(peerPacket(7).buffer, true);
  assert.equal(f.channel.transport, "direct");
});

test("shared channel sends SDP before gathering and buffers trickled candidates until the answer arrives", async (t) => {
  const f = await fixture(t, { rtc: true });
  const decode = (bytes) => JSON.parse(new TextDecoder().decode(bytes.subarray(9)));
  const offer = f.relay.filter((bytes) => bytes[0] === 4).map(decode).find((message) => message.type === "offer");
  assert.equal(f.peer.iceGatheringState, "gathering");
  assert.ok(offer, "SDP must not wait for ICE gathering completion");
  const candidate = { candidate: "candidate:1 1 udp 12345 127.0.0.1 5000 typ host", sdpMid: "0", sdpMLineIndex: 0 };
  f.peer.onicecandidate({ candidate: { toJSON: () => candidate } });
  assert.deepEqual(decode(f.relay.at(-1)), { type: "candidate", candidate, generation: offer.generation });
  const receive = async (message) => {
    f.channel.receive(peerPacket(4, 0, new TextEncoder().encode(JSON.stringify(message))).buffer);
    await flush();
  };
  await receive({ type: "candidate", candidate, generation: offer.generation });
  assert.deepEqual(f.peer.added, [], "Candidates cannot be applied before remote SDP");
  await receive({ type: "answer", sdp: "test", generation: offer.generation });
  assert.deepEqual(f.peer.added, [candidate]);
  await receive({ type: "candidate", candidate, generation: offer.generation - 1 });
  assert.equal(f.peer.added.length, 1, "Stale negotiation candidates must be ignored");
});

test("production P2P startup has no fixed gathering delay or sleep", () => {
  const browser = readFileSync(new URL("../src/p2pChannel.ts", import.meta.url), "utf8");
  const agent = readFileSync(new URL("../../internal/tunnel/direct.go", import.meta.url), "utf8");
  assert.doesNotMatch(browser, /setTimeout|finishGathering|icegatheringstatechange/);
  assert.doesNotMatch(agent, /GatheringCompletePromise|time\.NewTimer|time\.Sleep/);
});
