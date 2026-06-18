# gosshd Bastion

[English](README.md) | [简体中文](README.zh-CN.md)

`gosshd` is now a SQLite-backed SSH bastion. Users sign in to the web console, manage organizations, add SSH public keys, register direct or agent-backed SSH services, and connect with standard SSH aliases:

```sh
ssh test2@public-host
```

The server authenticates the SSH public key, resolves `test2` inside the user's personal organization first, then shared organizations, evaluates command policies, and writes audit logs.

## Features

- SQLite persistence for users, sessions, organizations, groups, targets, agents, policies, LLM configs, prompt resources, and audit logs.
- Every registered user gets a personal organization. Owner bindings default to that personal organization.
- Users can create shared organizations, invite others with codes, join multiple organizations, and leave shared organizations. Personal organizations cannot invite other users.
- Users can add SSH public keys and connect to target aliases with normal SSH clients.
- Targets can be direct SSH services or enrolled agents installed with `curl`/PowerShell commands. Agent targets are normal renameable SSH services.
- Command policies support blacklist rules, whitelist rules, user-group bindings, target bindings, default allow/deny, and optional LLM review.
- LLM provider configs are owner-level resources. Prompt resources are owner-level resources; a readonly default prompt is created for each personal/shared organization.
- `/mcp` exposes the control plane through the official Model Context Protocol Go SDK.
- The management console is embedded into the server binary.

## Run

```sh
gosshd-server \
  --http-listen :80 \
  --ssh-listen :22 \
  --database-path gosshd.db \
  --host-key-path gosshd_host_key
```

Open `http://public-host/`, register a user, add your SSH public key, then add a target with alias `test2`.

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
go build ./cmd/gosshd-server
go build ./cmd/gosshd-agent
```
