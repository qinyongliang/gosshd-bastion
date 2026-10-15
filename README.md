# GOSSHD Bastion

English | [简体中文](README.zh-CN.md)

## One way in. Clear boundaries throughout.

**GOSSHD Bastion is a self-hosted remote operations workspace for people and AI.** Bring SSH terminals, remote files, private nodes and TCP/UDP port tunnels into one entry point. Teams work in the browser; AI tools use standard SSH or MCP for inspection and assisted troubleshooting. You manage target credentials, access policies and audit records.

[Website](https://qinyongliang.github.io/gosshd-bastion/) · [Download Server](https://github.com/qinyongliang/gosshd-bastion/releases/latest) · [Product brief and announcement copy](site/brief.en.md)

## Why use it

- **Keep work together:** Use terminals, edit files, upload folders and copy or move selections in one workspace.
- **Work with people and agents:** Browser access for everyday tasks, standard SSH for existing workflows, MCP for AI tools.
- **Define explicit boundaries:** Organizations, user groups, targets, tags, source IPs and capability controls govern access instead of distributing target credentials broadly.
- **Keep a trail:** Command decisions and exit codes are audited; recorded interactive sessions can be replayed; transfers and tunnels retain their respective audits or statistics.
- **Adopt gradually:** Start with a test machine, verify policies, then expand to private networks, customer sites and team automation.

## Workspace capabilities

| Workflow | Capabilities |
| --- | --- |
| Troubleshooting and configuration | Web terminal, file editor, draggable workspace layout, desktop and mobile access |
| File management | Recursive folder upload, multi-selection and batch copy / move / download / delete |
| File transfer | Try encrypted browser-to-Agent P2P; relay when unavailable; per-file authorization and auditing |
| Private service access | Persistent TCP/UDP port tunnels, expiry, connections, traffic and source IP statistics |
| Team and AI access | Organizations and user groups, command rules, optional LLM review, temporary SSH grants, MCP tool groups |
| Investigation | Command decisions, exit codes, file audits and replay of enabled interactive recordings |

> Command policies are not a complete shell sandbox. P2P is not guaranteed on every network, and temporary grant identifiers are sensitive credentials. Validate policies, restrict capabilities and protect data before production use.

## SSH Access Model

GOSSHD uses the SSH username field as the target alias:

```sh
ssh -p 22022 prod-api@bastion.example.com "uptime"
```

The caller still needs an SSH identity. Your public key identifies the operator; the gateway manages the real target’s login credentials. The target alias resolves to a personal or organization SSH service. Allowed commands are forwarded to the real host; denied commands return a standard SSH failure such as `exit status 126` and are written to audit.

For temporary access, open **SSH services -> the service's menu -> Temporary authorizations**. Create a UUID with a configurable lifetime (24 hours by default) and copy the generated `ssh -p PORT UUID@HOST` command. Holding that UUID permits access to its service without a client key, using the creator's command policies and audit identity. Renewal retains the UUID and extends its expiry; expiry or deletion closes its connections. Members manage their own grants, while organization administrators and system administrators can manage all grants for services they can access.

## Core Capabilities

- **Shared access control plane:** teams and agents use web, SSH or MCP to reach managed servers.
- **SQLite control plane:** users, orgs, sessions, groups, keys, targets, tags, policies, prompts, and LLM configs are persisted locally.
- **Separate audit database:** command audit data is isolated from the main control database.
- **Private nodes:** Linux/macOS and Windows install commands include enrollment tokens; startup mode uses systemd on Linux and `sc.exe` on Windows.
- **Command safety groups:** blacklist, whitelist, LLM fallback, IP allowlists, target/tag binding, user-group binding, interactive terminal, port forwarding, upload, and download controls.
- **Web file transfers:** browser uploads and downloads automatically try an encrypted WebRTC connection to an updated Agent, including SFTP targets reached through that Agent. Consecutive files reuse one WebSocket/WebRTC/Agent session per target and direction, including the delegated SFTP connection; each file is authorized and audited separately. Idle sessions expire after two minutes; cancellation or errors close the session. Progress shows Direct/Relay; failed direct paths continue through the authenticated control connection without duplicate file bytes. Old Agents and SSH targets without an Agent retain HTTP transfers. Both directions enforce SFTP permissions and retain audits with Agent-computed SHA256. Uploads validate chunks and length, then replace a temporary sibling file; cancellation preserves the original destination. SFTP overwrites require atomic rename. The upload limit remains 1 GiB. Downloads stream to a local file when the browser save API is available; other browsers buffer up to 256 MiB and use native HTTP downloads for larger files. Cancelled streaming downloads abort the writable file. Agents automatically update to the server release when reconnecting, except development builds or failed updates. Configure `-tunnel-stun-servers` for public-network ICE discovery; direct connectivity depends on network/firewall reachability.
- **LLM review:** OpenAI-compatible chat completions with fail-closed behavior; allowed responses can omit a reason.
- **Terminal replay:** interactive shell sessions can be recorded as compressed timestamped files and replayed from the console.
- **Admin console:** system admins keep normal user menus and get system settings, account management, organization repair, DingTalk settings, and LDAP connection settings.
- **MCP endpoint:** `/mcp` exposes control-plane operations to AI tools.

## Quick Start

Download the latest server package from [GitHub Releases](https://github.com/qinyongliang/gosshd-bastion/releases/latest), then run:

```sh
./gosshd-server \
  --http-listen :18080 \
  --ssh-listen :22022 \
  --database-path ./data/gosshd.db \
  --secret-key-path ./data/gosshd.secret-key \
  --audit-database-path ./data/gosshd-audit.db \
  --host-key-path ./data/gosshd_host_key \
  --agent-cache-path ./agent-cache \
  --public-host bastion.example.com:18080 \
  --bootstrap-admin-password 'change-me'
```

Open `http://bastion.example.com:18080/` and sign in:

```text
email: admin
password: change-me
```

Then:

1. Add your SSH public key.
2. Create or select an organization.
3. Add a direct SSH server or create a private-node enrollment.
4. Add command safety groups and optional LLM review.
5. Bind user groups to batches of SSH targets when access needs to be scoped.
6. Connect through the SSH control plane:

The server encrypts SSH credentials and LLM API keys before writing them to SQLite. Keep the secret key outside backups and protect it with file permissions. Use HTTPS directly through a TLS reverse proxy before exposing the console or enrollment links.

```sh
ssh -p 22022 aws-ap-sg-billing-db@bastion.example.com "hostname"
```

### Reset a User Password

Use the one-shot startup flag to generate a 16-character random password for a user identified by email or user ID. The process exits after updating the account and prints the new password to standard output:

```sh
./gosshd-server --database-path ./data/gosshd.db --secret-key-path ./data/gosshd.secret-key --reset-user-password admin
```

## Private Node Install

Create a private-node enrollment from **SSH services -> Add service -> Private node**. The console gives tokenized commands.

Run once:

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sh
```

```powershell
irm http://bastion.example.com:18080/install/<token>.ps1 | iex
```

Install as a startup service:

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sudo sh -s -- install
```

```powershell
$s='http://bastion.example.com:18080/install/<token>.ps1'
irm $s -OutFile $env:TEMP\gosshd-agent-install.ps1
powershell -ExecutionPolicy Bypass -File $env:TEMP\gosshd-agent-install.ps1 -Install
```

Once registered, a private node is just another SSH service: rename it, tag it, bind policies to it, and audit it like a manually added target.


### Android Agent (ARMv7 / ARM64)

Supports native Android executables for **Android 7.0 / API 24 and newer**:

| Android ABI | Download platform | Release asset |
| --- | --- | --- |
| `armeabi-v7a` (32-bit ARMv7) | `android/armv7` | `gosshd-agent-<version>-android-armv7` |
| `arm64-v8a` (64-bit ARMv8) | `android/arm64` | `gosshd-agent-<version>-android-arm64` |

These are Android/Bionic binaries, not the Linux ARM binaries or an APK. The installer chooses the Termux **userspace** architecture (including 32-bit Termux on a 64-bit kernel), or the native Android ABI for a root shell. Android `arm` update requests resolve to the published ARMv7 asset.

#### Build

Install Android NDK r27c (`27.2.12479018`) or newer and set `ANDROID_NDK_HOME` (`ANDROID_NDK_ROOT` also works). Both targets use NDK/CGO, API 24, PIE and 16 KiB ELF page alignment. ARMv7 cannot be built with `CGO_ENABLED=0` using the current Go toolchain. The pinned `anet` dependency requires `-checklinkname=0`; the build scripts apply it only to Android.

```powershell
.\scripts\build-agent-android.ps1 -Version dev -NDKPath 'C:\Android\Sdk\ndk\27.2.12479018'
```

```sh
ANDROID_NDK_HOME="$HOME/Android/Sdk/ndk/27.2.12479018" bash scripts/build-agent-android.sh dev
```

Outputs are `build/agent/<version>/android/{armv7,arm64}/gosshd-agent` and the versioned assets in `dist/`. Full release scripts also build both Android agents and include them in `checksums.txt`.

For local testing without publishing a release, start a server with the same version and `--agent-path ./build/agent`; binaries are version-scoped to avoid serving stale builds.

#### Unrooted Termux

Use the Android commands from the private-node enrollment drawer. Install prerequisites first:

```sh
pkg install curl coreutils
# Background service only; reopen the Termux shell after installing:
pkg install termux-services
```

Run in the foreground, or add `install` for a runit-managed background service (**do not use sudo**):

```sh
curl -fsSL https://bastion.example.com/install/<token>.sh -o "$TMPDIR/gosshd-agent-install.sh"
sh "$TMPDIR/gosshd-agent-install.sh"          # foreground
sh "$TMPDIR/gosshd-agent-install.sh" install  # background
```

The executable, launcher, stable identity and log live in `$HOME/.gosshd/`. Control the service with `sv up "$PREFIX/var/service/gosshd-agent"`, `sv down ...` or `sv status ...`. Reboot startup additionally requires **Termux:Boot** from the same installation source, opened once; the installer writes `$HOME/.termux/boot/gosshd-agent`. Exclude Termux from battery optimization. Android may still terminate background processes; no script can guarantee survival on every vendor ROM.

#### Native rooted Android

Enter a root shell (`su`) and run the Android enrollment command there. `curl` or `wget`, a SHA256 checker, and standard toybox/BusyBox commands must be available; background installation also needs `nohup` and `readlink`. The same `.sh` installer detects this environment automatically:

```sh
# Run these inside the native root shell, not an unrooted adb shell.
curl -fsSL https://bastion.example.com/install/<token>.sh -o /data/local/tmp/gosshd-agent-install.sh
sh /data/local/tmp/gosshd-agent-install.sh          # foreground
sh /data/local/tmp/gosshd-agent-install.sh install  # background
/data/local/gosshd/control.sh status
/data/local/gosshd/control.sh stop
/data/local/gosshd/control.sh start
```

Files are stored privately in `/data/local/gosshd/`, including `agent.json` and `agent.log`. Reinstallation preserves identity, verifies SHA256 before atomically replacing the executable, and restarts only the process whose PID **and executable path** match this agent. If `/data/adb/service.d` already exists, an executable `gosshd-agent.sh` boot hook is installed for compatible root managers (Magisk / KernelSU / APatch). Otherwise the agent starts now, but reboot startup must be added manually using `/data/local/gosshd/control.sh start`. The installer does not modify system partitions or disable SELinux; device policies may impose additional restrictions.

## Command Review Model

Policy evaluation is intentionally predictable:

1. Source IP and capability gates are checked.
2. Blacklist rules deny matching commands.
3. Whitelist rules allow matching commands.
4. If no rule matches and an LLM is configured, the command is sent to the model.
5. If there is no valid decision, the request fails closed or uses the configured default action.

LLM responses use JSON:

```json
{"allow": true}
```

```json
{"allow": false, "reason": "Command modifies production data without an approved maintenance window."}
```

## Browser File Transfers

Drop multiple folders or files into the remote file list to upload recursively, preserving nested paths and empty directories. Click to select, double-click to open, use Shift for ranges and Alt to toggle individual entries, or drag a selection rectangle (Alt toggles the rectangle's entries). Ctrl/Cmd+A selects all; Escape clears selection. Right-click for batch download, deletion, path copying, or copying/moving selected files and folders into a chosen directory. Single-item copy/move still allows renaming via the destination path.

Uploads and downloads try a browser-to-Agent P2P connection and fall back to server relay when needed. Batches reuse connections while authorizing and auditing each file separately.

## Documentation And Website

[Website](https://qinyongliang.github.io/gosshd-bastion/) · [Product brief](site/brief.en.md) · [Tunnel guide](docs/tunnels.md).

GitHub Pages sources live in [`site/`](site/). The bilingual pages include illustrative workspace and policy demos plus downloadable briefs, not live product sessions.

## Development

```sh
go test ./...
go build ./cmd/gosshd-server ./cmd/gosshd-agent
```

Browser E2E requires explicit Node, Playwright, and Chrome paths:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
$env:GOSSHD_UI_E2E_NODE='C:\path\to\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\path\to\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\path\to\chrome.exe'
go test ./internal/server -run TestUIE2EWithBrowser -v
```

## Release Shape

Releases publish cross-platform server archives, standalone private-node binaries, and checksums. This version does not publish a `full` package.

Release packaging runs only for `v*` tags or manual Release dispatches and includes full file-transfer regression checks. Standalone ARM64 builds and dedicated P2P checks are manual only; ordinary pushes trigger neither packaging nor dedicated checks. GitHub Actions no longer packages the Windows desktop client; Windows server and Agent binaries remain in releases.

## Optional desktop MCP

An independent, opt-in desktop module supports Windows/macOS screenshots, bounded accessibility trees, semantic UI operations and keyboard/mouse input. Android reuses the same MCP tools with a root/adb shell backend for the primary screen. It can run as a standalone MCP host or through authorized Agent targets. Desktop permissions are separate from SSH command policies. Windows/macOS require a logged-in user context and native OS permissions; Android requires an unlocked root/adb shell session. Android shell input supports printable ASCII and advertised gesture equivalents; non-root Termux and semantic set_value are unsupported. See [Android desktop MCP](docs/android-desktop-mcp.md) for setup and limits. See [desktop MCP](docs/desktop-mcp.md) for setup, tool contracts, safety boundaries and native tests.

Android also offers a [paired no-root accessibility companion](docs/android-accessibility.md) with ADB/Shizuku activation, native Unicode editing and opt-in foreground lifecycle. The install wrapper routes by actual su authorization while preserving the root/ADB shell backend.
