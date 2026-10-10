# GOSSHD Bastion

[English](README.md) | 简体中文

**GOSSHD Bastion 是面向 AI Agent / MCP / 自动化任务的 SSH 控制平面：** 为 AI 服务和自动化任务提供命令级 SSH 安全网关，让智能体可以进入服务器完成巡检、排障和自动化任务，同时让团队保留权限边界、风险拦截和审计回放。

## 为什么需要它

AI 工具能比人更快地读取日志、检查容器、排查服务、串联命令。直接给它一把 SSH 私钥太危险；把每条命令都交给人工审批又太慢。

GOSSHD Bastion 解决的是一个很直接的问题：AI 需要 SSH 去完成真实工作，但生产访问必须有命令级边界、证据和回放。

- **对运维：** AI 可以帮忙巡检和排障，但不直接持有服务器钥匙。
- **对安全：** 高风险命令先被拦截、审核或交给模型判断。
- **对管理者：** 每次远程操作都能搜索、解释和回放，出了问题有证据。
- **对交付团队：** 内网机器、客户现场机器、GPU 节点都能快速接入统一入口。

## SSH 访问模型

GOSSHD 使用 SSH 的用户名字段承载目标服务别名：

```sh
ssh 服务商-地区-业务名称@gosshd.site "要执行的命令"
```

公钥用于识别操作者。目标别名会解析到个人或组织下的 SSH 服务。允许的命令会转发到真实主机；被拒绝的命令会返回标准 SSH 失败结果，例如 `exit status 126`，并写入审计。

短期访问可在 **SSH 服务 -> 对应服务菜单 -> 临时授权** 中创建 UUID，有效期默认 24 小时，可自行设置。复制生成的 `ssh -p 端口 UUID@主机` 命令即可连接对应服务，无需客户端密钥，命令策略和审计身份沿用授权创建者。续期保留原 UUID 并延长到期时间；到期或删除会断开相应连接。普通成员管理自己创建的授权，组织管理员和系统管理员可管理其有权访问的服务下的全部授权。

## 核心能力

- **AI-native SSH 控制平面：** 用户和智能体通过统一入口访问私有服务器。
- **SQLite 控制平面：** 用户、组织、会话、用户组、公钥、目标、标签、策略、提示词和 LLM 配置本地持久化。
- **独立审计数据库：** 命令审计数据和主控制数据库分离。
- **私有节点：** Linux/macOS 和 Windows 安装命令自带注册令牌；开机启动模式在 Linux 使用 systemd，在 Windows 使用 `sc.exe`。
- **命令安全组：** 支持黑名单、白名单、LLM 兜底、IP 白名单、目标/标签绑定、用户组绑定、交互式终端、端口转发、上传和下载控制。
- **网页文件传输：** 上传和下载都会自动尝试与新版 Agent 建立加密 WebRTC 直连，也支持通过 Agent 访问的 SFTP 目标。连续文件按目标和传输方向复用 WebSocket、WebRTC、Agent 会话及代理 SFTP 连接，每份文件仍单独鉴权与审计；空闲两分钟、取消或失败时释放会话。界面显示进度、速度及“直连/中转”，支持取消；直连中断后沿用同一传输中转继续，避免重复写入。旧版 Agent、没有 Agent 的 SSH 目标保留 HTTP 传输。两种方向均遵守 SFTP 权限并保留包含 Agent 计算的 SHA256 的审计。上传会校验分块和文件长度，完整写入临时文件后才替换目标，取消时保留原文件；覆盖 SFTP 已有文件需要服务器支持原子重命名，上传上限仍为 1 GiB。下载在浏览器支持时流式写入本地文件；其他浏览器缓存最多 256 MiB，更大文件沿用浏览器原生 HTTP 下载，避免占满内存。取消流式下载会中止写入。正式版 Agent 会在重连时按服务器版本自动更新，开发版或更新失败时除外。公网打洞沿用 `-tunnel-stun-servers` 配置，能否直连取决于网络与防火墙。
- **LLM 审核：** 使用 OpenAI 兼容 chat completions，异常默认拒绝；通过时可以省略原因。
- **终端回放：** 交互式 Shell 可录制为带时间戳的压缩文件，并在控制台回放。
- **管理员入口：** 系统管理员既有普通用户菜单，也有系统设置、账号管理、组织修复、钉钉和 LDAP 配置入口。
- **MCP 端点：** `/mcp` 向 AI 工具暴露控制面能力。

