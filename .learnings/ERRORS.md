# Errors

Command failures and integration errors.

---

## [ERR-20260807-007] docker_args_index

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: infra

### Summary
A fixed Docker args index was out of range during the final version check.

### Error

```text
error calling index: index out of range
```

### Context
- Public and host-local health checks still returned `ok`.
- Earlier full container inspection already showed the correct version argument.

### Suggested Fix
Match the serialized args instead of assuming a fixed positional index.

### Metadata
- Reproducible: yes
- Related Files: none

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Rechecked the complete args for `v0.1.111-bastion`.

---

## [ERR-20260807-006] remote_log_tail

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: infra

### Summary
A PowerShell pipeline command was accidentally used inside the remote Bash shell.

### Error

```text
bash: Select-Object: command not found
```

### Context
- Container state inspection succeeded before the log-tail pipeline failed.
- The deployed container remained running with zero restarts.

### Suggested Fix
Use `tail -n` for remote Linux log truncation.

### Metadata
- Reproducible: yes
- Related Files: none

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Re-ran the remote log check with `tail -n 20`.

---

## [ERR-20260807-005] release_binary_version_probe

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: infra

### Summary
The server's `--version` flag sets a value and cannot be used as a print-version probe.

### Error

```text
flag needs an argument: -version
```

### Context
- Ran the checksum-verified release binary with `--version` after extraction.
- The active production binary and container were not changed.

### Suggested Fix
Verify the release tag, Actions linker flag, and extracted binary checksum instead.

### Metadata
- Reproducible: yes
- Related Files: cmd/gosshd-server/main.go, .github/workflows/release.yml

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Used the release tag and binary checksum as version evidence.

---

## [ERR-20260807-004] git_fetch_https

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: medium
**Status**: resolved
**Area**: infra

### Summary
Fetching this GitHub repository over HTTPS timed out.

### Error

```text
command timed out after 34069 milliseconds
```

### Context
- Ran a read-only `git fetch origin main` before publishing.
- No remote refs were changed.

### Suggested Fix
Use the repository's working SSH transport without changing the configured origin URL.

### Metadata
- Reproducible: yes
- Related Files: .git/config

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Switched the remote check and push to GitHub SSH transport.

---

## [ERR-20260807-003] shell_command

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: tests

### Summary
A combined process-stop, build, and launch PowerShell command was blocked by terminal policy.

### Error

```text
CreateProcess rejected: blocked by policy
```

### Context
- Combined three independently auditable operations in one command.
- The rejected command made no changes.

### Suggested Fix
Run process stop, frontend build, backend build, and launch as separate short commands.

### Metadata
- Reproducible: unknown
- Related Files: web/src, cmd/gosshd-server/main.go

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Split the operations; all later commands succeeded.

---

## [ERR-20260807-002] start_process

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: tests

### Summary
The local verification backend could not bind because port 18080 was already in use.

### Error

```text
command timed out after 34064 milliseconds
```

### Context
- Attempted to launch the local backend and Vite server on the repository defaults.
- Vite remained on 5173 while an unrelated local service answered on 18080; repository data was untouched.

### Suggested Fix
Check both ports, then use a freshly built embedded frontend on an unused backend port.

### Metadata
- Reproducible: unknown
- Related Files: cmd/gosshd-server/main.go, vite.config.ts

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Switched to a freshly built temporary executable serving the embedded frontend on port 18081.

---

## [ERR-20260807-001] shell_command

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: config

### Summary
PowerShell did not expand Unix-style glob paths passed to `rg`.

### Error

```text
rg: internal/server/*_test.go: The filename, directory name, or volume label syntax is incorrect. (os error 123)
```

### Context
- Attempted to search Go tests by placing `*_test.go` directly in the path.
- The command was read-only and changed no files.

### Suggested Fix
Use `rg ... internal/server internal/store -g '*_test.go'` on Windows.

### Metadata
- Reproducible: yes
- Related Files: internal/server, internal/store

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Re-ran the search with `-g` filters.

---

