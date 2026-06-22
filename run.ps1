param(
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$ServerArgs
)

$ErrorActionPreference = "Stop"

$repo = if ($env:GOSSHD_REPO) { $env:GOSSHD_REPO } else { "qinyongliang/gosshd-bastion" }
$version = $env:GOSSHD_VERSION
$proxy = if ($env:GOSSHD_PROXY_URL) { $env:GOSSHD_PROXY_URL } else { "https://gh-proxy.com/" }

if (-not $version) {
  $latest = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$repo/releases/latest"
  $version = $latest.tag_name
}

if (-not $version) {
  throw "unable to resolve latest gosshd release version"
}

$machine = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($machine) {
  "x64" { $arch = "amd64" }
  "x86" { $arch = "386" }
  "arm64" { $arch = "arm64" }
  default { throw "unsupported arch: $machine" }
}

$platform = "windows-$arch"
$asset = "gosshd-$version-$platform.zip"
$url = "https://github.com/$repo/releases/download/$version/$asset"
$checksumsUrl = "https://github.com/$repo/releases/download/$version/checksums.txt"
$tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) "gosshd"
$archive = Join-Path $tmpRoot $asset
$checksums = Join-Path $tmpRoot "checksums-$version.txt"
$extractDir = Join-Path $tmpRoot "server-$version-$platform-$PID"

New-Item -ItemType Directory -Force -Path $tmpRoot | Out-Null

function Download-File {
  param(
    [string]$Source,
    [string]$Target
  )
  Invoke-WebRequest -UseBasicParsing -Uri $Source -OutFile $Target
}

Write-Host "downloading $url"
try {
  Download-File -Source $url -Target $archive
} catch {
  $proxyUrl = ($proxy.TrimEnd("/") + "/" + $url)
  Write-Warning "direct download failed; retrying $proxyUrl"
  Download-File -Source $proxyUrl -Target $archive
}
try {
  Download-File -Source $checksumsUrl -Target $checksums
} catch {
  $proxyChecksumsUrl = ($proxy.TrimEnd("/") + "/" + $checksumsUrl)
  Write-Warning "checksum download failed; retrying $proxyChecksumsUrl"
  Download-File -Source $proxyChecksumsUrl -Target $checksums
}
$expectedSha = $null
foreach ($line in Get-Content $checksums) {
  $parts = $line -split "\s+"
  if ($parts.Length -ge 2 -and ($parts[1] -eq $asset -or $parts[1] -eq "*$asset")) {
    $expectedSha = $parts[0].ToLowerInvariant()
    break
  }
}
if (-not $expectedSha) {
  throw "checksum for $asset not found"
}
$actualSha = (Get-FileHash -Algorithm SHA256 -Path $archive).Hash.ToLowerInvariant()
if ($actualSha -ne $expectedSha) {
  throw "sha256 mismatch: $actualSha != $expectedSha"
}

New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
Expand-Archive -Force -Path $archive -DestinationPath $extractDir

$server = Join-Path $extractDir "gosshd-$platform\gosshd-server.exe"
if (-not (Test-Path $server)) {
  throw "server binary not found in archive: $server"
}

Write-Host "starting $server $($ServerArgs -join ' ')"
& $server @ServerArgs
exit $LASTEXITCODE