## 快速开始

从 [GitHub Releases](https://github.com/qinyongliang/gosshd-bastion/releases/latest) 下载最新 server 包，然后运行：

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

打开 `http://bastion.example.com:18080/`，登录：

```text
email: admin
password: change-me
```

然后：

1. 添加自己的 SSH 公钥。
2. 创建或选择一个组织。
3. 添加直连 SSH 服务器，或创建私有节点注册令牌。
4. 配置命令安全组和可选 LLM 审核。
5. 根据访问范围，把用户组绑定到不同批次的 SSH 目标。
6. 通过 SSH 控制平面连接：

服务器会在写入 SQLite 前加密 SSH 凭据和 LLM API Key。请把密钥文件排除在备份之外并保护文件权限；对外提供控制台或注册链接前，请先通过 TLS 反向代理启用 HTTPS。

```sh
ssh -p 22022 aws-ap-sg-billing-db@bastion.example.com "hostname"
```

### 重置用户密码

可使用一次性启动参数为指定用户（邮箱或用户 ID）生成 16 位随机密码。命令完成后退出服务进程，并在标准输出中回显新密码：

```sh
./gosshd-server --database-path ./data/gosshd.db --secret-key-path ./data/gosshd.secret-key --reset-user-password admin
```

## 私有节点安装

在 **SSH 服务 -> 添加服务 -> 私有节点** 创建注册令牌。控制台会给出带 token 的命令。

一次运行：

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sh
```

```powershell
irm http://bastion.example.com:18080/install/<token>.ps1 | iex
```

安装为开机启动服务：

```sh
curl -fsSL http://bastion.example.com:18080/install/<token>.sh | sudo sh -s -- install
```

```powershell
$s='http://bastion.example.com:18080/install/<token>.ps1'
irm $s -OutFile $env:TEMP\gosshd-agent-install.ps1
powershell -ExecutionPolicy Bypass -File $env:TEMP\gosshd-agent-install.ps1 -Install
```

私有节点注册成功后就是普通 SSH 服务：可以重命名、改标签、绑定策略，也会像手动添加的目标一样进入审计。

## 命令审核模型

策略判断保持可解释：

1. 先检查来源 IP 和能力开关。
2. 黑名单规则命中则拒绝。
3. 白名单规则命中则允许。
4. 未命中规则且配置了 LLM 时，把命令发送给模型。
5. 没有有效决策时，按默认拒绝或配置的默认动作处理。

LLM 响应使用 JSON：

```json
{"allow": true}
```

```json
{"allow": false, "reason": "Command modifies production data without an approved maintenance window."}
```

## 网页文件传输

将多个文件夹或文件拖入远程文件列表即可递归上传，保留目录结构和空目录。文件列表支持单击选中、双击打开、Shift 连选、Alt 切换单项、拖动框选、Alt 框选切换、Ctrl/Cmd+A 全选和 Escape 清空；右键可对选中的多个文件下载、删除或复制路径。

上传和下载都会尝试浏览器与 Agent 之间的 P2P 直连，不可直连时自动使用服务器中转。批量传输复用连接，每个文件仍独立检查权限并记录审计。

## 官网和文档

GitHub Pages 源码位于 [`site/`](site/)。里面包含中英文宣传首页，以及官网里的动态终端和回放演示。

## 开发

```sh
go test ./...
go build ./cmd/gosshd-server ./cmd/gosshd-agent
```

浏览器 E2E 需要显式提供 Node、Playwright 和 Chrome 路径：

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
$env:GOSSHD_UI_E2E_NODE='C:\path\to\node.exe'
$env:GOSSHD_UI_E2E_PLAYWRIGHT='C:\path\to\playwright'
$env:GOSSHD_UI_E2E_BROWSER='C:\path\to\chrome.exe'
go test ./internal/server -run TestUIE2EWithBrowser -v
```

## 发布形态

Releases 会发布跨平台 server 压缩包、独立私有节点二进制和 checksums。本版本不发布 `full` 包。

发布打包仅由 `v*` 标签或手动运行 Release 工作流触发；ARM64 单独构建仅手动触发。普通代码推送运行回归测试，不生成发布包。GitHub Actions 不再打包 Windows 桌面客户端，Windows server 和 Agent 仍随版本发布。
