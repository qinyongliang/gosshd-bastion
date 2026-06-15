# gosshd

[English](README.md) | 简体中文

`gosshd` 是一个极简 Go SSH 中转项目：公网服务器接受标准 SSH/SFTP/SCP/隧道客户端连接，私有网络内的 agent 只需要主动连出一个 WebSocket，就能把本机通过稳定 UUID 暴露为：

```text
ssh UUID@public-host
```

v1 中，UUID 就是访问凭证。任何知道该 UUID 的人，都可以用运行 `gosshd-agent` 的系统用户权限访问 agent 所在机器。

## 构建

```powershell
go mod tidy
go build -o bin/gosshd-server.exe ./cmd/gosshd-server
go build -o bin/gosshd-agent.exe ./cmd/gosshd-agent
```

为服务器下载接口交叉构建 agent：

```powershell
$env:GOOS='linux'; $env:GOARCH='amd64'; go build -o dist/agent/linux/amd64/gosshd-agent ./cmd/gosshd-agent
$env:GOOS='windows'; $env:GOARCH='amd64'; go build -o dist/agent/windows/amd64/gosshd-agent.exe ./cmd/gosshd-agent
Remove-Item Env:\GOOS,Env:\GOARCH
```

Release 包由 GitHub Actions 在创建 GitHub Release 时自动构建，覆盖 Linux、Windows、macOS、FreeBSD 等常见系统和 CPU 架构。

## 运行

开发端口：

```powershell
bin/gosshd-server.exe --http-listen :8080 --ssh-listen :2222 --public-host localhost:8080 --agent-path dist/agent
bin/gosshd-agent.exe --server http://localhost:8080
```

生产默认端口可通过参数配置，HTTP 默认 `:80`，SSH 默认 `:22`。

## Docker 服务端

使用 Alpine runtime 镜像构建 Linux 服务端镜像，并内置支持矩阵中的可下载 agent：

```powershell
docker build -t gosshd-server:latest .
```

本地高端口运行：

```powershell
docker run --rm -p 8080:80 -p 2222:22 gosshd-server:latest --public-host localhost:8080 --http-listen :80 --ssh-listen :22 --agent-path /app/agent
```

公网主机默认端口运行：

```sh
docker run -d --name gosshd-server --restart unless-stopped \
  -p 80:80 -p 22:22 \
  gosshd-server:latest \
  --public-host your.host.name --http-listen :80 --ssh-listen :22 --agent-path /app/agent
```

如果宿主机的 SSH 管理服务也占用 `22`，建议先映射到高端口，例如 `-p 2222:22`。

## Agent 快速启动

Linux/macOS：

```sh
curl http://public-host/install.sh | sh
```

Windows PowerShell：

```powershell
irm http://public-host/install.ps1 | iex
```

Agent 启动后会打印类似地址：

```text
ssh UUID@public-host
```

非默认 SSH 端口示例：

```sh
ssh -p 2222 UUID@public-host
sftp -P 2222 UUID@public-host
scp -P 2222 file UUID@public-host:/tmp/file
ssh -p 2222 -L 15432:127.0.0.1:5432 UUID@public-host
ssh -p 2222 -D 1080 UUID@public-host
ssh -p 2222 -R 0:127.0.0.1:8080 UUID@public-host
```

## 发布

在 GitHub 创建 Release 后，`.github/workflows/release.yml` 会自动构建各平台压缩包并上传到 Release assets。

也可以在 GitHub Actions 页面手动运行 `Release` workflow，用于打包冒烟测试。

## v1 说明

- Agent 是临时前台进程，不会安装为 systemd 或 Windows Service。
- Agent UUID 默认保存到 `~/.gosshd/agent.json`。
- 服务端状态只保存在内存中；agent 离线后不会持久化设备记录。
- SFTP 暴露 agent 进程可访问的文件系统。
- 远程转发只允许绑定公网服务器上的 `127.0.0.1`/`localhost`。
- TLS、Web UI、审计日志、多用户认证、常驻服务安装暂不属于 v1 范围。
