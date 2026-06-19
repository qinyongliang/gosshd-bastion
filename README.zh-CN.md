# gosshd 堡垒机

[English](README.md) | 简体中文

`gosshd-bastion` 是一个面向 AI 服务、自动化 Agent 和运维人员的 SSH 堡垒机。它以单个 Go 服务运行，内置 Web 控制台、SQLite 存储、SSH 网关、Agent 注册、命令安全组、LLM 实时审核钩子和 MCP 控制面。

当前公开版本是 [`v0.1.16-bastion`](https://github.com/qinyongliang/gosshd-bastion/releases/tag/v0.1.16-bastion)。最新版本入口在 [GitHub Releases](https://github.com/qinyongliang/gosshd-bastion/releases/latest)。

## 当前已实现

- 基于 SQLite 持久化用户、会话、组织、组织成员、用户组、SSH 公钥、SSH 服务、目标标签、Agent 注册、命令安全组、LLM 配置、提示词资源和审计日志。
- 首次启动自动创建 `admin` 账号。系统管理员拥有普通用户菜单，也拥有系统配置、账号管理和组织修复入口。
- 支持个人组织和共享组织。每个用户都有个人组织；共享组织支持 `owner`、`admin`、`member` 三种角色。
- 支持组织用户组。每个组织默认有一个全员用户组，命令安全组可以绑定一个或多个用户组。
- 用户配置自己的 SSH public key 后，可以通过堡垒机 SSH 登录。SSH 用户名就是目标别名，例如 `test2`。
- SSH 服务支持显示名称、别名、主机、端口、远程用户名、认证方式和多个标签。标签是独立表，可用于筛选和安全组绑定。
- 支持直连 SSH 服务，认证方式包括账号密码和私钥。
- 支持 Agent SSH 服务。Agent 注册会生成带 token 的 Linux/macOS 与 Windows 命令，并包含开机启动安装命令。Agent 上线后会成为普通 SSH 服务，可以重命名、改标签、筛选和绑定安全组。
- 命令安全组支持黑名单、白名单、精确/前缀/包含匹配、目标绑定、目标标签绑定、用户组绑定、默认允许/拒绝，以及未命中规则时接入 LLM 审核。
- 对 SSH `exec` 命令请求写入审计日志，包括命令、目标、策略决策、原因和退出码。
- 支持钉钉 OAuth 登录。首次通过钉钉登录的用户可以自动创建本地账号，并加入默认组织和角色。
- 系统管理中提供 LDAP 连接配置入口。当前版本尚未启用 LDAP 登录。
- 内嵌 Web UI 默认使用简体中文，支持英文切换并记住选择；默认白色主题，支持黑白主题切换；复杂资源页使用资源表、标签筛选、弹窗和详情抽屉。
- `/mcp` 使用官方 Model Context Protocol Go SDK 暴露控制面工具。

## 当前边界

- SSH 网关当前支持命令执行请求，例如 `ssh test2@bastion.example.com hostname`。交互式 shell 和 SFTP 在当前版本还没有开放。
- 目标服务密码/私钥和 LLM API Key 在当前版本会按 API 提交内容存入 SQLite。请限制数据库文件访问，并使用主机或磁盘级保护；凭据加密需要后续版本补上。
- 官网和文档源码位于 `site/`，仓库里已经包含 Pages workflow；实际发布依赖当前 GitHub 账号/仓库是否可用 GitHub Pages。
- 当前版本发布跨平台 server 压缩包和独立 agent 二进制，不发布 `full` 包。

## 发布产物

每个版本会发布：

- `gosshd-<version>-linux-amd64.tar.gz`、`gosshd-<version>-darwin-arm64.tar.gz` 等 server 包。
- `gosshd-<version>-windows-amd64.zip` 等 Windows server 包。
- `gosshd-agent-<version>-<goos>-<goarch>` 独立 Agent 二进制。
- `checksums.txt`。

Linux 从 GitHub Releases 安装示例：

```sh
version=v0.1.16-bastion
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

打开 `http://bastion.example.com:18080/`，使用以下账号登录：

```text
email: admin
password: change-me
```

默认管理员密码按以下顺序确定：

1. `--bootstrap-admin-password`
2. `GOSSHD_BOOTSTRAP_ADMIN_PASSWORD`
3. 如果前两者都为空，首次创建 `admin` 时生成随机密码，并且只在服务端日志打印一次

## 首次使用

1. 使用 `admin` 登录。
2. 在 **SSH 公钥** 页面添加自己的 public key。
3. 创建或选择一个组织。
4. 添加 SSH 服务，填写显示名称、别名，例如 `test2`，选择认证方式，并添加标签，例如 `测试环境`。
5. 通过堡垒机执行命令：

```sh
ssh -p 22022 test2@bastion.example.com hostname
```

堡垒机会先通过 SSH public key 找到用户，再优先在用户个人组织中解析别名 `test2`，然后查找用户加入的共享组织。如果多个共享组织里存在同名别名，请求会因为歧义被拒绝。

## Agent 注册

在 **Agent SSH** 页面创建 Agent enrollment。返回结果会给出当前 owner 范围下带 token 的安装命令。

Linux/macOS 一次运行：

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sh
```

Linux 使用 `systemctl` 安装为开机启动服务：

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sudo sh -s -- install
```

Windows 一次运行：

```powershell
irm http://bastion.example.com:18080/install/<token>.ps1 | iex
```

Windows 使用 `sc.exe` 安装为开机启动服务：

```powershell
$s='http://bastion.example.com:18080/install/<token>.ps1'; irm $s -OutFile $env:TEMP\gosshd-agent-install.ps1; powershell -ExecutionPolicy Bypass -File $env:TEMP\gosshd-agent-install.ps1 -Install
```

服务端会优先从本地 `--agent-path` 提供 Agent 二进制；如果本地没有，则从 GitHub Release 下载匹配平台的 Agent 到 `--agent-cache-path`，再提供给私有主机。

## 命令安全组

命令安全组按目标服务进行评估：

- 安全组可以直接绑定 SSH 服务。
- 安全组可以绑定目标标签，因此编辑 SSH 服务标签后，标签绑定的安全组关系会即时变化。
- 安全组可以绑定组织用户组。如果没有绑定用户组，则对所有能访问该目标的用户生效。
- 黑名单命令会立即拒绝。
- 白名单命令会允许执行。
- 如果没有命中黑白名单，并且安全组配置了 LLM，则命令会交给对应模型和提示词审核。模型需要返回 JSON：`{"allow": true|false, "reason": "short reason"}`。
- 如果没有命中规则，也没有 LLM，则使用安全组默认动作。

## 系统管理

系统管理员可以管理：

- 钉钉登录配置：启用开关、client id/secret、auth/token/userinfo URL、redirect URL、默认组织和默认角色。
- LDAP 连接配置：server URL、bind DN/password、base DN、用户过滤器、邮箱属性和显示名属性。LDAP 登录留到后续版本。
- 账号管理：提升或取消系统管理员权限。
- 组织管理：查看成员、调整角色，以及在需要修复时转移组织 owner。

组织 owner 可以把所有权转移给已有成员，原 owner 会变为 `admin`。个人组织不能转移 owner。

## MCP

MCP 端点：

```text
http://bastion.example.com:18080/mcp
```

它提供注册、组织管理、公钥、目标服务、目标标签、Agent enrollment、LLM 配置、提示词资源、命令安全组、策略绑定和审计查询工具。

## 开发验证

常规 Go 验证：

```sh
go test ./...
go build ./cmd/gosshd-server ./cmd/gosshd-agent
```

Windows PowerShell 浏览器 E2E：

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
$env:GOSSHD_UI_E2E_NODE='C:\path\to\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\path\to\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\path\to\chrome.exe'
go test ./internal/server -run TestUIE2EWithBrowser -v
```

Linux/macOS 浏览器 E2E：

```sh
GOSSHD_UI_E2E_NODE=/path/to/node \
GOSSHD_UI_E2E_PLAYWRIGHT=/absolute/path/to/playwright \
GOSSHD_UI_E2E_BROWSER=/absolute/path/to/chrome \
go test ./internal/server -run TestUIE2EWithBrowser -v
```

`TestUIE2EWithBrowser` 缺少浏览器变量时会直接失败。它会用 Playwright 和真实浏览器驱动内嵌界面。
