//go:build windows

package agent

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func replaceAndRestart(currentExe, tmpPath string) error {
	target := stableExecutablePath(currentExe)
	if filepath.Dir(tmpPath) != filepath.Dir(target) || stableExecutablePath(tmpPath) != target || tmpPath == target {
		return fmt.Errorf("replacement is not an update file beside %s", target)
	}
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	// Older updaters launched an unmanaged temp binary from the service. Restore
	// that stopped service instead of perpetuating the unmanaged child chain.
	if !isService && currentExe != target {
		isService = legacyUpdateService(target)
	}
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	powershell := filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
	script := windowsUpdateScript(currentExe, tmpPath, os.Getpid(), os.Args[1:], isService)
	cmd := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func legacyUpdateService(target string) bool {
	manager, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer manager.Disconnect()
	service, err := manager.OpenService("gosshd-agent")
	if err != nil {
		return false
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil || status.State != svc.Stopped {
		return false
	}
	config, err := service.Config()
	if err != nil {
		return false
	}
	args, err := windows.DecomposeCommandLine(config.BinaryPathName)
	return err == nil && len(args) > 0 && strings.EqualFold(filepath.Clean(args[0]), target)
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func encodePowerShell(script string) string {
	encoded := utf16.Encode([]rune(script))
	bytes := make([]byte, len(encoded)*2)
	for i, value := range encoded {
		binary.LittleEndian.PutUint16(bytes[i*2:], value)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func windowsUpdateScript(currentExe, tmpPath string, parentPID int, args []string, isService bool) string {
	escapedArgs := make([]string, len(args))
	for i, arg := range args {
		escapedArgs[i] = windows.EscapeArg(arg)
	}
	service := "$false"
	if isService {
		service = "$true"
	}
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$target = %s
$replacement = %s
$parentPID = %d
$arguments = %s
$isService = %s
$parentExited = $false
$installed = $false
$errorLog = $target + '.update-error.log'
try {
    $parentProcess = Get-Process -Id $parentPID -ErrorAction SilentlyContinue
    if ($parentProcess -and -not $parentProcess.WaitForExit(60000)) {
        throw 'Timed out waiting for the old agent to exit'
    }
    $parentExited = $true
    Add-Type -TypeDefinition 'using System;
using System.Runtime.InteropServices;
public static class GosshdUpdate {
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool MoveFileEx(string source, string target, uint flags);
}'
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        if ([GosshdUpdate]::MoveFileEx($replacement, $target, 9)) {
            $installed = $true
            break
        }
        $moveError = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
        Start-Sleep -Milliseconds 500
    }
    if (-not $installed) { throw "Cannot replace agent executable (Win32 error $moveError)" }
    # Exact numeric update basenames only; no recursion, backups, or config deletion.
    $directory = [IO.Path]::GetDirectoryName($target)
    $pattern = '^' + [regex]::Escape([IO.Path]::GetFileName($target)) + '(?:\.new-[0-9]+)+$'
    Get-ChildItem -LiteralPath $directory -File | Where-Object { $_.Name -match $pattern } | ForEach-Object {
        if ([IO.Path]::GetDirectoryName($_.FullName) -eq $directory) {
            Remove-Item -LiteralPath $_.FullName -Force -ErrorAction Continue
        }
    }
    Remove-Item -LiteralPath $errorLog -Force -ErrorAction SilentlyContinue
} catch {
    # One overwritten diagnostic file, never a copy of the old executable.
    $_ | Out-String | Set-Content -LiteralPath $errorLog -Encoding UTF8 -ErrorAction SilentlyContinue
} finally {
    if (-not $installed) { Remove-Item -LiteralPath $replacement -Force -ErrorAction SilentlyContinue }
}
if ($parentExited) {
    try {
        if ($isService) {
            $service = Get-Service -Name 'gosshd-agent'
            if ($service.Status -eq 'StopPending') {
                $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
            }
            Start-Service -Name 'gosshd-agent'
            $service.WaitForStatus('Running', [TimeSpan]::FromSeconds(30))
        } else {
            $start = New-Object Diagnostics.ProcessStartInfo
            $start.FileName = $target
            $start.Arguments = $arguments
            $start.UseShellExecute = $false
            $start.CreateNoWindow = $true
            $child = [Diagnostics.Process]::Start($start)
            $child.Dispose()
        }
    } catch {
        $_ | Out-String | Set-Content -LiteralPath $errorLog -Encoding UTF8 -ErrorAction SilentlyContinue
    }
}
if (-not $installed) { exit 1 }
`, quotePowerShell(stableExecutablePath(currentExe)), quotePowerShell(tmpPath), parentPID, quotePowerShell(strings.Join(escapedArgs, " ")), service)
}
