param(
  [string]$SDKPath = $env:ANDROID_HOME,
  [string]$JavaHome = $env:JAVA_HOME
)
$ErrorActionPreference = 'Stop'
if (-not $SDKPath) { $SDKPath = Join-Path $env:LOCALAPPDATA 'Android/Sdk' }
$SDKPath = (Resolve-Path -LiteralPath $SDKPath).Path
if (-not $JavaHome) { throw 'Set JAVA_HOME to a JDK installation.' }
$tools = Join-Path $SDKPath 'build-tools/35.0.0'
$androidJar = Join-Path $SDKPath 'platforms/android-35/android.jar'
foreach ($file in @('aapt2.exe','d8.bat','zipalign.exe','apksigner.bat')) {
  if (-not (Test-Path -LiteralPath (Join-Path $tools $file))) { throw "Missing Android build tool: $file" }
}
function Invoke-Checked([string]$Tool, [string[]]$ToolArgs) {
  & $Tool @ToolArgs
  if ($LASTEXITCODE -ne 0) { throw "$Tool failed (exit $LASTEXITCODE)" }
}
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
  $out = Join-Path (Get-Location) 'build/desktop/android/fixture'
  $source = Join-Path (Get-Location) 'internal/desktop/native/android/fixture'
  foreach ($sub in @('gen','classes','dex')) { New-Item -ItemType Directory -Force -Path (Join-Path $out $sub) | Out-Null }
  Invoke-Checked (Join-Path $tools 'aapt2.exe') @('compile','--dir',(Join-Path $source 'res'),'-o',(Join-Path $out 'resources.zip'))
  $unsigned = Join-Path $out 'fixture-unsigned.apk'
  Invoke-Checked (Join-Path $tools 'aapt2.exe') @('link','-I',$androidJar,'--manifest',(Join-Path $source 'AndroidManifest.xml'),'--java',(Join-Path $out 'gen'),'-o',$unsigned,(Join-Path $out 'resources.zip'))
  $javaFiles = @((Join-Path $source 'MainActivity.java')) + @(Get-ChildItem -LiteralPath (Join-Path $out 'gen') -Filter '*.java' -Recurse | ForEach-Object FullName)
  Invoke-Checked (Join-Path $JavaHome 'bin/javac.exe') (@('--release','8','-encoding','UTF-8','-classpath',$androidJar,'-d',(Join-Path $out 'classes'))+$javaFiles)
  $classFiles = @(Get-ChildItem -LiteralPath (Join-Path $out 'classes') -Filter '*.class' -Recurse | ForEach-Object FullName)
  Invoke-Checked (Join-Path $tools 'd8.bat') (@('--min-api','24','--lib',$androidJar,'--output',(Join-Path $out 'dex'))+$classFiles)
  Invoke-Checked (Join-Path $JavaHome 'bin/jar.exe') @('uf',$unsigned,'-C',(Join-Path $out 'dex'),'classes.dex')
  $aligned = Join-Path $out 'fixture-aligned.apk'
  Invoke-Checked (Join-Path $tools 'zipalign.exe') @('-f','4',$unsigned,$aligned)
  # Ephemeral fixture key stays under ignored build/ and signs only this test APK.
  $key = Join-Path $out 'fixture.keystore'
  if (-not (Test-Path -LiteralPath $key)) {
    Invoke-Checked (Join-Path $JavaHome 'bin/keytool.exe') @('-genkeypair','-keystore',$key,'-storepass','fixture-only','-keypass','fixture-only','-alias','fixture','-keyalg','RSA','-keysize','2048','-validity','3650','-dname','CN=Example Fixture','-noprompt')
  }
  $apk = Join-Path $out 'fixture.apk'
  Invoke-Checked (Join-Path $tools 'apksigner.bat') @('sign','--ks',$key,'--ks-pass','pass:fixture-only','--key-pass','pass:fixture-only','--out',$apk,$aligned)
  Invoke-Checked (Join-Path $tools 'apksigner.bat') @('verify',$apk)
  Write-Output "Built owned Android fixture: $apk"
} finally { Pop-Location }
