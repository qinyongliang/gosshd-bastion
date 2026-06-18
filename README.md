# gosshd Bastion

[English](README.md) | [简体中文](README.zh-CN.md)

`gosshd` is now a SQLite-backed SSH bastion. Users sign in to the web console, manage organizations, add SSH public keys, register direct or agent-backed SSH services, and connect with standard SSH aliases:

```sh
ssh test2@public-host
```

The server authenticates the SSH public key, resolves `test2` inside the user's personal organization first, then shared organizations, evaluates command policies, and writes audit logs.

## Features

- SQLite persistence for users, sessions, organizations, groups, targets, agents, policies, LLM configs, prompt resources, and audit logs.
- A default system administrator account is bootstrapped on first run. Admins keep normal user menus and also get global settings, account management, and organization management.
- Every registered user gets a personal organization. The UI selects an active organization, and API/MCP writes require explicit owner scope.
- Users can create shared organizations, invite others with codes, join multiple organizations, and leave shared organizations. Personal organizations cannot invite other users.
- Shared organizations have `owner`, `admin`, and `member` roles. Owners can transfer ownership; admins can manage members, user groups, targets, and policies.
- Users can add SSH public keys and connect to target aliases with normal SSH clients.
- Targets can be direct SSH services or enrolled agents installed with `curl`/PowerShell commands. Agent targets are normal renameable SSH services.
- Command policies support blacklist rules, whitelist rules, user-group bindings, target bindings, default allow/deny, and optional LLM review.
- LLM provider configs are owner-level resources. Prompt resources are owner-level resources; a readonly default prompt is created for each personal/shared organization.
- DingTalk OAuth login can be enabled from the system admin console. New DingTalk users are created automatically and can be assigned to a default organization and role.
- LDAP connection settings can be stored from the system admin console; LDAP login itself is reserved for a later slice.
- `/mcp` exposes the control plane through the official Model Context Protocol Go SDK.
- The management console is embedded into the server binary.

## Run

```sh
gosshd-server \
  --http-listen :80 \
  --ssh-listen :22 \
  --database-path gosshd.db \
  --host-key-path gosshd_host_key \
  --bootstrap-admin-password 'change-me'
```

Open `http://public-host/`, sign in as `admin`, add your SSH public key, then add a target with alias `test2`.

The bootstrap admin password is resolved in this order:

1. `--bootstrap-admin-password`
2. `GOSSHD_BOOTSTRAP_ADMIN_PASSWORD`
3. A generated password printed once in the server log when the account is first created

## System Administration

System admins can open the system administration view to manage:

- DingTalk settings: enabled flag, client id/secret, auth/token/userinfo URLs, redirect URL, default organization, and default role.
- LDAP settings: enabled flag, server URL, bind DN/password, base DN, user filter, email attribute, and name attribute. These settings are stored for configuration, but LDAP login is not active in this version.
- Accounts: promote or demote system administrators.
- Organizations: list organizations, inspect members, update roles, and transfer ownership for repair.

Organization owners can transfer ownership to an existing member. The previous owner becomes `admin`. Personal organization ownership cannot be transferred.

For an agent-backed target, create an agent enrollment in the console and run the generated command on the private host:

```sh
curl -fsSL http://public-host/install/<token>.sh | sh
```

Windows:

```powershell
irm http://public-host/install/<token>.ps1 | iex
```

To install the agent as a startup service, use install mode. Linux registers `gosshd-agent` with `systemctl`:

```sh
curl -fsSL http://public-host/install/<token>.sh | sudo sh -s -- install
```

Windows registers `gosshd-agent` with `sc.exe`:

```powershell
$s='http://public-host/install/<token>.ps1'; irm $s -OutFile $env:TEMP\gosshd-agent-install.ps1; powershell -ExecutionPolicy Bypass -File $env:TEMP\gosshd-agent-install.ps1 -Install
```

## MCP

The MCP endpoint is:

```text
http://public-host/mcp
```

It exposes tools for registration, organization management, public keys, targets, agent enrollments, LLM configs, prompt resources, command policies, policy bindings, and audit logs.

## Verify

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
go test ./internal/server -run TestBastionE2E -v
go test ./internal/server -run TestDingTalkAdminOrganizationE2E -v
GOSSHD_UI_E2E_NODE=/path/to/node \
GOSSHD_UI_E2E_PLAYWRIGHT=/absolute/path/to/playwright \
GOSSHD_UI_E2E_BROWSER=/absolute/path/to/chrome \
go test ./internal/server -run TestUIE2EWithBrowser -v
go build ./cmd/gosshd-server
go build ./cmd/gosshd-agent
```

`TestUIE2EWithBrowser` intentionally fails when the three browser variables are missing. It drives the real embedded UI with Playwright and a local browser; it is not skipped or replaced by static assertions.
