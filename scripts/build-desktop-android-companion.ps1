param(
  [string]$SDKPath = $env:ANDROID_HOME,
  [string]$JavaHome = $env:JAVA_HOME,
  [switch]$Debug,
  [string]$SigningKey,
  [string]$SigningAlias = 'companion',
  [ValidateRange(1,2100000000)][int]$VersionCode = 1,
  [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._+/-]{0,79}$')][string]$VersionName = 'dev',
  [string]$TestMCPBinary
)
$ErrorActionPreference = 'Stop'
if (-not $SDKPath) { $SDKPath = Join-Path $env:LOCALAPPDATA 'Android/Sdk' }
if (-not $JavaHome) { throw 'Set JAVA_HOME to a JDK installation.' }
$SDKPath = (Resolve-Path -LiteralPath $SDKPath).Path
$tools = Join-Path $SDKPath 'build-tools/35.0.0'
$androidJar = Join-Path $SDKPath 'platforms/android-35/android.jar'
function Invoke-Checked([string]$Tool, [string[]]$ToolArgs) {
  & $Tool @ToolArgs
  if ($LASTEXITCODE -ne 0) { throw "$Tool failed" }
}
function Get-Sha256([string]$Path) {
  $stream = [IO.File]::OpenRead($Path)
  $sha = [Security.Cryptography.SHA256]::Create()
  try { return [BitConverter]::ToString($sha.ComputeHash($stream)).Replace('-','').ToLowerInvariant() }
  finally { $stream.Dispose(); $sha.Dispose() }
}
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
  $out = Join-Path (Get-Location) 'build/desktop/android/companion'
  $source = Join-Path (Get-Location) 'client/android-desktop/src/main'
  foreach ($sub in @('gen','classes','dex','deps')) {
    New-Item -ItemType Directory -Force -Path (Join-Path $out $sub) | Out-Null
  }
  $hashes = @{
    api='4def9bde498ef8626614c2fc5db9af4749c86f16f6c33e3f5658d35e70bab59b'
    provider='b0f18cd9812464ec171c53cac93a819fe411718a3965c311f01eb4de265381b3'
    aidl='33fe7191cdd69fcb66d649264f3b0c47acb2f3d6343afc05b98dbbff6f221963'
    shared='4659642c9339be0a26e9c65bb8648f7ad6d8f4a465f557993ccbc78802381635'
  }
  $jars = @()
  foreach ($name in @('api','provider','aidl','shared')) {
    $archive = Join-Path $out "deps/$name.aar"
    if (-not (Test-Path -LiteralPath $archive)) {
      Invoke-WebRequest -Uri "https://repo.maven.apache.org/maven2/dev/rikka/shizuku/$name/13.1.5/$name-13.1.5.aar" -OutFile $archive
    }
    if ((Get-Sha256 $archive) -ne $hashes[$name]) {
      throw "Shizuku $name dependency checksum mismatch"
    }
    $dir = Join-Path $out "deps/$name"
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Push-Location $dir
    try { Invoke-Checked (Join-Path $JavaHome 'bin/jar.exe') @('xf',$archive,'classes.jar') }
    finally { Pop-Location }
    $jars += Join-Path $dir 'classes.jar'
  }
  $annotation = Join-Path $out 'deps/annotation.jar'
  if (-not (Test-Path -LiteralPath $annotation)) {
    Invoke-WebRequest -Uri 'https://dl.google.com/dl/android/maven2/androidx/annotation/annotation/1.3.0/annotation-1.3.0.jar' -OutFile $annotation
  }
  if ((Get-Sha256 $annotation) -ne '97dc45afefe3a1e421da42b8b6e9f90491477c45fc6178203e3a5e8a05ee8553') {
    throw 'AndroidX annotation checksum mismatch'
  }
  $jars += $annotation
  $manifest = [IO.File]::ReadAllText((Join-Path $source 'AndroidManifest.xml'))
  $versionAttributes = '<manifest android:versionCode="' + $VersionCode + '" android:versionName="' + [Security.SecurityElement]::Escape($VersionName) + '" '
  $manifest = $manifest.Replace('<manifest ', $versionAttributes)
  if ($Debug) { $manifest = $manifest.Replace('<application ', '<application android:debuggable="true" ') }
  $manifestFile = Join-Path $out 'AndroidManifest.xml'
  [IO.File]::WriteAllText($manifestFile,$manifest,[Text.UTF8Encoding]::new($false))
  Invoke-Checked (Join-Path $tools 'aapt2.exe') @('compile','--dir',(Join-Path $source 'res'),'-o',(Join-Path $out 'resources.zip'))
  $unsigned = Join-Path $out 'companion-unsigned.apk'
  Invoke-Checked (Join-Path $tools 'aapt2.exe') @('link','-I',$androidJar,'--manifest',$manifestFile,'--java',(Join-Path $out 'gen'),'-o',$unsigned,(Join-Path $out 'resources.zip'))
  $aidlDir = Join-Path $source 'aidl'
  $generated = Join-Path $out 'gen'
  foreach ($aidl in Get-ChildItem -LiteralPath $aidlDir -Filter '*.aidl' -Recurse) {
    Invoke-Checked (Join-Path $tools 'aidl.exe') @("-I$aidlDir","-p$SDKPath/platforms/android-35/framework.aidl","-o$generated",$aidl.FullName)
  }
  $javaFiles = @(Get-ChildItem -LiteralPath (Join-Path $source 'java'),$generated -Filter '*.java' -Recurse | ForEach-Object FullName)
  $classpath = (@($androidJar)+$jars) -join [IO.Path]::PathSeparator
  Invoke-Checked (Join-Path $JavaHome 'bin/javac.exe') (@('--release','8','-encoding','UTF-8','-Xlint:-options','-classpath',$classpath,'-d',(Join-Path $out 'classes'))+$javaFiles)
  $classes = @(Get-ChildItem -LiteralPath (Join-Path $out 'classes') -Filter '*.class' -Recurse | ForEach-Object FullName)
  Invoke-Checked (Join-Path $tools 'd8.bat') (@('--min-api','30','--lib',$androidJar,'--output',(Join-Path $out 'dex'))+$classes+$jars)
  Invoke-Checked (Join-Path $JavaHome 'bin/jar.exe') @('uf',$unsigned,'-C',(Join-Path $out 'dex'),'classes.dex')
  if ($TestMCPBinary) {
    if (-not $Debug) { throw 'TestMCPBinary requires Debug' }
    $jni = Join-Path $out 'lib/x86_64'
    New-Item -ItemType Directory -Force -Path $jni | Out-Null
    Copy-Item -LiteralPath $TestMCPBinary -Destination (Join-Path $jni 'libdesktop_mcp.so')
    Invoke-Checked (Join-Path $JavaHome 'bin/jar.exe') @('uf',$unsigned,'-C',$out,'lib/x86_64/libdesktop_mcp.so')
  }
  $aligned = Join-Path $out 'companion-aligned.apk'
  Invoke-Checked (Join-Path $tools 'zipalign.exe') @('-f','-P','16','4',$unsigned,$aligned)
  if (-not $SigningKey) {
    if (-not $Debug) { throw 'Production APK requires a stable SigningKey; use Debug for local acceptance' }
    $SigningKey = Join-Path $out 'debug.keystore'
    if (-not (Test-Path -LiteralPath $SigningKey)) {
      Invoke-Checked (Join-Path $JavaHome 'bin/keytool.exe') @('-genkeypair','-keystore',$SigningKey,'-storepass','local-test-only','-keypass','local-test-only','-alias',$SigningAlias,'-keyalg','RSA','-keysize','2048','-validity','3650','-dname','CN=Example Local Companion','-noprompt')
    }
    $passArgs = @('--ks-pass','pass:local-test-only','--key-pass','pass:local-test-only')
  } else {
    $passArgs = @('--ks-pass','env:GOSSHD_APK_KEYSTORE_PASSWORD')
  }
  $apk = Join-Path $out 'gosshd-desktop-companion.apk'
  Invoke-Checked (Join-Path $tools 'apksigner.bat') (@('sign','--ks',$SigningKey,'--ks-key-alias',$SigningAlias,'--out',$apk)+$passArgs+$aligned)
  # Windows PowerShell converts captured native stderr warnings into error records.
  # Preserve them for diagnostics, but decide success only from the native exit code.
  $previousPolicy = $ErrorActionPreference
  try {
    $ErrorActionPreference = 'Continue'
    $verification = & (Join-Path $tools 'apksigner.bat') verify --print-certs $apk 2>&1
    $verifyExit = $LASTEXITCODE
  } finally { $ErrorActionPreference = $previousPolicy }
  if ($verifyExit -ne 0) { throw 'APK signature verification failed' }
  if (-not $Debug) {
    $expected = (Get-Content -LiteralPath 'client/android-desktop/signing/release-certificate.sha256' -Raw).Trim().ToLowerInvariant()
    $signer = [regex]::Match(($verification -join "`n"),'Signer #1 certificate SHA-256 digest:\s*([0-9a-fA-F]{64})')
    if ($expected -notmatch '^[0-9a-f]{64}$' -or -not $signer.Success -or $signer.Groups[1].Value.ToLowerInvariant() -ne $expected) {
      throw 'APK signer does not match the maintained public release certificate'
    }
  }
  New-Item -ItemType Directory -Force -Path dist | Out-Null
  $distName = if ($Debug) { 'dist/gosshd-desktop-companion-debug.apk' } else { 'dist/gosshd-desktop-companion.apk' }
  Copy-Item -LiteralPath $apk -Destination $distName
  if (-not $Debug) {
    $sha = (Get-Sha256 $distName)
    [IO.File]::WriteAllText((Join-Path (Get-Location) ($distName+'.sha256')),$sha+'  '+[IO.Path]::GetFileName($distName)+[char]10,[Text.UTF8Encoding]::new($false))
    Copy-Item -LiteralPath 'client/android-desktop/signing/release-certificate.pem' -Destination 'dist/gosshd-desktop-companion-certificate.pem'
  }
  Write-Output "Built Android companion: $apk"
} finally { Pop-Location }
