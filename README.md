# gosshd

[English](README.md) | [简体中文](README.zh-CN.md)

`gosshd` is a small SSH relay for reaching a private-network machine through a public server. A private host runs `gosshd-agent`, the public host runs `gosshd-server`, and users connect with standard SSH tools:

```text
ssh UUID@public-host
```

The UUID is the access secret in the current version. Anyone who knows it can access the agent machine with the permissions of the `gosshd-agent` process.

## Architecture

```text
  Any network                         Public network                         Private network
+-------------+    ssh/sftp/scp     +---------------+    outbound ws      +---------------+
| SSH client  | ------------------> | gosshd-server | <------------------ | gosshd-agent  |
|             |                     | :22 / :80     |                     | shell / sftp  |
+-------------+                     +---------------+                     +-------+-------+
                                                                                  |
                                                                          +-------v-------+
                                                                          | private host  |
                                                                          +---------------+
```

## Quick Start

Start the public server with the latest GitHub Release binary:

```sh
curl -fsSL https://raw.githubusercontent.com/qinyongliang/gosshd/main/run.sh | \
  sudo sh -s -- --http-listen :80 --ssh-listen :22 --public-host public-host
```

Windows:

```powershell
$run = "$env:TEMP\gosshd-run.ps1"; iwr -UseBasicParsing https://raw.githubusercontent.com/qinyongliang/gosshd/main/run.ps1 -OutFile $run; powershell -NoProfile -ExecutionPolicy Bypass -File $run --http-listen :80 --ssh-listen :22 --public-host public-host
```

Start an agent on a private Linux/macOS host:

```sh
curl http://public-host/run.sh | sh
```

The agent prints an SSH address. Use it from anywhere:

```sh
ssh UUID@public-host
sftp UUID@public-host
scp file UUID@public-host:/tmp/file
```

## Notes

- The agent run script is temporary-run oriented; it does not install a service.
- The server provides the agent run script used by private hosts.
- SSH tunneling is supported by standard SSH clients.

See [Releases](https://github.com/qinyongliang/gosshd/releases) for prebuilt binaries.
