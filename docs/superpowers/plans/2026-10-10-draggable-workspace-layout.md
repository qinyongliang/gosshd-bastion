# Draggable connection workspace implementation plan

> Execute in this session. Keep the existing file manager styling changes. Do not commit or publish unless requested.

**Goal:** Let users drag host information, file management, terminals and editors to any side of a view or the entire connection workspace, resize their splits and restore their layout from browser storage.

**Architecture:** Extend the existing pane tree with host and file views. Render its leaves in stable, keyed containers with computed bounds instead of mounting components under a changing split hierarchy. This preserves terminal runtimes, uploads and unsaved editor buffers while a view moves. Store validated, versioned layouts by browser user, organization, viewport mode and target; new connections inherit the last arrangement of tool panels. Save editor paths and geometry, not editor contents or credentials.

**Tech stack:** React, TypeScript, pointer events, ResizeObserver, localStorage; existing Playwright browser checks and Node tests.

- [x] Add `web/src/workspaceLayout.ts`: pane types, tree insertion/removal/movement, constrained layout bounds, storage validation/restoration. Add meaningful Node regression tests for full-width bottom docking, nesting, invalid storage and restoration.
- [x] Add `web/src/components/DockWorkspace.tsx`: flat stable view rendering, pointer-driven drag handles, placement preview, resize dividers, cancellation and minimum sizes. Keep hidden tabs mounted.
- [x] Update `web/src/pages/ConnectPage.tsx`: include tool panes in the tree; preserve tab split/close behavior, focus and terminal fullscreen; add drag handles to every view, browser persistence and a reset layout action. Editors initially open to the right.
- [x] Update `web/styles.css` and `web/src/i18n.tsx`: theme-aware handles and drop previews, responsive tool panels and translated instructions. Compact mobile appbar to a single row with home navigation, the current service name and service switching; hide branding text, alias and connection metadata.
- [x] Verify with `pnpm check`, `pnpm build`, Node layout tests and Playwright: move files to the full workspace bottom, editor to the right, host relocation, divider resizing, refresh restoration, tab switching, drag cancellation, fullscreen, narrow viewport, compact mobile header and unsaved buffer preservation.

## Release and deployment verification

- Released `v0.1.159-bastion` from `ffee33e145e7929d05f3f2ef24223544ab39d889`, including UI implementation `c9388ae2`. The unused v158 tag failed an old direct-child selector; the updated control-style test passes with the draggable panel wrapper.
- GitHub Actions release `38035793509`, P2P transfer checks `38035789411`, Windows Client `38035789539` and ARM64 build `38035789342` passed. Release validation includes Node layout tests and Chromium control styles, traffic statistics, file uploads/downloads and draggable workspace checks. Builds ran in Actions.
- Both `https://ssh.jsydf.cn` and `http://qyl.my.to:8880` run v159 from SHA256-verified archives, with healthy containers and zero restarts. Public assets `index-CFdFa8ts.js` and `ConnectPage-CwDO5Qtk.js` contain the transfer and persistent draggable workspace features. Anonymous transfer routes return 401.
- SQLite integrity and source-IP statistics checks passed. Original configuration fields were retained, excluding normal `updated_at` changes; the main site gained two targets during verification (30 to 32). All 26276 main-site and 4018 ARM-site audit rows were retained.
- Public Windows AMD64, Linux AMD64 and Linux ARM64 Agent checksums match v159 release assets on both sites. Main-site assets were fetched through the release proxy, checksum-verified and atomically cached.
- Server binary SHA256: AMD64 `75be15f461af650147fabcb168920cfa4ebc83681d8dde7d441ef52165829c85`; ARM64 `3aa3c909ab0811036f3edfbf712b933c767f18c526275950f528fbbc9a1e87c3`.
- After verification, stopped rollback containers were removed; program/database backups remain. ARM64 free space is approximately 412 MiB after caching v159 Agents. Server binaries are 28,590,242 bytes (AMD64) and 27,263,138 bytes (ARM64).
