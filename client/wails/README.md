# GOSSHD Wails Client

This is the desktop shell for the local single-user GOSSHD client.

The Wails process starts the existing Go server in `--client-mode` inside the same process and opens the local web UI in an embedded client shell. The shell keeps a small menu bar for resources, audit, and settings. The hosted web UI still talks to the local HTTP backend directly, so terminal WebSocket sessions and file operations keep using the existing server code.

## Runtime Data

Local data is stored under the user config directory:

```text
GOSSHD/Client
```

The directory contains the main SQLite database, audit database, host key, known hosts file, and terminal recordings.

## Build

From the repository root:

```powershell
.\build-client-windows.ps1 -Version dev -Runtime win-x64
```

If `wails` is not on `PATH`, the script installs `github.com/wailsapp/wails/v2/cmd/wails@v2.12.0` with `go install`.

## Client Mode UI Contract

The shell opens the server in `--client-mode`. In that mode the web UI must not expose user, organization, member, invite, user-group, login/logout, or system-admin controls. The visible client navigation is resource services, command safety groups, and audit. Connection pages are opened as separate windows.