## [ERR-20260804-002] go_test

**Logged**: 2026-08-04T00:00:00+08:00
**Priority**: low
**Status**: pending
**Area**: tests

### Summary
The full Go suite has a locale-dependent Windows ping assertion.

### Error

```text
TestWindowsShellUsesConPTYForInteractiveInput timed out waiting for "Reply from 127.0.0.1" while ping emitted the Chinese equivalent.
```

### Context
- `go test ./... -count=1` on a Chinese Windows environment.
- The store and server packages passed; this failure is unrelated to target deletion.

### Suggested Fix
Assert a locale-independent ping signal or inject a deterministic interactive command.

### Metadata
- Reproducible: yes
- Related Files: internal/agent/command_windows_test.go

---

## [ERR-20260804-001] exec_command

**Logged**: 2026-08-04T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: tests

### Summary
Complex inline PowerShell and SQLite benchmark command was rejected by the terminal policy.

### Error

```text
CreateProcess rejected: blocked by policy
```

### Context
- Attempted a disposable SQLite deletion benchmark with a PowerShell here-string.
- No repository or application data was changed.

### Suggested Fix
Use a focused Go test or a shorter command without nested shell quoting.

### Metadata
- Reproducible: unknown
- Related Files: internal/store/store_test.go

### Resolution
- **Resolved**: 2026-08-04T00:00:00+08:00
- **Notes**: Switched to the repository's Go test path.

---

## [ERR-20260926-UI-001] browser-ui-verification

**Logged**: 2026-09-26
**Priority**: low
**Status**: pending
**Area**: tests

### Summary
UI automation verification could not start because the environment reported no available browser session and a browser inventory fetch failure.

### Error
`Browsers: Error: nodeRepl.fetch request failed`

### Context
Attempted to inspect the local Vite frontend through the computer-use browser surface after starting the dev server. Static type checking and production builds remained available.

### Suggested Fix
Run the UI E2E suite in an environment with a configured browser and the required Playwright variables.

### Metadata
- Reproducible: unknown
- Related Files: web/e2e/ui_e2e.mjs

---

## [ERR-20260926-PNPM-001] add-radix-dropdown-menu

**Logged**: 2026-09-26
**Priority**: low
**Status**: pending
**Area**: frontend

### Summary
Adding `@radix-ui/react-dropdown-menu` was blocked by an existing pnpm virtual-store location mismatch.

### Error
`ERR_PNPM_UNEXPECTED_VIRTUAL_STORE`

### Context
The workspace node_modules links use the repository virtual store while pnpm attempted to use `E:\.pnpm-store\v11\links`.

### Suggested Fix
Reinstall dependencies with the repository's configured pnpm store before adding new packages, or keep the shared menu primitive dependency-free.

---

## [ERR-20261009-STYLE-QA] Existing verification failures

**Logged**: 2026-10-09T17:57:00+08:00
**Priority**: medium
**Status**: pending
**Area**: frontend

### Summary
The workspace has nine existing TS7006 diagnostics; the existing mobile console browser test cannot click the collapsed file sidebar after expanding the host sidebar.

### Details
TypeScript diagnostics were reproduced with HEAD versions of modified source files. The mobile test failed at web/e2e/ui_e2e.mjs:84 with both updated CSS and HEAD CSS injected through a Playwright stylesheet route. Neither failure is introduced by the control-style changes. New control-style, tunnel-traffic, and new-terminal-connection browser tests pass.

Reconfirmed on 2026-10-10 while verifying search folder visibility: `pnpm check` reports the same nine TS7006 diagnostics in SystemAdminPage.tsx and TunnelsPage.tsx. TargetsPage.tsx has no diagnostics; focused folder-filter checks pass.

### Suggested Action
Address baseline implicit-any errors and the mobile sidebar expansion flow in a separate scoped change. For UI QA, wait for the terminal and sidebar to mount before deciding whether the file sidebar needs expansion; Ant Design virtualized options should be clicked through the visible dropdown rather than hidden accessibility nodes.

---
