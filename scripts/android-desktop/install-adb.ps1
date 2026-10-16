param(
  [string]$ApkPath = './gosshd-agent-android.apk',
  [string]$Serial,
  [string]$AdbPath = 'adb',
  [switch]$EnableAccessibility
)
$ErrorActionPreference = 'Stop'
$ApkPath = (Resolve-Path -LiteralPath $ApkPath).Path
$adb = (Get-Command $AdbPath -ErrorAction Stop).Source
function Invoke-Adb([string[]]$Arguments) {
  $previous = $ErrorActionPreference
  try {
    $ErrorActionPreference = 'Continue'
    $output = & $adb @Arguments 2>&1
    $code = $LASTEXITCODE
  } finally { $ErrorActionPreference = $previous }
  if ($code -ne 0) { throw "ADB failed ($code): $($output -join [Environment]::NewLine)" }
  return $output
}
$devices = @((Invoke-Adb @('devices')) | ForEach-Object {
  if ([string]$_ -match '^\s*(\S+)\s+device\s*$') { $Matches[1] }
})
if ($Serial) {
  if ($Serial -notin $devices) { throw 'Selected device is not connected and authorized.' }
  $target = $Serial
} elseif ($devices.Count -eq 1) { $target = $devices[0] }
elseif ($devices.Count -eq 0) { throw 'No authorized device. Enable USB debugging, connect the phone and approve its prompt.' }
else { throw 'Multiple devices found. Select one with -Serial.' }
$sdk = ((Invoke-Adb @('-s', $target, 'shell', 'getprop', 'ro.build.version.sdk')) -join '').Trim()
if ($sdk -notmatch '^\d+$' -or [int]$sdk -lt 30) { throw 'This APK requires Android 11 (API 30) or newer.' }
Invoke-Adb @('-s', $target, 'install', '-r', $ApkPath) | Write-Output
if ($EnableAccessibility) {
  Invoke-Adb @('-s', $target, 'shell', 'pm', 'grant', 'com.gosshd.desktop', 'android.permission.WRITE_SECURE_SETTINGS') | Write-Output
}
Invoke-Adb @('-s', $target, 'shell', 'am', 'start', '-n', 'com.gosshd.desktop/.ui.MainActivity') | Write-Output
Write-Output 'Enter the server HTTPS address and enrollment token in the app, then tap Save and start Agent. ADB can now be disconnected.'
if ($EnableAccessibility) { Write-Output 'Optional desktop control: tap Activate with ADB grant inside the app, then enable desktop control.' }
