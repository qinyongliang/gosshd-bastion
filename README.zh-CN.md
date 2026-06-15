# gosshd

[English](README.md) | 简体中文

`gosshd` 是一个小型 SSH 中转工具，用于通过公网服务器访问私有网络中的机器。私网机器运行 `gosshd-agent`，公网机器运行 `gosshd-server`，用户直接使用标准 SSH 工具连接：

```text
ssh UUID@public-host
```

当前版本中，UUID 就是访问凭证。任何知道 UUID 的人，都可以用 `gosshd-agent` 进程的系统权限访问这台机器。

## 架构

```text
  任意网络                            公网服务器                            私有网络
+-------------+    ssh/sftp/scp     +---------------+    outbound ws      +---------------+
| SSH client  | ------------------> | gosshd-server | <------------------ | gosshd-agent  |
|             |                     | :22 / :80     |                     | shell / sftp  |
+-------------+                     +---------------+                     +-------+-------+
                                                                                  |
                                                                          +-------v-------+
                                                                          | private host  |
                                                                          +---------------+
```

## 快速使用

使用 GitHub Release 中的最新版二进制启动公网服务器：

```sh
curl -fsSL https://raw.githubusercontent.com/qinyongliang/gosshd/main/run.sh | \
  sudo sh -s -- --http-listen :80 --ssh-listen :22 --public-host public-host
```

Windows：

```powershell
$run = "$env:TEMP\gosshd-run.ps1"; iwr -UseBasicParsing https://raw.githubusercontent.com/qinyongliang/gosshd/main/run.ps1 -OutFile $run; powershell -NoProfile -ExecutionPolicy Bypass -File $run --http-listen :80 --ssh-listen :22 --public-host public-host
```

在私有网络的 Linux/macOS 主机上启动 agent：

```sh
curl http://public-host/run.sh | sh
```

Agent 会打印 SSH 地址，然后可以在任意网络访问：

```sh
ssh UUID@public-host
sftp UUID@public-host
scp file UUID@public-host:/tmp/file
```

## 说明

- agent 运行脚本只用于临时运行，不会安装系统服务。
- server 会提供私网机器使用的 agent 运行脚本。
- SSH 隧道能力由标准 SSH 客户端支持。

预编译二进制见 [Releases](https://github.com/qinyongliang/gosshd/releases)。
