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
$names = @('GOOS','GOARCH','GOARM','CGO_ENABLED','CC')
$saved = @{}
foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name,'Process') }
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
  foreach ($p in $platforms) {
    $dir = "build/desktop/android/$Version/$($p.Arch)"
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $env:GOOS='android'; $env:GOARCH=$p.GOARCH; $env:GOARM=$p.GOARM; $env:CGO_ENABLED='1'
    $env:CC='"' + $clang + '" --target=' + $p.Target
    Write-Host "building independent desktop MCP android/$($p.Arch) (API 24+)"
    $flags = "-s -w -extldflags=-Wl,-z,max-page-size=16384 -X main.version=$Version"
    go build -buildmode=pie -trimpath -ldflags $flags -o "$dir/gosshd-desktop-mcp" ./cmd/gosshd-desktop-mcp
    if ($LASTEXITCODE -ne 0) { throw "Android desktop $($p.Arch) build failed" }
    Copy-Item -LiteralPath docs/desktop-mcp.md,docs/android-desktop-mcp.md -Destination $dir
  }
} finally {
  foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name,$saved[$name],'Process') }
  Pop-Location
}
