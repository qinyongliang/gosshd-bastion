# Web P2P Upload Implementation Plan

> Execute in this session; the user authorized implementation, commit, push and deployment. Builds and verification run in GitHub Actions, following the deployment reference chat.

**Goal:** Upload from the browser directly to an Agent when ICE connectivity is available, retaining relay fallback, upload policy enforcement and trusted completion audits.

**Architecture:** Reuse the existing WebRTC negotiator and reliable tunnel byte stream. A new upload-only Agent stream receives a server-selected destination and delegated, pinned SSH hops. Browser binary packets are relayed through an authenticated same-origin WebSocket until direct connectivity is established. The Agent checks each chunk, writes a temporary sibling file, validates the declared length and commits only after an explicit finish record. Completion and byte progress always return on the authenticated Agent control stream.

**Tech Stack:** Go, Pion WebRTC, existing tunnel.Conn, gorilla WebSocket, pkg/sftp, TypeScript/React, Playwright, GitHub Actions.

---

- [x] Add protocol upload request/status definitions, a bounded upload-only sink, and Agent dispatch. Verify corrupted chunks, short/oversized uploads, cancellation cleanup, replacement and SSH-backed uploads in Go tests.
- [x] Add the authenticated upload WebSocket route. Validate filename, length, existing SFTP permissions and target ownership before opening an Agent stream; return unsupported for old Agents/pure SSH so the browser uses the legacy endpoint. Audit only Agent-confirmed completion; reject client-supplied status packets. Verify authorization, origin, fallback and trusted results in server tests.
- [x] Add a browser tunnel sender with a 64-packet window, sequence ACKs, retransmission over relay, WebRTC negotiation and cancellation. Use CRC32 per record plus encrypted transport integrity; show Agent-confirmed progress and direct/relay status in both languages. Verify real Chromium-to-Agent direct upload, forced relay, interruption fallback, empty files and cancellation.
- [ ] Extend release checks, document behavior, review diff, commit and push to main. Run the release workflow, fix failures, publish a fresh version, deploy both existing environments using checksum-verified release packages and backups, then verify health/frontend/Agent assets.

**Limits:** P2P needs a current Agent and reachable ICE candidates; the control WebSocket remains required. HTTP-only pages may fall back to relay when the browser restricts WebRTC. No new broad SFTP access or SSH credentials are exposed to the browser.

**Verification:** GitHub Actions run 38021793103 passed the frontend build, upload tests with the race detector, existing tunnel tests, pooled-writer regression, and all six real Chromium upload scenarios. A follow-up moves the upload toast into the existing React portal so notification overlays cannot block cancellation.
