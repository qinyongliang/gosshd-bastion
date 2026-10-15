# Agent installation and reinstallation

The generated installers support foreground runs and managed background installation. Use a newly downloaded installer after updating the server; a previously saved script retains its old behavior.

## Reinstallation sequence

1. Download to a unique staging file and verify SHA256 before stopping the existing Agent.
2. Prepare and validate startup configuration before replacing the existing installation.
3. Stop the existing managed service, if applicable, then stop executable-path-matched Agent processes and wait for them to exit. A stop failure aborts replacement; unrelated same-named processes are not selected.
4. Replace the program and startup configuration, preserving `agent.json`.
5. Start the new Agent and check the service startup result.

Linux uses systemd; macOS uses the `com.gosshd.agent` system LaunchDaemon; Windows uses the `gosshd-agent` service. Android uses Termux services or the root controller. Unix and Android binaries are replaced by renaming a verified staging file rather than writing over a running executable. Foreground re-runs stop only the matching runtime installation, not a separate system service; Android foreground and background modes share one installation directory and therefore stop its existing supervisor first.

Stopping or replacing an Agent interrupts its active sessions. Run the installer from a local terminal or an independent SSH/RDP connection, not from a terminal hosted by the Agent being stopped: the service manager may terminate that installer along with the old Agent.

A failed checksum or stop does not replace the installed binary. A later installation or startup error is reported; automatic rollback is not provided. Fix the reported error and rerun the installer.
