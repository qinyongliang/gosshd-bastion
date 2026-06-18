# gosshd 堡垒机

[English](README.md) | 简体中文

`gosshd` 现在是一个基于 SQLite 的 SSH 堡垒机。用户通过 Web 控制台登录，管理组织、SSH 公钥、直连或 agent 接入的 SSH 服务，并使用标准 SSH 别名连接：

```sh
ssh test2@public-host
```

服务端会通过 SSH public key 找到用户，优先在用户的个人组织中解析 `test2`，再查找用户加入的共享组织，随后执行命令安全组策略并写入审计日志。

## 功能

- 使用 SQLite 持久化用户、会话、组织、用户组、目标服务、agent、安全组策略、LLM 配置、提示词资源和审计日志。
- 首次启动会自动创建默认系统管理员账号。管理员拥有普通用户菜单，也拥有全局配置、账号管理和组织管理入口。
- 用户注册后自动拥有一个个人组织。所有 owner 绑定默认落到这个个人组织。
- 用户可以创建共享组织、通过邀请码邀请其他用户、加入多个组织，也可以退出共享组织。个人组织不能邀请其他用户。
- 共享组织包含 `owner`、`admin`、`member` 三种角色。owner 可以转移所有权；admin 可以管理成员、用户组、目标服务和策略。
- 用户可以配置自己的 SSH public key，并用普通 SSH 客户端连接目标别名。
- SSH 服务可以是直连目标，也可以是通过安装命令注册的 agent 目标。agent 注册后也是普通可重命名目标。
- 命令安全组支持黑名单、白名单、目标绑定、一个或多个用户组绑定、默认允许/拒绝，以及可选 LLM 实时审核。
- LLM provider 配置是组织级资源。提示词也是组织级资源；每个个人/共享组织创建时都会自动拥有一个只读默认提示词。
- 可以在系统管理员控制台启用钉钉 OAuth 登录。钉钉用户首次登录会自动创建本地账号，并可自动加入默认组织和角色。
- 可以在系统管理员控制台保存 LDAP 连接配置；当前版本只提供配置入口，LDAP 登录留到后续版本。
- `/mcp` 使用官方 Model Context Protocol Go SDK 暴露控制面工具。
- Web 管理控制台已嵌入服务端二进制。

## 运行

```sh
gosshd-server \
  --http-listen :80 \
  --ssh-listen :22 \
  --database-path gosshd.db \
  --host-key-path gosshd_host_key \
  --bootstrap-admin-password 'change-me'
```

打开 `http://public-host/`，使用 `admin` 登录，添加 SSH 公钥，然后添加别名为 `test2` 的目标服务。

默认管理员密码按以下优先级确定：

1. `--bootstrap-admin-password`
2. `GOSSHD_BOOTSTRAP_ADMIN_PASSWORD`
3. 如果前两者都为空，首次创建账号时生成随机密码并在服务端日志打印一次

## 系统管理

系统管理员可以进入系统管理视图：

- 钉钉配置：启用开关、client id/secret、auth/token/userinfo URL、redirect URL、默认组织和默认角色。
- LDAP 配置：启用开关、server URL、bind DN/password、base DN、user filter、email 属性和姓名属性。当前版本保存这些配置，但尚未启用 LDAP 登录。
- 账号管理：提升或取消系统管理员权限。
- 组织管理：查看组织、查看成员、调整角色，以及在需要修复时转移组织所有者。

组织 owner 可以把所有权转移给已有成员，原 owner 会变为 `admin`。个人组织不能转移 owner。

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
go test ./internal/server -run TestDingTalkAdminOrganizationE2E -v
go build ./cmd/gosshd-server
go build ./cmd/gosshd-agent
```
