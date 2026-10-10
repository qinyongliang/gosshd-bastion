# Folder drop uploads and desktop file selection

**Goal:** Drop multiple local folders/files recursively into the current remote directory, preserving folder names and empty directories; select remote entries with clicks, Shift ranges, Alt toggles and a drag rectangle.

**Architecture:** Reuse the sequential upload queue and cached P2P/relay session. Collect browser drop entries synchronously before awaiting recursive traversal, drain every directory-reader page, validate relative paths, then create directories and upload files. Keep upload destination pinned for the batch. Isolate drop traversal and selection logic from the existing file-manager presentation. Selection uses remote paths and follows the displayed sort order. A single click selects; double click opens. Right-click preserves a multi-selection, and download/delete/copy-path operate on selected entries.

**Implementation and verification:** Execute in this session; retain other in-progress workspace-layout edits. Builds and browser checks run in GitHub Actions.

- [ ] Add `web/src/fileDrop.ts` for recursive browser-entry traversal, plain-file fallback, empty directories, paginated reads, cancellation and safe relative paths; verify with `web/e2e/file_drop.test.mjs`.
- [ ] Add `web/src/fileSelection.ts` and `web/src/pages/useFileSelection.ts` for path-based range/toggle/rectangle selection, scroll-aware hit testing, cancellation and keyboard selection; verify pure selection cases and real browser mouse/modifier interactions.
- [ ] Update `web/src/pages/FileManager.tsx` to reuse one batch uploader for inputs and drops, create remote directory structure, preserve connection reuse and cancellation, and expose selected-item operations. Add localized drop/selection labels and scoped styles.
- [ ] Extend real browser upload tests with two folder roots, nested files, empty files/directories, a paginated reader, contents and one WebSocket/SDP across the batch; add selection regression tests for sorted ranges, Alt exclusion, rectangle selection and right-click preservation.
- [ ] Add tests to CI/release checks, run CI, and finish the authorized commit/push/deployment workflow.
