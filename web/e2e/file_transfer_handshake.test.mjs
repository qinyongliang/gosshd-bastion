import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import { closeFileTransfers, downloadFileP2P, uploadFileP2P } from "../src/fileTransfer.ts";

import { readFilePreview } from "../src/filePreview.ts";

const flush = () => new Promise((resolve) => setImmediate(resolve));
const packet = (kind, seq = 0, body = new Uint8Array()) => {
  const bytes = new Uint8Array(9 + body.length);
  bytes[0] = kind;
  new DataView(bytes.buffer).setBigUint64(1, BigInt(seq));
  bytes.set(body, 9);
  return bytes.buffer;
};
const kinds = (packets) => packets.filter((bytes) => typeof bytes !== "string").map((bytes) => bytes[0]);
const chunk = (text) => {
  const data = new TextEncoder().encode(text);
  let crc = 0xffffffff;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1;
  }
  const record = new Uint8Array(9 + data.length);
  record[0] = 1;
  const view = new DataView(record.buffer);
  view.setUint32(1, 4 + data.length);
  view.setUint32(5, (crc ^ 0xffffffff) >>> 0);
  record.set(data, 9);
  return record;
};

async function fixture(t, { rtc = true, failOffer = false } = {}) {
  const saved = Object.getOwnPropertyDescriptors(globalThis);
  const relay = [], direct = [], operations = [];
  let socket, channel, peer, sockets = 0, offers = 0;
  const target = t.name;
  class FakeSocket {
    static OPEN = 1;
    readyState = 1;
    bufferedAmount = 0;
    constructor() { socket = this; sockets++; }
    send(bytes) { relay.push(typeof bytes === "string" ? bytes : new Uint8Array(bytes).slice()); }
    receive(data) { this.onmessage({ data }); }
    close() { this.readyState = 3; }
  }
  class FakePeer {
    iceGatheringState = "gathering";
    listeners = new Set();
    constructor() { peer = this; }
    createDataChannel() {
      channel = {
        readyState: "connecting", bufferedAmount: 0,
        send: (bytes) => direct.push(new Uint8Array(bytes).slice()),
        close() { this.readyState = "closed"; },
        receive(data) { this.onmessage({ data }); },
        open() { this.readyState = "open"; this.onopen(); },
      };
      return channel;
    }
    async createOffer() { offers++; if (failOffer) throw new Error("ICE failed"); return { type: "offer", sdp: "test" }; }
    async setLocalDescription(description) { this.localDescription = description; }
    async setRemoteDescription(description) { this.remoteDescription = description; }
    addEventListener(type, listener) { this.listeners.add(listener); }
    removeEventListener(type, listener) { this.listeners.delete(listener); }
    gather() { this.iceGatheringState = "complete"; this.onicecandidate?.({ candidate: { toJSON: () => ({ candidate: "candidate:1 1 udp 12345 127.0.0.1 5000 typ host", sdpMid: "0", sdpMLineIndex: 0 }) } }); }
    close() {}
  }
  Object.defineProperties(globalThis, {
    WebSocket: { configurable: true, value: FakeSocket },
    RTCPeerConnection: { configurable: true, value: rtc ? FakePeer : undefined },
    location: { configurable: true, value: new URL("https://test.example/") },
  });
  t.after(async () => {
    closeFileTransfers(target);
    await Promise.allSettled(operations);
    for (const key of ["WebSocket", "RTCPeerConnection", "location"]) {
      if (saved[key]) Object.defineProperty(globalThis, key, saved[key]);
      else delete globalThis[key];
    }
  });
  return {
    relay, direct,
    get socket() { return socket; }, get channel() { return channel; }, get peer() { return peer; },
    get sockets() { return sockets; }, get offers() { return offers; },
    async download(size, sink, progress, reuse = false) {
      const operation = downloadFileP2P(target, "/test", { maxSize: Math.max(1024, size), write: sink }, progress);
      operation.catch(() => {}); operations.push(operation);
      await flush();
      socket.receive(JSON.stringify({ type: "ready", size, path: "/test", stun_servers: [], reuse }));
      await flush();
      return { operation };
    },
    async preview(size, progress, signal) {
      const operation = readFilePreview(target, "/test.png", size, progress, signal);
      operation.catch(() => {}); operations.push(operation);
      await flush();
      socket.receive(JSON.stringify({ type: "ready", size, path: "/test.png", stun_servers: [], reuse: true }));
      await flush();
      return { operation };
    },
    async upload() {
      const operation = uploadFileP2P(target, "/test", new File([new Uint8Array(1024 * 1024)], "test.bin"));
      operation.catch(() => {}); operations.push(operation);
      await flush();
      socket.receive(JSON.stringify({ type: "ready", size: 1024 * 1024, path: "/test", stun_servers: [] }));
      await flush();
    },
    complete(text) {
      socket.receive(packet(9, 0, new TextEncoder().encode(JSON.stringify({
        type: "complete", loaded: Buffer.byteLength(text), sha256: createHash("sha256").update(text).digest("hex"), direct: channel?.readyState === "open",
      }))));
    },
  };
}

