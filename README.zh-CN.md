# GOSSHD Bastion

[English](README.md) | 简体中文

**GOSSHD Bastion 是为 AI 服务设计的堡垒机：** 一个单体 Go 服务，为运维人员、自动化工具和 AI 智能体提供原生 SSH 访问，同时具备别名路由、命令安全组、LLM 审核、审计搜索和终端回放。

![GOSSHD Bastion 宣传图](site/assets/hero-ai-bastion.png)

## 为什么需要它

AI 工具能比人更快地读取日志、检查容器、重启服务、串联命令。直接给它一把 SSH 私钥太危险；把每条命令都交给人工审批又太慢。

GOSSHD Bastion 位于 AI 任务和私有基础设施之间：

- SSH 使用方式不变：`ssh inference-gpu@bastion.example.com`。
- 公钥识别平台用户，SSH 用户名解析目标别名。
- 目标可以是直连 SSH 服务器，也可以是 NAT 后方注册上来的私有节点。
- 命令安全组支持白名单、黑名单、来源 IP、用户组、目标标签和能力开关。
- 未命中的命令可以交给 OpenAI 兼容 LLM 实时审核。
- Exec 决策和交互式终端都会进入审计，终端录制会压缩存储并支持回放。

## 产品预览

![GOSSHD Bastion 控制台](site/assets/console-dashboard.png)

内置控制台可以管理组织、成员、用户组、公钥、SSH 服务、彩色标签、私有节点安装、安全组、LLM 配置、提示词、审计搜索和终端回放。

![LLM 审核概念图](site/assets/llm-review-panel.png)

## 核心能力

- **别名原生 SSH：** 用户通过 `ssh alias@public-ip` 访问目标。
- **SQLite 控制平面：** 用户、组织、会话、用户组、公钥、目标、标签、策略、提示词和 LLM 配置本地持久化。
- **独立审计数据库：** 命令审计数据和主控制数据库分离。
- **私有节点：** Linux/macOS 和 Windows 安装命令自带注册令牌；开机启动模式在 Linux 使用 systemd，在 Windows 使用 `sc.exe`。
- **命令安全组：** 支持黑名单、白名单、LLM 兜底、IP 白名单、目标/标签绑定、用户组绑定、交互式终端、端口转发、上传和下载控制。
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
5. 通过堡垒机连接：

```sh
ssh -p 22022 inference-gpu@bastion.example.com hostname
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

## 官网和文档

GitHub Pages 源码位于 [`site/`](site/)。里面包含宣传首页、文档页、生成的宣传视觉资源，以及官网里的 xterm 风格终端回放演示。

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
