# Batch File Copy and Move Implementation Plan

**Goal:** Copy or move a file-manager multi-selection into one chosen remote directory, retaining each name and nested directory contents.

**Architecture:** Extend the existing move/copy routes with `sources` plus a destination directory; keep single `source` plus an exact destination path. Validate the whole batch before changing files, reuse one SFTP client, and reuse existing recursive copy/move and per-item audit functions. Reuse the directory browser modal for both single and multiple selections, disable resubmission while pending, and refresh after success or partial failure.

**Verification and delivery:** Execute in this session. All builds and browser checks run in GitHub Actions; retain the authorized main commit/push/two-host deployment workflow.

- [ ] Extend `internal/server/api_connect.go`: accept batch sources, preserve names/spaces, reject same-source/descendant and duplicate destinations, require an existing destination directory for batches, reuse one client and audit each attempted item.
- [ ] Update `web/src/api.ts`, `web/src/pages/FileManager.tsx`, `web/src/components/ui.tsx` and translations: enable both actions for multi-selection, show selected paths and a directory destination for batches, preserve single-item renaming, prevent duplicate submissions and refresh after failures.
- [ ] Add API integration coverage for Agent/delegated SSH, nested content, one connection, policies, invalid batches, partial failures and legacy single operations; extend real browser selection coverage for multi-copy/move and single renaming.
- [ ] Run transfer CI and release validation, commit/push main, deploy checksum-verified release archives to both existing hosts, verify data/frontend/Agent checksums and remove stopped rollback containers.
