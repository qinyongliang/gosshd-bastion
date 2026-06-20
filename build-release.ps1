param(
  [string]$Version = 'v0.1.0-bastion'
)

$ErrorActionPreference = 'Stop'

$pnpm = Get-Command pnpm -ErrorAction SilentlyContinue
if (-not $pnpm) {
  throw 'pnpm is required to build the React console assets.'
}

& $pnpm.Source install --frozen-lockfile
& $pnpm.Source run build

$serverPlatforms = @(
  @{ GOOS='linux'; GOARCH='amd64'; GOARM=''; Platform='linux-amd64'; AgentArch='amd64'; Ext='' },
  @{ GOOS='linux'; GOARCH='arm64'; GOARM=''; Platform='linux-arm64'; AgentArch='arm64'; Ext='' },
  @{ GOOS='linux'; GOARCH='386'; GOARM=''; Platform='linux-386'; AgentArch='386'; Ext='' },
  @{ GOOS='linux'; GOARCH='arm'; GOARM='6'; Platform='linux-armv6'; AgentArch='armv6'; Ext='' },
  @{ GOOS='linux'; GOARCH='arm'; GOARM='7'; Platform='linux-armv7'; AgentArch='armv7'; Ext='' },
  @{ GOOS='linux'; GOARCH='riscv64'; GOARM=''; Platform='linux-riscv64'; AgentArch='riscv64'; Ext='' },
  @{ GOOS='windows'; GOARCH='amd64'; GOARM=''; Platform='windows-amd64'; AgentArch='amd64'; Ext='.exe' },
  @{ GOOS='windows'; GOARCH='arm64'; GOARM=''; Platform='windows-arm64'; AgentArch='arm64'; Ext='.exe' },
  @{ GOOS='windows'; GOARCH='386'; GOARM=''; Platform='windows-386'; AgentArch='386'; Ext='.exe' },
  @{ GOOS='darwin'; GOARCH='amd64'; GOARM=''; Platform='darwin-amd64'; AgentArch='amd64'; Ext='' },
  @{ GOOS='darwin'; GOARCH='arm64'; GOARM=''; Platform='darwin-arm64'; AgentArch='arm64'; Ext='' },
  @{ GOOS='freebsd'; GOARCH='amd64'; GOARM=''; Platform='freebsd-amd64'; AgentArch='amd64'; Ext='' },
  @{ GOOS='freebsd'; GOARCH='arm64'; GOARM=''; Platform='freebsd-arm64'; AgentArch='arm64'; Ext='' },
  @{ GOOS='openbsd'; GOARCH='amd64'; GOARM=''; Platform='openbsd-amd64'; AgentArch='amd64'; Ext='' },
  @{ GOOS='openbsd'; GOARCH='arm64'; GOARM=''; Platform='openbsd-arm64'; AgentArch='arm64'; Ext='' }
)

$agentPlatforms = $serverPlatforms + @(
  @{ GOOS='netbsd'; GOARCH='amd64'; GOARM=''; Platform='netbsd-amd64'; AgentArch='amd64'; Ext='' },
  @{ GOOS='netbsd'; GOARCH='arm64'; GOARM=''; Platform='netbsd-arm64'; AgentArch='arm64'; Ext='' }
)

function Invoke-GoBuild {
  param(
    [hashtable]$Platform,
    [string]$Output,
    [string]$Package
  )

  $env:CGO_ENABLED = '0'
  $env:GOOS = $Platform.GOOS
  $env:GOARCH = $Platform.GOARCH
  if ($Platform.GOARM) {
    $env:GOARM = $Platform.GOARM
  } else {
    Remove-Item Env:\GOARM -ErrorAction SilentlyContinue
  }

  go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $Output $Package
}

Remove-Item -Recurse -Force build, package, dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path build/server, build/agent, dist | Out-Null

foreach ($p in $serverPlatforms) {
  $serverDir = Join-Path 'build/server' $p.Platform
  New-Item -ItemType Directory -Force -Path $serverDir | Out-Null
  $serverOut = Join-Path $serverDir ('gosshd-server' + $p.Ext)

  Write-Host "building server $($p.Platform)"
  Invoke-GoBuild -Platform $p -Output $serverOut -Package './cmd/gosshd-server'
}

foreach ($p in $agentPlatforms) {
  $agentDir = Join-Path (Join-Path 'build/agent' $p.GOOS) $p.AgentArch
  New-Item -ItemType Directory -Force -Path $agentDir | Out-Null
  $agentOut = Join-Path $agentDir ('gosshd-agent' + $p.Ext)

  Write-Host "building agent $($p.Platform)"
  Invoke-GoBuild -Platform $p -Output $agentOut -Package './cmd/gosshd-agent'
  Copy-Item $agentOut (Join-Path 'dist' ("gosshd-agent-$Version-$($p.GOOS)-$($p.AgentArch)$($p.Ext)"))
}

foreach ($p in $serverPlatforms) {
  $pkgRoot = Join-Path 'package' ("gosshd-$($p.Platform)")
  New-Item -ItemType Directory -Force -Path $pkgRoot | Out-Null

  Copy-Item (Join-Path (Join-Path 'build/server' $p.Platform) ('gosshd-server' + $p.Ext)) $pkgRoot
  Copy-Item README.md, README.zh-CN.md, run.sh, run.ps1 $pkgRoot
  if (Test-Path LICENSE) {
    Copy-Item LICENSE $pkgRoot
  }

  if ($p.GOOS -eq 'windows') {
    Compress-Archive -Force -Path $pkgRoot -DestinationPath (Join-Path 'dist' ("gosshd-$Version-$($p.Platform).zip"))
  } else {
    tar -C package -czf (Join-Path 'dist' ("gosshd-$Version-$($p.Platform).tar.gz")) ("gosshd-$($p.Platform)")
  }

  Remove-Item -Recurse -Force $pkgRoot
}

Remove-Item Env:\GOOS, Env:\GOARCH, Env:\GOARM, Env:\CGO_ENABLED -ErrorAction SilentlyContinue

Get-ChildItem dist -File |
  Sort-Object Name |
  Get-FileHash -Algorithm SHA256 |
  ForEach-Object { "$($_.Hash.ToLower())  $([IO.Path]::GetFileName($_.Path))" } |
  Set-Content -Encoding ascii dist\checksums.txt
