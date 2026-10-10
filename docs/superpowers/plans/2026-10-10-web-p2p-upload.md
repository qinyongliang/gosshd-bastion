# Web P2P Upload Implementation Plan

> Execute in this session; the user authorized implementation, commit, push and deployment. Builds and verification run in GitHub Actions, following the deployment reference chat.

**Goal:** Upload from the browser directly to an Agent when ICE connectivity is available, retaining relay fallback, upload policy enforcement and trusted completion audits.

**Architecture:** Reuse the existing WebRTC negotiator and reliable tunnel byte stream. A new upload-only Agent stream receives a server-selected destination and delegated, pinned SSH hops. Browser binary packets are relayed through an authenticated same-origin WebSocket until direct connectivity is established. The Agent checks each chunk, writes a temporary sibling file, validates the declared length and commits only after an explicit finish record. Completion and byte progress always return on the authenticated Agent control stream.

**Tech Stack:** Go, Pion WebRTC, existing tunnel.Conn, gorilla WebSocket, pkg/sftp, TypeScript/React, Playwright, GitHub Actions.

---

- [x] Add protocol upload request/status definitions, a bounded upload-only sink, and Agent dispatch. Verify corrupted chunks, short/oversized uploads, cancellation cleanup, replacement and SSH-backed uploads in Go tests.
- [x] Add the authenticated upload WebSocket route. Validate filename, length, existing SFTP permissions and target ownership before opening an Agent stream; return unsupported for old Agents/pure SSH so the browser uses the legacy endpoint. Audit only Agent-confirmed completion; reject client-supplied status packets. Verify authorization, origin, fallback and trusted results in server tests.
- [x] Add a browser tunnel sender with a 64-packet window, sequence ACKs, retransmission over relay, WebRTC negotiation and cancellation. Use CRC32 per record plus encrypted transport integrity; show Agent-confirmed progress and direct/relay status in both languages. Verify real Chromium-to-Agent direct upload, forced relay, interruption fallback, empty files and cancellation.
- [x] Extend release checks, document behavior, review diff, commit and push to main. Run the release workflow, fix failures, publish a fresh version, deploy both existing environments using checksum-verified release packages and backups, then verify health/frontend/Agent assets.

**Limits:** P2P needs a current Agent and reachable ICE candidates; the control WebSocket remains required. HTTP-only pages may fall back to relay when the browser restricts WebRTC. No new broad SFTP access or SSH credentials are exposed to the browser.

**Verification:** GitHub Actions run 38021793103 passed the frontend build, upload tests with the race detector, existing tunnel tests, pooled-writer regression, and all six real Chromium upload scenarios. A follow-up moves the upload toast into the existing React portal so notification overlays cannot block cancellation.

**Release:** `v0.1.155-bastion` was built from `b9ac146d`. The final Portal regression (38021916936), main upload checks (38022182935), Windows Client (38022182976), ARM64 build (38022182987), and Release (38022188155) succeeded. Both existing environments were deployed from checksum-verified packages. Public health checks, the protected upload route (anonymous requests return 401), the new frontend (`index-BXvFXib6.js`), database integrity and unchanged configuration/audit counts were verified. Both sites advertise the release Windows Agent checksum `603b1c61ddf1eb6e08059d8d96b7974abb9444a493468cf09c4ad23536294851`.

**Backups:** Program/database snapshots remain on both servers. Qyl release backups v146–v148 were transferred to `build/qyl-release-backups-v146-v148-20261010.tar.gz` and all 18 files were checked against remote SHA-256 values before removing only the transferred copies. Archive SHA-256: `43bd43dddadaceb2158dec13b8bafb32cf99d6c0b4bf9994a111961406869497`. Qyl had approximately 160 MiB free after deployment. External Agents require upgrading to v155 to use the new upload stream; legacy upload fallback remains available.