test("download starts on relay while ICE gathers, then migrates without duplicate or reordered bytes", async (t) => {
  const f = await fixture(t);
  const chunks = [], progress = [];
  const { operation } = await f.download(10, async (bytes) => chunks.push(Buffer.from(bytes)), (value) => progress.push(value));
  assert.ok(kinds(f.relay).includes(1), "Ready must immediately relay the download start, without waiting for ICE");
  assert.ok(kinds(f.relay).includes(4), "SDP must be sent even while ICE is still gathering");
  assert.equal(progress.at(-1).transport, "relay");
  f.socket.receive(packet(1, 1, chunk("relay")));
  await flush();
  assert.ok(f.relay.some((bytes) => bytes[0] === 2), "Relayed data is accepted before ICE completes");

  f.peer.gather();
  await flush();
  assert.ok(kinds(f.relay).includes(4));
  f.channel.open();
  assert.ok(kinds(f.direct).includes(6), "Opening the local channel must probe the remote peer");
  f.socket.receive(packet(7));
  assert.equal(progress.at(-1).transport, "relay", "A relayed probe ACK cannot confirm direct readiness");
  f.channel.receive(packet(7));
  assert.equal(progress.at(-1).transport, "direct");
  f.channel.receive(packet(1, 1, chunk("relay"))); // Duplicate across the path switch.
  f.channel.receive(packet(1, 3, new Uint8Array([2, 0, 0, 0, 0]))); // Finish arrives out of order.
  f.channel.receive(packet(1, 2, chunk("later")));
  await flush();
  assert.equal(Buffer.concat(chunks).toString(), "relaylater");
  assert.ok(kinds(f.direct).includes(1), "The final receipt uses the confirmed direct path");
  f.complete("relaylater");
  assert.deepEqual(await operation, { path: "/test" });
});

test("downloads start immediately on relay when WebRTC is missing or ICE fails", async (t) => {
  for (const options of [{ rtc: false }, { failOffer: true }]) {
    await t.test(JSON.stringify(options), async (t) => {
      const f = await fixture(t, options);
      await f.download(0, async () => {});
      assert.ok(kinds(f.relay).includes(1));
      assert.ok(!kinds(f.direct).includes(1));
    });
  }
});

test("an empty reusable download can finish before ICE, then upgrade the idle session for the next file", async (t) => {
  const f = await fixture(t);
  const { operation } = await f.download(0, async () => {}, undefined, true);
  assert.ok(kinds(f.relay).includes(1), "An empty file must not wait for ICE");
  f.socket.receive(packet(1, 1, new Uint8Array([2, 0, 0, 0, 0])));
  await flush();
  f.complete("");
  await operation;
  const signals = f.relay.filter((bytes) => bytes[0] === 4).length;
  f.peer.gather();
  await flush();
  assert.equal(f.relay.filter((bytes) => bytes[0] === 4).length, signals + 1, "Idle reusable sessions must continue trickling candidates");
  f.channel.open();
  f.channel.receive(packet(7));
  const { operation: next } = await f.download(0, async () => {}, undefined, true);
  assert.equal(f.sockets, 1);
  assert.equal(f.offers, 1);
  assert.ok(kinds(f.direct).includes(1), "The next file reuses the already confirmed direct path");
  f.channel.receive(packet(1, 2, new Uint8Array([2, 0, 0, 0, 0])));
  await flush();
  f.complete("");
  await next;
});

