# File Breadcrumb Scroll Design

## Problem

The file manager closes its breadcrumb directory menu on every captured `scroll` event. This includes scrolling the menu's own directory list and scrolling the surrounding page.

## Design

Keep the menu rendered through its existing portal. Store the breadcrumb button that opened it as the menu anchor. While the menu is open, captured `scroll` and window `resize` events recalculate the anchor rectangle and update the menu position instead of closing it.

The menu continues to close when the user clicks outside it, presses Escape, selects a drive or directory, or switches targets.

No behavior changes are made to the file context menu or file listing.

## Verification

- Open a breadcrumb directory menu and scroll its directory list; the menu remains open.
- Scroll the surrounding page; the menu remains aligned with its breadcrumb button.
- Resize the window; the menu remains aligned.
- Click outside the menu or press Escape; the menu closes.
- Run the existing frontend type check.
