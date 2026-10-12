# GOSSHD Bastion

[English](README.md) | 简体中文

## 远程运维，一个入口。每一步，都有边界。

**GOSSHD Bastion 是人和 AI 共用的自托管远程运维工作台。** 把 SSH 终端、远程文件管理、私有节点和 TCP 隧道集中到一个入口，让团队在浏览器里完成日常工作，让 AI 通过标准 SSH 或 MCP 参与巡检与辅助排障；目标凭证、访问策略和审计记录由你管理。

[中文官网](https://qinyongliang.github.io/gosshd-bastion/index.zh-CN.html) · [下载 Server](https://github.com/qinyongliang/gosshd-bastion/releases/latest) · [产品介绍与发布文案](site/brief.zh-CN.md)

## 为什么选择它

- **工作集中：** 终端、文件编辑、文件夹上传和批量复制 / 移动在同一工作台完成，减少工具切换。
- **人机共用：** 浏览器适合日常操作，标准 SSH 兼容既有流程，MCP 面向 AI 工具。
- **边界明确：** 用组织、用户组、目标、标签、来源 IP 与能力开关约束访问，而不是到处分发目标服务器凭证。
- **过程可查：** 命令审计记录判定和退出码；启用录制的交互式会话可以回放；文件传输和隧道也有对应审计或统计。
- **逐步落地：** 先接入一台测试主机，验证策略，再扩展到内网、客户现场和团队自动化。

## 工作台能力

| 工作 | 提供的能力 |
| --- | --- |
| 排障与配置 | 网页终端、文件编辑器、可拖动调整的工作区布局、桌面与手机访问 |
| 文件管理 | 递归上传文件夹、多选与框选、批量复制 / 移动 / 下载 / 删除 |
| 文件传输 | 尝试浏览器与 Agent 的加密 P2P，无法直连时中转，逐文件鉴权与审计 |
| 内网服务访问 | 持久 TCP 隧道、定时到期、连接与流量及来源 IP 统计 |
| 团队与 AI 访问 | 组织与用户组、命令规则、可选 LLM 审查、临时 SSH 授权、MCP 工具分组 |
| 复盘 | 命令判定、退出码、文件审计与已开启录制的终端回放 |

> 命令策略不是完整 Shell 沙箱，P2P 不保证所有网络都能直连，临时授权标识本身也是敏感凭证。请在生产使用前验证策略、限制能力并保护数据。

## SSH 访问模型

GOSSHD 使用 SSH 的用户名字段承载目标服务别名：

```sh
ssh -p 22022 prod-api@bastion.example.com "uptime"
```

调用方仍需自己的 SSH 身份；公钥用于识别操作者，真实目标的登录凭证由网关管理。目标别名会解析到个人或组织下的 SSH 服务。允许的命令会转发到真实主机；被拒绝的命令会返回标准 SSH 失败结果，例如 `exit status 126`，并写入审计。

短期访问可在 **SSH 服务 -> 对应服务菜单 -> 临时授权** 中创建 UUID，有效期默认 24 小时，可自行设置。复制生成的 `ssh -p 端口 UUID@主机` 命令即可连接对应服务，无需客户端密钥，命令策略和审计身份沿用授权创建者。续期保留原 UUID 并延长到期时间；到期或删除会断开相应连接。普通成员管理自己创建的授权，组织管理员和系统管理员可管理其有权访问的服务下的全部授权。

## 核心能力

- **统一访问控制面：** 团队和智能体通过网页、SSH 或 MCP 访问受管服务器。
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


### Android Agent（ARMv7 / ARM64）

提供 **Android 7.0 / API 24 及以上** 的原生可执行文件：

| Android ABI | 下载平台 | Release 文件 |
| --- | --- | --- |
| `armeabi-v7a`（32 位 ARMv7） | `android/armv7` | `gosshd-agent-<version>-android-armv7` |
| `arm64-v8a`（64 位 ARMv8） | `android/arm64` | `gosshd-agent-<version>-android-arm64` |

这是 Android/Bionic 二进制，不是 Linux ARM 包，也不是 APK。安装脚本优先按 Termux 的**用户空间架构**选择（包括 64 位内核上的 32 位 Termux）；原生 root Shell 则按 Android ABI 选择。Android `arm` 的自动更新请求会映射到 ARMv7 发布文件。

#### 编译

安装 Android NDK r27c（`27.2.12479018`）或更新版本，配置 `ANDROID_NDK_HOME`（也支持 `ANDROID_NDK_ROOT`）。两个架构均使用 NDK/CGO、API 24、PIE 和 16 KiB ELF 页对齐；当前 Go 工具链下 ARMv7 不能使用 `CGO_ENABLED=0` 编译。固定版本的 `anet` 依赖需要 `-checklinkname=0`，脚本仅在 Android 构建中使用这个参数。

```powershell
.\scripts\build-agent-android.ps1 -Version dev -NDKPath 'C:\Android\Sdk\ndk\27.2.12479018'
```

```sh
ANDROID_NDK_HOME="$HOME/Android/Sdk/ndk/27.2.12479018" bash scripts/build-agent-android.sh dev
```

输出到 `build/agent/<version>/android/{armv7,arm64}/gosshd-agent`，以及 `dist/` 中带版本号的文件。完整发布脚本也会构建两个 Android 包并纳入 `checksums.txt`。

本地测试无需先发布 Release：启动相同版本的服务器，并设置 `--agent-path ./build/agent`；目录按版本隔离，避免返回旧构建。

#### 无 root 的 Termux

在私有节点安装抽屉选择 Android 命令，先安装依赖：

```sh
pkg install curl coreutils
# 仅后台服务需要；安装后重新打开 Termux Shell：
pkg install termux-services
```

前台运行，或追加 `install` 安装 runit 后台服务（**不要使用 sudo**）：

```sh
curl -fsSL https://bastion.example.com/install/<token>.sh -o "$TMPDIR/gosshd-agent-install.sh"
sh "$TMPDIR/gosshd-agent-install.sh"          # 前台运行
sh "$TMPDIR/gosshd-agent-install.sh" install  # 后台服务
```

程序、启动脚本、稳定身份和日志保存在 `$HOME/.gosshd/`。使用 `sv up "$PREFIX/var/service/gosshd-agent"`、`sv down ...`、`sv status ...` 控制服务。重启手机后自动运行还需从与 Termux 相同来源安装 **Termux:Boot** 并打开一次；安装器会写入 `$HOME/.termux/boot/gosshd-agent`。请关闭 Termux 电池优化；Android 仍可能结束后台进程，脚本无法保证所有厂商 ROM 下永久保活。

#### 原生已 root Android

先进入 root Shell（`su`），在其中执行 Android 注册命令。需有 `curl` 或 `wget`、SHA256 校验工具及常见 toybox/BusyBox 命令，后台安装还需 `nohup` 和 `readlink`。同一个 `.sh` 安装器会自动识别环境：

```sh
# 在原生 root Shell 内执行，而不是无权限的 adb Shell。
curl -fsSL https://bastion.example.com/install/<token>.sh -o /data/local/tmp/gosshd-agent-install.sh
sh /data/local/tmp/gosshd-agent-install.sh          # 前台运行
sh /data/local/tmp/gosshd-agent-install.sh install  # 后台安装
/data/local/gosshd/control.sh status
/data/local/gosshd/control.sh stop
/data/local/gosshd/control.sh start
```

文件私有保存到 `/data/local/gosshd/`，包括 `agent.json` 和 `agent.log`。重复安装保留身份，SHA256 校验成功后原子替换程序，只重启 **PID 与程序路径均匹配** 的本 Agent 进程。如果已有 `/data/adb/service.d`，会写入可执行的 `gosshd-agent.sh`，供兼容的 Magisk / KernelSU / APatch 开机机制调用；如果没有此目录，则只立即启动后台进程，并提示手动将 `/data/local/gosshd/control.sh start` 接入自己的开机机制。安装器不修改系统分区，也不会关闭 SELinux；具体 ROM 的安全策略可能存在额外限制。

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

将多个文件夹或文件拖入远程文件列表即可递归上传，保留目录结构和空目录。文件列表支持单击选中、双击打开、Shift 连选、Alt 切换单项、拖动框选、Alt 框选切换、Ctrl/Cmd+A 全选和 Escape 清空；右键可批量下载、删除、复制路径，以及将选中的文件和文件夹复制或移动到指定目录。单选复制/移动仍可通过目标路径重命名。

上传和下载都会尝试浏览器与 Agent 之间的 P2P 直连，不可直连时自动使用服务器中转。批量传输复用连接，每个文件仍独立检查权限并记录审计。

## 官网和文档

[中文官网](https://qinyongliang.github.io/gosshd-bastion/index.zh-CN.html) · [产品介绍](site/brief.zh-CN.md) · [隧道说明](docs/tunnels.md)。

GitHub Pages 源码位于 [`site/`](site/)，提供中英文宣传首页、工作台与命令策略交互示意和可下载资料；页面数据不是实时产品会话。

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

发布打包仅由 `v*` 标签或手动运行 Release 工作流触发，并执行完整文件传输回归测试；ARM64 单独构建和 P2P 专项检查仅手动触发。普通代码推送不触发打包或专项检查。GitHub Actions 不再打包 Windows 桌面客户端，Windows server 和 Agent 仍随版本发布。
