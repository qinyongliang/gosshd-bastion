# Mobile Console UX Design

## Scope

Improve the existing `/connect` workspace below 760px without changing desktop behavior or restructuring the terminal, host information, and file manager components.

## Layout

- Keep host information above the terminal and the file manager below it.
- Render both collapsed states as consistent, full-width 34px title bars with a label and chevron.
- Keep expanded content in normal document flow so it never covers the terminal.
- Move terminal pane actions into a reserved toolbar row on mobile instead of floating over terminal output.
- Position the server switcher menu against the viewport with 8px side margins and a viewport-bounded height.

## Terminal Input

- Reduce mobile terminal horizontal character spacing while preserving the current font size and line height.
- Add a mobile-only two-row key bar above the software keyboard.
- Show the key bar only while xterm's hidden input textarea has focus; loading, tab activation, fullscreen changes, and taps outside the terminal must not focus it on mobile.
- Row 1: Esc, Tab, Ctrl, Alt, Home, End.
- Row 2: Page Up, Left, Up, Down, Right, Page Down.
- Esc, Tab, navigation, Home, End, Page Up, and Page Down send their terminal escape sequences immediately.
- Ctrl and Alt are one-shot modifiers: tapping one arms it for the next printable terminal key, then clears it. Tapping the active modifier again cancels it.
- Hide the key bar on desktop.

## Behavior And Failure Handling

- Key presses use the existing terminal WebSocket input path. When the session is not connected, controls remain visible but disabled.
- Existing reconnect, split, fullscreen, AI collaboration, host information, and file manager behavior remain unchanged.
- Mobile layout changes are CSS-scoped to the current 760px breakpoint.
- When the software keyboard changes the visual viewport, refit the terminal to the visible height and keep the latest output line in view.

## Verification

- TypeScript check and production build must pass.
- Extend `web/e2e/ui_e2e.mjs` at a 390px viewport to assert the key bar follows the real terminal input focus, the latest output remains visible after a visual viewport resize, the terminal toolbar is static, collapsed bars are consistent, and the server switcher is viewport-bounded. Keep the key-sequence mapping in one directly testable function.
- Visually verify 315px and 390px widths: no horizontal overflow, no terminal toolbar overlap, consistent collapsed bars, and an in-viewport server switcher.
