import assert from "node:assert/strict";
import test from "node:test";
import { closeFileTransfers, downloadFileP2P } from "../src/fileTransfer.ts";

const packet = (kind) => { const bytes = new Uint8Array(9); bytes[0] = kind; return bytes.buffer; };
const flush = () => new Promise((resolve) => setImmediate(resolve));

test("file data waits for the remote peer to confirm the direct path", async () => {
  const saved = Object.getOwnPropertyDescriptors(globalThis);
  const relay = [], direct = [];
  let socket, channel;
  class FakeSocket {
    static OPEN = 1;
    readyState = 1;
    bufferedAmount = 0;
    constructor() {
      socket = this;
      queueMicrotask(() => this.onmessage({ data: JSON.stringify({ type: "ready", size: 0, path: "/test", stun_servers: [] }) }));
    }
    send(bytes) { relay.push(new Uint8Array(bytes)[0]); }
    close() {}
  }
  class FakePeer {
    iceGatheringState = "complete";
    createDataChannel() {
      channel = { readyState: "connecting", bufferedAmount: 0, send: (bytes) => direct.push(new Uint8Array(bytes)[0]), close() {} };
      return channel;
    }
    async createOffer() { return { type: "offer", sdp: "test" }; }
    async setLocalDescription(description) {
      this.localDescription = description;
      channel.readyState = "open";
      queueMicrotask(() => channel.onopen());
    }
    close() {}
  }
  const controller = new AbortController();
  let operation;
  try {
    Object.defineProperties(globalThis, {
      WebSocket: { configurable: true, value: FakeSocket },
      RTCPeerConnection: { configurable: true, value: FakePeer },
      location: { configurable: true, value: new URL("https://test.example/") },
    });
    operation = downloadFileP2P("handshake", "/test", { maxSize: 1024, write: async () => {} }, undefined, controller.signal);
    operation.catch(() => {});
    await flush();
    assert.ok(direct.includes(6), "Opening the local channel must probe the remote peer");
    assert.ok(!direct.includes(1) && !relay.includes(1), "No file request may start before confirmation");
    socket.onmessage({ data: packet(7) });
    await flush();
    assert.ok(!direct.includes(1), "A relayed probe ACK does not confirm the direct path");
    channel.onmessage({ data: packet(7) });
    await flush();
    assert.ok(direct.includes(1), "The confirmed peer must receive the download request directly");
    assert.ok(!relay.includes(1), "Startup must not relay file data");
  } finally {
    controller.abort();
    if (operation) await assert.rejects(operation, { name: "AbortError" });
    closeFileTransfers("handshake");
    for (const key of ["WebSocket", "RTCPeerConnection", "location"]) {
      if (saved[key]) Object.defineProperty(globalThis, key, saved[key]);
      else delete globalThis[key];
    }
  }
});
