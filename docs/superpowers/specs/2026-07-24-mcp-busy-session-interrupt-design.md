# MCP Busy Session Visibility and Interrupt Design

## Goal

Keep terminals with a foreground task visible to MCP so an agent can inspect or interrupt them, while preventing those terminals from receiving another command automatically.

## Behavior

- `session_list` returns every non-closed terminal session that has AI collaboration enabled, including sessions whose shell is busy.
- Each listed session includes `shell_busy`, allowing MCP clients to distinguish an idle shell from a running foreground task.
- `session_send_command` continues to reject a busy session.
- Automatic SSH/AI terminal selection continues to exclude busy sessions.
- `session_interrupt` sends Ctrl+C (`0x03`) to the selected session, including when `shell_busy` is true.
- MCP instructions tell clients to use `session_screen` or `session_interrupt` for a busy session and not send it a new command.

## Implementation

Expose the existing `terminalSession.shellBusy` value through `terminalSessionInfo` and `mcpSessionInfo`. Remove only the busy-session filter from `listForUser`. Reuse the existing `terminalSession.interrupt` implementation; do not add another stop tool or alter routing readiness checks.

## Verification

- A manager test verifies busy and idle sessions are both listed with the correct `ShellBusy` value.
- An interrupt test verifies a busy session receives exactly Ctrl+C.
- An MCP mapping test verifies `shell_busy` is exposed.
- Existing routing tests continue to prove busy sessions are not selected for command reuse.
- Run focused and full `internal/server` tests, then `git diff --check`.
