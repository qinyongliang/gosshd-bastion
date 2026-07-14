# Mobile Lists And Flat Background Design

## Scope

Improve every application list below 760px while preserving desktop information density and existing behavior. Remove decorative square and grid textures from page and terminal backgrounds in both light and dark themes.

## Shared Table Lists

- Add each existing table header as a `data-label` on its matching `SimpleTable` cell.
- On mobile, render each `SimpleTable` row as a full-width card and each cell as a label/value pair.
- Keep actions tappable at the bottom of the card, allow long identifiers to wrap, and avoid horizontal page overflow.
- This shared behavior covers organizations, members, groups, credentials, keys, MCP tokens, policies, audit logs, and system-administration lists without page-specific markup rewrites.
- Keep the file manager as a real scrollable table because aligned name, size, permission, and time columns are useful for file browsing.

## Existing Card And Tree Lists

- Keep target trees, member cards, resource rows, policy rows, server switching, telemetry lists, and transfer lists on their existing components.
- At the mobile breakpoint, reduce excessive padding, use one content column, wrap metadata, and keep row actions visible and finger-sized.
- Do not remove fields or change list ordering, filtering, selection, folder collapsing, paging, or navigation.

## Backgrounds

- Remove decorative repeating grids and square-line textures from application workspaces, connect workspaces, terminal surfaces, client desktop content, empty states, and command surfaces in both themes.
- Retain flat theme colors and restrained non-repeating gradients so light/dark contrast remains intact.
- Keep functional chart guide lines because they communicate scale rather than decorate a page background.

## Accessibility And Failure Handling

- Preserve semantic tables on desktop and screen-reader-visible header labels on mobile.
- Preserve focus styles, button sizes, scrolling, and empty states.
- Long values must wrap or scroll inside their own region instead of widening the page.
- No new dependency, route, API, or data transformation is introduced.

## Verification

- Add browser E2E assertions at 390px and 315px for card rows, label/value cells, no horizontal overflow, server-list bounds, and file-table scrolling.
- Assert representative page and terminal surfaces no longer use repeating/grid background textures.
- Run TypeScript checks and the production build, and verify desktop tables remain semantic table layouts.
