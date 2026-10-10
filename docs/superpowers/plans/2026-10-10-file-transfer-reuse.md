# File transfer connection reuse

**Goal:** Consecutive files reuse WebSocket, WebRTC, Agent stream and delegated SFTP connections.

**Architecture:** Cache one sequential session per target and direction. Preserve tunnel sequence numbers across files. New operations travel only through the authenticated relay, are authorized individually by the server, and cannot change the pinned SSH route. Completion retains the session; cancellation, errors, disposal and idle expiry close it. Legacy Agents retain HTTP fallback.

**Verification:** GitHub Actions builds and runs race tests and real Chromium transfers. No local builds.

- [x] Add relay-only operation control and Agent session processing, reusing source/destination helpers and one SFTP client.
- [x] Add server session bridge with per-file authorization, route validation and audit.
- [x] Retain browser session state across operations and dispose on target change/unmount.
- [x] Verify multi-file upload/download content, connection/SDP counts, relay fallback and policy denial.
- [x] Push, pass CI, release and deploy both authorized environments, verify health/data/Agent downloads.

**Verification:** GitHub Actions `38033526049` passed the frontend build, transfer race tests, per-file policy/audit tests, relay-only operation validation, tunnel regressions and real Chromium upload/download suites. Three-file uploads and repeated downloads (including empty files) use one WebSocket and one SDP offer in direct/interrupted modes, or no offer in forced relay mode. Delegated SSH tests compare connection counts before/after the batch.

**Release:** `v0.1.157-bastion` was built from `342cbb7d`. Release `38033822810`, main transfer checks `38033819903`, Windows Client `38033819845`, and ARM64 build `38033819933` passed. Both environments run v157 from checksum-verified archives; public health, protected transfer routes and frontend `index-CkUEcpUq.js` are verified. Database quick checks pass; configuration counts remain 5 users/29 targets/1 tunnel and 1 user/2 targets/0 tunnels; all 26248 and 4016 existing audit rows were retained. Stopped rollback containers were removed after verification.

**ARM64 storage:** v149-v152 backups were archived locally with SHA256 `257e7e94f2dd1dd4163bda6b3374f3682548c73e364707425de99d7b7f021925`. With user authorization, obsolete Agent caches and v154/v155 release backups were removed. Current databases and the newest rollback backups remain. Free space rose from 89 MiB to 562 MiB; after deploying and caching v157 Agents it is approximately 488 MiB.
