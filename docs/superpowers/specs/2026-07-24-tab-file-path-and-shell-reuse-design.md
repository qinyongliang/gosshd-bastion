# Tab File Path and Shell Reuse Design

## Goal

Keep each web-console tab's file-manager path independent, and never route AI or SSH commands into a terminal while tmux or another foreground program owns its shell.

## Design

- Add `filePath` to `ConnectionTab`. `ConnectWorkspace` passes the active tab's value into `FileManager` and stores resolved path changes back on that tab.
- Keep only the path across tab switches. Selection, menus, modals, and draft input remain transient.
- Treat `terminalSession.shellBusy` as unavailable in automatic terminal lookup and MCP session listing. Existing command readiness checks remain the final guard for explicit session IDs.
- Keep the outer Bash OSC 633 hooks. Mark `PROMPT_COMMAND` non-exported after installing the hook so tmux child shells cannot inherit `__gosshd_precmd` without its function definition.
- Apply the prompt environment fix to direct SSH and Unix agent Bash integration.

## Verification

- Test independent tab path updates.
- Test that busy sessions are excluded from lookup and listing while becoming available after the prompt completion event.
- Test that both Bash integration scripts remove the export attribute from `PROMPT_COMMAND`.
- Run Go server tests, frontend type checking, and production builds before release.

