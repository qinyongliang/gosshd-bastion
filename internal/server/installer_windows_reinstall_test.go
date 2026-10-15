package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsInstallerStopsAgentBeforeReplacingFiles(t *testing.T) {
	script := generatedHomeInstaller(t, true)
	previous := -1
	for _, step := range []string{
		"if ($isInstall) {",
		"  Assert-Administrator",
		`  $serviceName = "gosshd-agent"`,
		"  Stop-InstalledAgent -ServiceName $serviceName -BinaryPath $target",
		"  Copy-Item -Force $download $target",
		"  Install-WinPty -Destination $targetDir",
		`    Invoke-ScChecked -Action "delete existing $serviceName service"`,
		"  Invoke-ServiceCreateChecked -ServiceName $serviceName -BinaryPath $binPath",
		`  Invoke-ScChecked -Action "failed to start $serviceName service"`,
	} {
		index := strings.Index(script, step)
		if index < 0 || index <= previous {
			t.Fatalf("installer step missing or out of order: %s", step)
		}
		previous = index
	}
}

func TestWindowsInstallerStopInstalledAgent(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell execution")
	}
	script := generatedHomeInstaller(t, true)
	start := strings.Index(script, "function Wait-AgentServiceState {")
	if start < 0 {
		t.Fatal("Stop-InstalledAgent function missing")
	}
	end := strings.Index(script[start:], "\nfunction Invoke-ServiceCreateChecked")
	if end < 0 {
		t.Fatal("end of Stop-InstalledAgent function missing")
	}
	helper := script[start : start+end]
	for _, scenario := range []string{"fresh", "stopped", "running", "manual", "service-timeout", "process-timeout", "stop-error"} {
		t.Run(scenario, func(t *testing.T) {
			testScript := filepath.Join(t.TempDir(), "stop-agent.ps1")
			harness := `$ErrorActionPreference = 'Stop'
$scenario = $env:GOSSHD_TEST_STOP_SCENARIO
$events = [Collections.Generic.List[string]]::new()
$serviceStopped = $false
$serviceWaited = $false
function Invoke-ScChecked {
 param($Action, $Command)
 if ($Action -notlike 'stop existing*') { throw 'unexpected service operation' }
 $events.Add('service-stop')
 if ($scenario -eq 'stop-error') { throw 'service stop failed' }
 $script:serviceStopped = $true
}
function Start-Sleep {
 param($Milliseconds)
 if ($Milliseconds -ne 500) { throw 'unexpected service polling interval' }
}
function Get-CimInstance {
 param($ClassName, $Filter)
 if ($ClassName -eq 'Win32_Service') {
  if ($Filter -ne "Name='gosshd-agent'") { throw 'unexpected service' }
  if ($scenario -in @('fresh', 'manual', 'process-timeout')) { return }
  if ($serviceStopped -and -not $serviceWaited) { $events.Add('service-wait'); $script:serviceWaited = $true }
  $state = 'Running'
  if ($scenario -eq 'stopped' -or ($serviceStopped -and $scenario -ne 'service-timeout')) { $state = 'Stopped' }
  return [pscustomobject]@{ State = $state }
 }
 if ($ClassName -ne 'Win32_Process' -or $Filter -ne "Name='gosshd-agent.exe'") { throw 'unexpected process query' }
 # A same-named agent in another directory must never be stopped.
 [pscustomobject]@{ ExecutablePath = 'C:\other\gosshd-agent.exe'; ProcessId = 456 }
 [pscustomobject]@{ ExecutablePath = $null; ProcessId = 789 }
 if ($scenario -in @('manual', 'process-timeout', 'running')) {
  [pscustomobject]@{ ExecutablePath = 'c:\EXAMPLE\gosshd-agent.exe'; ProcessId = 123 }
 }
}
function Get-Process {
 param($Id, $ErrorAction)
 if ($Id -ne 123) { throw 'unrelated process selected' }
 $process = [pscustomobject]@{ Id = $Id }
 $process | Add-Member ScriptMethod WaitForExit {
  param($timeout)
  if ($timeout -ne 30000) { throw 'unexpected process wait' }
  $events.Add('process-wait')
  return ($scenario -ne 'process-timeout')
 }
 return $process
}
function Stop-Process {
 param($InputObject, [switch]$Force, $ErrorAction)
 if ($InputObject.Id -ne 123 -or -not $Force) { throw 'unexpected process stop' }
 $events.Add('process-stop')
}
` + helper + `
$failure = $null
try {
 Stop-InstalledAgent -ServiceName 'gosshd-agent' -BinaryPath 'C:\example\gosshd-agent.exe'
 $events.Add('copy')
} catch {
 $failure = $_.Exception.Message
}
$expectedFailure = $null
switch ($scenario) {
 'fresh' { $expected = 'copy' }
 'stopped' { $expected = 'copy' }
 'running' { $expected = 'service-stop,service-wait,process-stop,process-wait,copy' }
 'manual' { $expected = 'process-stop,process-wait,copy' }
 'service-timeout' { $expected = 'service-stop,service-wait'; $expectedFailure = 'Timed out waiting for gosshd-agent service to reach Stopped' }
 'process-timeout' { $expected = 'process-stop,process-wait'; $expectedFailure = 'Timed out waiting for the old agent process to exit' }
 'stop-error' { $expected = 'service-stop'; $expectedFailure = 'service stop failed' }
}
if (($events -join ',') -ne $expected) { throw "unexpected order: $($events -join ','); expected $expected" }
if ($expectedFailure) {
 if (-not $failure -or -not $failure.Contains($expectedFailure)) { throw "unexpected failure: $failure" }
} elseif ($failure) { throw $failure }
`
			if err := os.WriteFile(testScript, []byte(harness), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOSSHD_TEST_STOP_SCENARIO", scenario)
			command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", testScript)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("stop installed agent: %v\n%s", err, output)
			}
		})
	}
}

