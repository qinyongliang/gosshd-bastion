# GOSSHD Bastion

[English](README.md) | [简体中文](README.zh-CN.md)

**GOSSHD Bastion is an AI-native SSH access control plane:** a command-level SSH safety gateway for AI agents, MCP tools, and automation jobs. It lets agents enter servers for inspection, troubleshooting, and automation while your team keeps control of permissions, risky commands, and replayable evidence.

## Why It Exists

AI tools can inspect machines, read logs, diagnose services, and chain commands faster than a human can watch a terminal. Giving those tools a raw SSH key is too much trust; putting every command behind a human approval queue is too slow.

GOSSHD Bastion solves a simple product problem: AI needs SSH to get real work done, but production access needs command-level boundaries, evidence, and replay.

- **For operations:** agents can inspect and troubleshoot without owning server keys directly.
- **For security:** risky commands can be blocked, reviewed, or judged by a model before execution.
- **For leaders:** every remote action can be searched, explained, and replayed when evidence matters.
- **For delivery teams:** private machines, customer-side boxes, and GPU nodes can join one governed entry point.

## SSH Access Model

GOSSHD uses the SSH username field as the target alias:

```sh
ssh provider-region-service@gosshd.site "command"
```

Your public key identifies the operator. The target alias resolves to a personal or organization SSH service. Allowed commands are forwarded to the real host; denied commands return a standard SSH failure such as `exit status 126` and are written to audit.

For temporary access, open **SSH services -> the service's menu -> Temporary authorizations**. Create a UUID with a configurable lifetime (24 hours by default) and copy the generated `ssh -p PORT UUID@HOST` command. Holding that UUID permits access to its service without a client key, using the creator's command policies and audit identity. Renewal retains the UUID and extends its expiry; expiry or deletion closes its connections. Members manage their own grants, while organization administrators and system administrators can manage all grants for services they can access.

## Core Capabilities

- **AI-native SSH control plane:** users and agents reach private servers through one governed entry point.
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

The GitHub Pages source lives in [`site/`](site/). It includes the bilingual promotional homepage and animated terminal/replay demos used on the public website.

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
