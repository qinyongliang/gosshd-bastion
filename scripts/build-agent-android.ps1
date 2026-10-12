param(
  [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._+-]*$')]
  [string]$Version = 'dev',
  [string]$NDKPath = $env:ANDROID_NDK_HOME
)

$ErrorActionPreference = 'Stop'
if (-not $NDKPath) { $NDKPath = $env:ANDROID_NDK_ROOT }
if (-not $NDKPath) { throw 'Set ANDROID_NDK_HOME or pass -NDKPath to an Android NDK installation (r27c or newer).' }
$NDKPath = (Resolve-Path -LiteralPath $NDKPath).Path
$hostTag = if ($env:OS -eq 'Windows_NT') { 'windows-x86_64' } elseif ($IsMacOS) { 'darwin-x86_64' } else { 'linux-x86_64' }
$clangName = if ($env:OS -eq 'Windows_NT') { 'clang.exe' } else { 'clang' }
$clang = Join-Path $NDKPath "toolchains/llvm/prebuilt/$hostTag/bin/$clangName"
if (-not (Test-Path -LiteralPath $clang -PathType Leaf)) { throw "Android NDK compiler not found: $clang" }

$platforms = @(
  @{ Arch='armv7'; GOARCH='arm'; GOARM='7'; Target='armv7a-linux-androideabi24' },
  @{ Arch='arm64'; GOARCH='arm64'; GOARM=''; Target='aarch64-linux-android24' }
)
$names = @('GOOS', 'GOARCH', 'GOARM', 'CGO_ENABLED', 'CC')
$saved = @{}
foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
  New-Item -ItemType Directory -Force -Path dist | Out-Null
  foreach ($p in $platforms) {
    $dir = Join-Path "build/agent/$Version/android" $p.Arch
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $output = Join-Path $dir 'gosshd-agent'
    $env:GOOS = 'android'
    $env:GOARCH = $p.GOARCH
    $env:GOARM = $p.GOARM
    $env:CGO_ENABLED = '1'
    # Use clang directly: the NDK's .cmd wrappers are not portable Go CC commands.
    $env:CC = '"' + $clang + '" --target=' + $p.Target
    # anet uses restricted linknames on Android; do not apply this workaround globally.
    $flags = "-s -w -checklinkname=0 -extldflags=-Wl,-z,max-page-size=16384 -X main.version=$Version"
    Write-Host "building agent android/$($p.Arch) (API 24+)"
    go build -buildmode=pie -trimpath -ldflags $flags -o $output ./cmd/gosshd-agent
    if ($LASTEXITCODE -ne 0) { throw "Android $($p.Arch) build failed with exit code $LASTEXITCODE" }
    Copy-Item -LiteralPath $output -Destination "dist/gosshd-agent-$Version-android-$($p.Arch)"
  }
} finally {
  foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
  Pop-Location
}
