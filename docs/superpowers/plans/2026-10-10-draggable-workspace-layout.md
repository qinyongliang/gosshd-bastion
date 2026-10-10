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
