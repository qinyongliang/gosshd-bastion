# File transfer connection reuse

**Goal:** Consecutive files reuse WebSocket, WebRTC, Agent stream and delegated SFTP connections.

**Architecture:** Cache one sequential session per target and direction. Preserve tunnel sequence numbers across files. New operations travel only through the authenticated relay, are authorized individually by the server, and cannot change the pinned SSH route. Completion retains the session; cancellation, errors, disposal and idle expiry close it. Legacy Agents retain HTTP fallback.

**Verification:** GitHub Actions builds and runs race tests and real Chromium transfers. No local builds.

- [x] Add relay-only operation control and Agent session processing, reusing source/destination helpers and one SFTP client.
- [x] Add server session bridge with per-file authorization, route validation and audit.
- [x] Retain browser session state across operations and dispose on target change/unmount.
- [x] Verify multi-file upload/download content, connection/SDP counts, relay fallback and policy denial.
- [ ] Push, pass CI, release and deploy both authorized environments, verify health/data/Agent downloads.

**Verification:** GitHub Actions `38033526049` passed the frontend build, transfer race tests, per-file policy/audit tests, relay-only operation validation, tunnel regressions and real Chromium upload/download suites. Three-file uploads and repeated downloads (including empty files) use one WebSocket and one SDP offer in direct/interrupted modes, or no offer in forced relay mode. Delegated SSH tests compare connection counts before/after the batch.
