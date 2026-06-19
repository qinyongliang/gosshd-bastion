# gosshd Bastion

[English](README.md) | [简体中文](README.zh-CN.md)

`gosshd-bastion` is an SSH bastion built for AI services, automation agents, and operators that need audited access to private machines. It runs as a single Go server with an embedded web console, SQLite storage, an SSH gateway, agent enrollment, command safety policies, LLM review hooks, and an MCP control plane.

The current public release is [`v0.1.20-bastion`](https://github.com/qinyongliang/gosshd-bastion/releases/tag/v0.1.20-bastion). The latest release page is [here](https://github.com/qinyongliang/gosshd-bastion/releases/latest).

## What Works Now

- SQLite-backed users, sessions, organizations, organization members, user groups, SSH public keys, SSH services, target tags, agent enrollments, command policies, LLM configs, prompt resources, and audit logs.
- A first-run `admin` account. System admins keep normal user menus and also get system settings, account management, and organization repair tools.
- Personal and shared organizations. Every user gets a personal organization. Shared organizations support `owner`, `admin`, and `member` roles.
- Organization user groups. Every organization has a default all-members group, and command policies can bind to one or more user groups.
- SSH public-key login to the bastion. The SSH username is the target alias, for example `test2`.
- SSH services with a display name, alias, host, port, remote username, auth type, and multiple tags. Tags are stored as first-class records and can be used for filtering and policy binding.
- Direct SSH services using password or private-key authentication.
- Private-node SSH services. The SSH services page can generate tokenized Linux/macOS and Windows install commands, including startup-service install commands. Once a private node registers, it becomes a normal SSH service and can be renamed, retagged, filtered, and bound to policies.
- Advanced SSH server routing can use an existing SSH service as a jump host/proxy for private subnets.
- Command safety groups with whitelist and blacklist rules, exact/prefix/contains matching, target binding, target-tag binding, user-group binding, default allow/deny, and optional LLM review when no rule matches.
- Command audit logs for SSH `exec` requests, including command, target, policy decision, reason, and exit code.
- DingTalk OAuth login. New DingTalk users can be auto-created and placed into a default organization and role.
- LDAP settings in the admin console. LDAP login is not active in this release.
- Embedded web UI with Simplified Chinese as the default locale, English switching, persisted language preference, white default theme, dark theme switching, resource tables, tag filters, modals, and detail drawers.
- `/mcp` exposes the control plane through the official Model Context Protocol Go SDK.

## Current Boundaries

- The SSH gateway currently supports command execution requests such as `ssh test2@bastion.example.com hostname`. Interactive shell and SFTP are not exposed in this slice.
- Target passwords/private keys and LLM API keys are stored in the SQLite database as provided by the API in this release. Restrict database access and use host/disk protection until credential encryption is completed.
- GitHub Pages source lives under `site/` and the workflow is present, but Pages publishing depends on the repository/account Pages availability.
- This release publishes cross-platform server packages and standalone agent binaries. It does not publish a `full` package.

## Release Assets

Each release publishes:

- `gosshd-<version>-linux-amd64.tar.gz`, `gosshd-<version>-darwin-arm64.tar.gz`, and other server packages.
- `gosshd-<version>-windows-amd64.zip` and other Windows server packages.
- `gosshd-agent-<version>-<goos>-<goarch>` standalone agent binaries.
- `checksums.txt`.

Example Linux install from GitHub Releases:

```sh
version=v0.1.20-bastion
platform=linux-amd64

curl -fL -o "gosshd-${version}-${platform}.tar.gz" \
  "https://github.com/qinyongliang/gosshd-bastion/releases/download/${version}/gosshd-${version}-${platform}.tar.gz"

tar -xzf "gosshd-${version}-${platform}.tar.gz"
cd "gosshd-${platform}"
mkdir -p data agent-cache

./gosshd-server \
  --http-listen :18080 \
  --ssh-listen :22022 \
  --database-path ./data/gosshd.db \
  --host-key-path ./data/gosshd_host_key \
  --agent-cache-path ./agent-cache \
  --public-host bastion.example.com:18080 \
  --bootstrap-admin-password 'change-me'
```

Open `http://bastion.example.com:18080/` and sign in as:

```text
email: admin
password: change-me
```

The bootstrap admin password is resolved in this order:

1. `--bootstrap-admin-password`
2. `GOSSHD_BOOTSTRAP_ADMIN_PASSWORD`
3. a random password printed once in the server log when the first admin account is created

## First Use

1. Sign in as `admin`.
2. Add your SSH public key under **Public keys**.
3. Create or select an organization.
4. Add an SSH service with a display name, alias such as `test2`, authentication method, and tags such as `test-env`.
5. Run a command through the bastion:

```sh
ssh -p 22022 test2@bastion.example.com hostname
```

The bastion authenticates your public key, resolves alias `test2` first in your personal organization and then in shared organizations. If a shared alias appears in more than one organization, the request is rejected as ambiguous.

## Private Node Enrollment

Open **SSH services**, choose **Add service**, then select the **Private node** tab. Enter the service alias and create an install token. The response contains tokenized commands for the selected owner scope.

Linux/macOS run once:

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sh
```

Linux startup service with `systemctl`:

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sudo sh -s -- install
```

Windows run once:

```powershell
irm http://bastion.example.com:18080/install/<token>.ps1 | iex
```

Windows startup service with `sc.exe`:

```powershell
$s='http://bastion.example.com:18080/install/<token>.ps1'; irm $s -OutFile $env:TEMP\gosshd-agent-install.ps1; powershell -ExecutionPolicy Bypass -File $env:TEMP\gosshd-agent-install.ps1 -Install
```

The server serves private-node binaries from local `--agent-path` when present, otherwise it downloads the matching release asset into `--agent-cache-path` and serves it to the private host.

## Command Policies

Command safety groups are evaluated per target:

- Policies can bind directly to SSH services.
- Policies can bind to target tags, so editing a service's tags immediately changes which tag-bound policies apply.
- Policies can bind to organization user groups. If no user group is bound, the policy applies to all users who can reach the target.
- Blacklist rules deny immediately.
- Whitelist rules allow matching commands.
- If no rule matches and an LLM config is attached, the command is sent to the configured model with the selected prompt. The model must return JSON: `{"allow": true|false, "reason": "short reason"}`.
- If no rule and no LLM applies, the policy's default action is used.

## System Administration

System admins can manage:

- DingTalk login settings: enabled flag, client id/secret, auth/token/userinfo URLs, redirect URL, default organization, and default role.
- LDAP connection settings: server URL, bind DN/password, base DN, user filter, email attribute, and display-name attribute. LDAP login is reserved for a later release.
- Accounts: promote or demote system administrators.
- Organizations: inspect members, update roles, and transfer ownership for repair.

Organization owners can transfer ownership to an existing member. The previous owner becomes `admin`. Personal organization ownership cannot be transferred.

## MCP

The MCP endpoint is:

```text
http://bastion.example.com:18080/mcp
```

It exposes tools for registration, organizations, public keys, targets, target tags, agent enrollments, LLM configs, prompt resources, command policies, policy bindings, and audit logs.

## Development

Normal Go checks:

```sh
go test ./...
go build ./cmd/gosshd-server ./cmd/gosshd-agent
```

Browser E2E on Windows PowerShell:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
$env:GOSSHD_UI_E2E_NODE='C:\path\to\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\path\to\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\path\to\chrome.exe'
go test ./internal/server -run TestUIE2EWithBrowser -v
```

Browser E2E on Linux/macOS:

```sh
GOSSHD_UI_E2E_NODE=/path/to/node \
GOSSHD_UI_E2E_PLAYWRIGHT=/absolute/path/to/playwright \
GOSSHD_UI_E2E_BROWSER=/absolute/path/to/chrome \
go test ./internal/server -run TestUIE2EWithBrowser -v
```

`TestUIE2EWithBrowser` intentionally fails when the browser variables are missing. It drives the embedded UI with Playwright and a real browser.
