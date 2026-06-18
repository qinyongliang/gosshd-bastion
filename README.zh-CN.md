# gosshd 堡垒机

[English](README.md) | 简体中文

`gosshd` 现在是一个基于 SQLite 的 SSH 堡垒机。用户通过 Web 控制台登录，管理组织、SSH 公钥、直连或 agent 接入的 SSH 服务，并使用标准 SSH 别名连接：

```sh
ssh test2@public-host
```

服务端会通过 SSH public key 找到用户，优先在用户的个人组织中解析 `test2`，再查找用户加入的共享组织，随后执行命令安全组策略并写入审计日志。

## 功能

- 使用 SQLite 持久化用户、会话、组织、用户组、目标服务、agent、安全组策略、LLM 配置、提示词资源和审计日志。
- 用户注册后自动拥有一个个人组织。所有 owner 绑定默认落到这个个人组织。
- 用户可以创建共享组织、通过邀请码邀请其他用户、加入多个组织，也可以退出共享组织。个人组织不能邀请其他用户。
- 用户可以配置自己的 SSH public key，并用普通 SSH 客户端连接目标别名。
- SSH 服务可以是直连目标，也可以是通过安装命令注册的 agent 目标。agent 注册后也是普通可重命名目标。
- 命令安全组支持黑名单、白名单、目标绑定、一个或多个用户组绑定、默认允许/拒绝，以及可选 LLM 实时审核。
- LLM provider 配置是组织级资源。提示词也是组织级资源；每个个人/共享组织创建时都会自动拥有一个只读默认提示词。
- `/mcp` 使用官方 Model Context Protocol Go SDK 暴露控制面工具。
- Web 管理控制台已嵌入服务端二进制。

## 运行

```sh
gosshd-server \
  --http-listen :80 \
  --ssh-listen :22 \
  --database-path gosshd.db \
  --host-key-path gosshd_host_key
```

打开 `http://public-host/`，注册用户，添加 SSH 公钥，然后添加别名为 `test2` 的目标服务。

如果需要 agent 接入，在控制台创建 agent enrollment，然后在私有主机执行生成的命令：

```sh
curl -fsSL http://public-host/install/<token>.sh | sh
```

Windows：

```powershell
irm http://public-host/install/<token>.ps1 | iex
```

如果要安装为开机启动服务，使用 install 模式。Linux 会通过 `systemctl` 注册 `gosshd-agent`：

```sh
curl -fsSL http://public-host/install/<token>.sh | sudo sh -s -- install
```

Windows 会通过 `sc.exe` 注册 `gosshd-agent`：

```powershell
$s='http://public-host/install/<token>.ps1'; irm $s -OutFile $env:TEMP\gosshd-agent-install.ps1; powershell -ExecutionPolicy Bypass -File $env:TEMP\gosshd-agent-install.ps1 -Install
```

## MCP

MCP 端点：

```text
http://public-host/mcp
```

它提供注册、组织管理、公钥、目标服务、agent enrollment、LLM 配置、提示词资源、命令安全组、策略绑定和审计查询工具。

## 验证

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
go test ./internal/server -run TestBastionE2E -v
go build ./cmd/gosshd-server
go build ./cmd/gosshd-agent
```
