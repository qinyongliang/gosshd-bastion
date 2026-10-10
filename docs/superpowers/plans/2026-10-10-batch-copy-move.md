# Batch File Copy and Move Implementation Plan

**Goal:** Copy or move a file-manager multi-selection into one chosen remote directory, retaining each name and nested directory contents.

**Architecture:** Extend the existing move/copy routes with `sources` plus a destination directory; keep single `source` plus an exact destination path. Validate the whole batch before changing files, reuse one SFTP client, and reuse existing recursive copy/move and per-item audit functions. Reuse the directory browser modal for both single and multiple selections, disable resubmission while pending, and refresh after success or partial failure.

**Verification and delivery:** Execute in this session. All builds and browser checks run in GitHub Actions; retain the authorized main commit/push/two-host deployment workflow.

- [x] Extend `internal/server/api_connect.go`: accept batch sources, preserve names/spaces, reject same-source/descendant and duplicate destinations, require an existing destination directory for batches, reuse one client and audit each attempted item.
- [x] Update `web/src/api.ts`, `web/src/pages/FileManager.tsx`, `web/src/components/ui.tsx` and translations: enable both actions for multi-selection, show selected paths and a directory destination for batches, preserve single-item renaming, prevent duplicate submissions and refresh after failures.
- [x] Add API integration coverage for Agent/delegated SSH, nested content, one connection, policies, invalid batches, partial failures and legacy single operations; extend real browser selection coverage for multi-copy/move and single renaming.
- [x] Run transfer CI and release validation, commit/push main, deploy checksum-verified release archives to both existing hosts, verify data/frontend/Agent checksums and remove stopped rollback containers.

**Follow-up validation fixes:** Additive policy fixtures are detached between denial scenarios. The upload/download browser tests wait for the initial canonical directory before editing a path and locate the actual input. A transport regression reproduces local-channel startup before remote readiness; browser file sessions now probe and await a direct ACK before starting data, preserving relay fallback and batch reuse. All nine Node tests pass.

**CI notification adjustment:** At the user's request, dedicated P2P checks now run only by manual dispatch, with overlapping runs cancelled by concurrency. Release validation retains race checks, tunnel/store tests and real browser checks; ordinary main pushes trigger neither dedicated checks nor packaging.
**Release and deployment:** Release `38041917274` succeeded for `v0.1.166-bastion`, source `ef6e41fb`, including all race tests, source-IP statistics/migrations, handshake tests and real Chromium suites. Both existing hosts run checksum-verified archives. Database integrity and original configuration rows were preserved; command audits remained 26350/4087. Public health, protected transfer routes and batch-copy/move frontend markers passed; deployed assets are `index-B-HkKoc2.js` and `ConnectPage-DK0cV2G_.js`. Program/database backups were retained.