test("uploads start on relay and only use direct after a direct probe ACK", async (t) => {
  const f = await fixture(t);
  await f.upload();
  assert.ok(kinds(f.relay).includes(1), "Upload data must not wait for ICE");
  f.channel.open();
  f.socket.receive(packet(7));
  f.socket.receive(packet(2, 1));
  await flush();
  assert.ok(!kinds(f.direct).includes(1), "Local open or relay confirmation is insufficient");
  f.channel.receive(packet(7));
  f.socket.receive(packet(2, 2));
  await flush();
  assert.ok(kinds(f.direct).includes(1), "New upload packets must switch to the confirmed direct path");
});

test("closing a download during ICE gathering prevents late signaling", async (t) => {
  const f = await fixture(t);
  const { operation } = await f.download(10, async () => {});
  assert.ok(kinds(f.relay).includes(1));
  const signals = f.relay.filter((bytes) => bytes[0] === 4).length;
  closeFileTransfers(t.name);
  await assert.rejects(operation, { name: "AbortError" });
  f.peer.gather();
  await flush();
  assert.equal(f.relay.filter((bytes) => bytes[0] === 4).length, signals, "A closed session must not send a late candidate");
});

test("download sink writes are batched with bounded memory rather than one disk write per 8 KiB record", async (t) => {
  const f = await fixture(t, { rtc: false });
  const text = "x".repeat(8183), expected = text.repeat(70), writes = [];
  const { operation } = await f.download(expected.length, async (bytes) => writes.push(Buffer.from(bytes)));
  for (let seq = 1; seq <= 70; seq++) {
    f.socket.receive(packet(1, seq, chunk(text)));
    await flush();
  }
  f.socket.receive(packet(1, 71, new Uint8Array([2, 0, 0, 0, 0])));
  await flush();
  f.complete(expected);
  await operation;
  assert.equal(Buffer.concat(writes).toString(), expected);
  assert.equal(writes.length, 3, "70 protocol records should require only three bounded sink writes");
  assert.ok(writes.every((bytes) => bytes.length <= 8183 * 32));
});


test("preview reads use the shared relay-to-direct channel, deduplicate bytes and reuse the session", async (t) => {
  const f = await fixture(t);
  const progress = [];
  const { operation } = await f.preview(10, (value) => progress.push(value));
  assert.equal(progress.at(-1).transport, "relay");
  f.socket.receive(packet(1, 1, chunk("relay")));
  await flush();
  f.channel.open();
  f.channel.receive(packet(7));
  assert.equal(progress.at(-1).transport, "direct");
  f.channel.receive(packet(1, 1, chunk("relay")));
  f.channel.receive(packet(1, 3, new Uint8Array([2, 0, 0, 0, 0])));
  f.channel.receive(packet(1, 2, chunk("later")));
  await flush();
  f.complete("relaylater");
  const blob = await operation;
  assert.equal(blob.type, "image/png");
  assert.equal(await blob.text(), "relaylater");
  const { operation: next } = await f.preview(0);
  f.channel.receive(packet(1, 4, new Uint8Array([2, 0, 0, 0, 0])));
  await flush();
  f.complete("");
  assert.equal((await next).size, 0);
  assert.equal(f.sockets, 1);
  assert.equal(f.offers, 1);
});

test("closing a preview aborts the active shared-channel read", async (t) => {
  const f = await fixture(t);
  const controller = new AbortController();
  const { operation } = await f.preview(10, undefined, controller.signal);
  controller.abort();
  await assert.rejects(operation, { name: "AbortError" });
  assert.equal(f.socket.readyState, 3);
});
