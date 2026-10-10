# Web P2P Download Implementation Plan

> Execute in this session. The user authorized implementation, commit, push and deployment; use GitHub Actions for builds and verification.

**Goal:** Browser downloads attempt the same Agent WebRTC direct path as uploads, with relay continuity, existing SFTP permissions and audit records.

**Architecture:** Generalize the upload record/status and WebSocket bridge into file transfer components without changing their existing wire values. Add a server-selected download source opened locally or through the existing pinned SSH hop helper. The browser negotiates once, then sends a start record; the Agent sends bounded CRC32 records and a finish record, computes SHA256 and waits for the receiver receipt. Sequence ACKs after sink writes bound buffering and allow retransmission without duplicate file bytes. Prefer the browser save-file API for streaming, with bounded Blob fallback and native HTTP downloads for oversized files on unsupported browsers.

**Tech Stack:** Go, Pion WebRTC, tunnel.Conn, pkg/sftp, TypeScript/React, Playwright, GitHub Actions.

- [x] Generalize shared records, status and server bridge; retain upload interoperability. Add sender/source tests for exact length, empty, directory and missing sources.
- [x] Add download Agent handler and authenticated route. Verify local and delegated SSH bytes, cancellation, denied policy, origin and legacy fallback.
- [x] Generalize browser transport, ordered receiver and sink; show download progress, speed, direct/relay mode and cancellation using existing toast styles. Keep upload behavior unchanged.
- [x] Verify real browser direct download with zero relayed payload, interruption continuity, forced relay, empty files, cancellation, legacy fallback and streaming sink. Run existing upload regressions in CI.
- [ ] Review, commit, push, release, deploy both existing environments from checksum-verified packages and verify health, frontend and Agent assets.

**Integrity:** CRC32 per record, exact advertised file length and encrypted transports. SHA256 is computed by the Agent and retained in audit. The browser accepts status only from its authenticated relay. Aborting a streaming save aborts its writable file; Blob downloads are offered only after successful completion.

**Verification:** GitHub Actions run `38031042993` passed the frontend build, race-checked transfer/source/destination/authorization tests, existing tunnel tests, and both Chromium upload/download suites. Downloads cover direct delivery with zero relayed content, forced relay, direct interruption, empty files, cancellation, legacy HTTP fallback, slow streaming saves, streaming cancellation, corrupted chunks, native large-file fallback and disk-write failure. Editing the path now closes its breadcrumb menu so it cannot block file activation.