func TestWindowsInstallerForegroundStagesBeforeStopping(t *testing.T) {
	script := generatedHomeInstaller(t, true)
	previous := -1
	for _, step := range []string{
		`Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $download`,
		`(Get-FileHash -Algorithm SHA256 -Path $download)`,
		`Stop-AgentProcesses -BinaryPath $tmp`,
		`Copy-Item -Force $download $tmp`,
		`& $tmp --server`,
	} {
		index := strings.Index(script, step)
		if index < 0 || index <= previous {
			t.Fatalf("foreground installer step missing or out of order: %s", step)
		}
		previous = index
	}
	if !strings.Contains(script, `Wait-AgentServiceState -ServiceName $serviceName -State "Running"`) {
		t.Fatal("service startup must be checked")
	}
}

func TestWindowsInstallerBadChecksumPreservesForegroundBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell execution")
	}
	root := t.TempDir()
	installer := filepath.Join(root, "installer.ps1")
	if err := os.WriteFile(installer, []byte(generatedHomeInstaller(t, true)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSSHD_TEST_INSTALL_SCRIPT", installer)
	t.Setenv("GOSSHD_TEST_INSTALL_TEMP", root)
	harness := `$ErrorActionPreference = 'Stop'
Import-Module Microsoft.PowerShell.Utility
$env:TEMP = $env:GOSSHD_TEST_INSTALL_TEMP
$original = Join-Path $env:TEMP 'gosshd-agent\gosshd-agent.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $original) | Out-Null
[IO.File]::WriteAllText($original, 'old-binary')
function Invoke-WebRequest {
 param([switch]$UseBasicParsing, $Uri, $OutFile)
 if ($Uri.EndsWith('.sha256')) { return [pscustomobject]@{ Content = ('0' * 64) } }
 [IO.File]::WriteAllText($OutFile, 'unverified-binary')
}
function Get-CimInstance { throw 'must not inspect or stop processes before checksum verification' }
$failure = $null
try { & $env:GOSSHD_TEST_INSTALL_SCRIPT } catch { $failure = $_.Exception.Message }
if (-not $failure -or -not $failure.Contains('sha256 mismatch')) { throw "unexpected failure: $failure" }
if ([IO.File]::ReadAllText($original) -ne 'old-binary') { throw 'unverified download replaced the original' }
if (Get-ChildItem -LiteralPath (Split-Path $original) -Filter 'gosshd-agent-download-*.exe') { throw 'staging files leaked' }
`
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", harness)
	// Go may inherit PowerShell 7's PSModulePath. Load Windows PowerShell's own
	// built-in modules instead of incompatible modules from the parent runtime.
	command.Env = append(os.Environ(), "PSModulePath="+filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "Modules"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("checksum safety: %v\n%s", err, output)
	}
}
