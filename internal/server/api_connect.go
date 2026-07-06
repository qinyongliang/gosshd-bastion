package server

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

const webConsolePublicKeyName = "Web console"
const webFileEditorMaxBytes = 2 << 20

type terminalWSMessage struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Code      int    `json:"code,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

type terminalWSWriter struct {
	mu sync.Mutex
	ws *websocket.Conn
}

type apiTargetSystemUsage struct {
	UsedBytes  int64   `json:"used_bytes"`
	TotalBytes int64   `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

type apiTargetSystemProcess struct {
	RSSBytes   int64   `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
	Command    string  `json:"command"`
}

type apiTargetSystemNetwork struct {
	Interface string `json:"interface"`
	RXBytes   int64  `json:"rx_bytes"`
	TXBytes   int64  `json:"tx_bytes"`
}

type apiTargetSystemFilesystem struct {
	Path       string  `json:"path"`
	UsedBytes  int64   `json:"used_bytes"`
	TotalBytes int64   `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

type apiTargetSystemResponse struct {
	OS          string                      `json:"os,omitempty"`
	Hostname    string                      `json:"hostname,omitempty"`
	IP          string                      `json:"ip,omitempty"`
	PublicIP    string                      `json:"public_ip,omitempty"`
	PublicIPv4  string                      `json:"public_ipv4,omitempty"`
	PublicIPv6  string                      `json:"public_ipv6,omitempty"`
	Uptime      string                      `json:"uptime,omitempty"`
	Load        string                      `json:"load,omitempty"`
	CPUPercent  float64                     `json:"cpu_percent"`
	Memory      apiTargetSystemUsage        `json:"memory"`
	Swap        apiTargetSystemUsage        `json:"swap"`
	Processes   []apiTargetSystemProcess    `json:"processes"`
	Network     []apiTargetSystemNetwork    `json:"network"`
	Filesystems []apiTargetSystemFilesystem `json:"filesystems"`
	CollectedAt string                      `json:"collected_at,omitempty"`
}

func (w *terminalWSWriter) write(msg terminalWSMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ws.WriteJSON(msg)
}

func (a *App) handleTargetSystem(w http.ResponseWriter, r *http.Request, user store.User) {
	target, err := a.targetForUser(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	var snapshot apiTargetSystemResponse
	switch strings.TrimSpace(r.URL.Query().Get("scope")) {
	case "", "full":
		snapshot, err = a.collectTargetSystem(ctx, target)
	case "metrics":
		snapshot, err = a.collectTargetSystemMetrics(ctx, target)
	case "filesystems":
		snapshot, err = a.collectTargetSystemFilesystems(ctx, target)
	default:
		writeError(w, http.StatusBadRequest, "invalid system scope")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (a *App) collectTargetSystem(ctx context.Context, target store.SSHTarget) (apiTargetSystemResponse, error) {
	return a.collectTargetSystemWithCommands(ctx, target, linuxSystemProbeCommand, windowsSystemProbeCommand(), "collect system metrics")
}

func (a *App) collectTargetSystemMetrics(ctx context.Context, target store.SSHTarget) (apiTargetSystemResponse, error) {
	return a.collectTargetSystemWithCommands(ctx, target, linuxSystemMetricsCommand, windowsSystemMetricsCommand(), "collect system metrics")
}

func (a *App) collectTargetSystemFilesystems(ctx context.Context, target store.SSHTarget) (apiTargetSystemResponse, error) {
	return a.collectTargetSystemWithCommands(ctx, target, linuxSystemFilesystemsCommand, windowsSystemFilesystemsCommand(), "collect system filesystems")
}

func (a *App) collectTargetSystemWithCommands(ctx context.Context, target store.SSHTarget, linuxCommand, windowsCommand, label string) (apiTargetSystemResponse, error) {
	linuxOut, linuxErr := a.runTargetSystemCommand(ctx, target, linuxCommand)
	if snapshot, ok := parseTargetSystemProbe(linuxOut); ok && snapshot.OS != "windows" {
		return snapshot, nil
	}
	winOut, winErr := a.runTargetSystemCommand(ctx, target, windowsCommand)
	if snapshot, ok := parseTargetSystemProbe(winOut); ok {
		return snapshot, nil
	}
	if winErr != nil {
		return apiTargetSystemResponse{}, fmt.Errorf("%s: %v", label, winErr)
	}
	if linuxErr != nil {
		return apiTargetSystemResponse{}, fmt.Errorf("%s: %v", label, linuxErr)
	}
	return apiTargetSystemResponse{}, fmt.Errorf("%s: unsupported probe output", label)
}

func (a *App) runTargetSystemCommand(ctx context.Context, target store.SSHTarget, command string) (string, error) {
	if target.TargetType == store.TargetAgent {
		return a.runAgentSystemCommand(ctx, target.AgentID, command)
	}
	client, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
			_ = client.Close()
		case <-done:
		}
	}()
	out, err := session.CombinedOutput(command)
	close(done)
	if ctx.Err() != nil {
		return string(out), ctx.Err()
	}
	if len(out) > 256*1024 {
		out = out[:256*1024]
	}
	return string(out), err
}

func (a *App) runAgentSystemCommand(ctx context.Context, agentID, command string) (string, error) {
	reader, stream, err := a.openAgentStream(agentID, protocol.StreamRequest{Type: protocol.StreamExec, Command: command, Width: 100, Height: 24})
	if err != nil {
		return "", err
	}
	defer stream.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-done:
		}
	}()
	var builder strings.Builder
	exitCode := 255
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			close(done)
			if ctx.Err() != nil {
				return builder.String(), ctx.Err()
			}
			return builder.String(), err
		}
		switch frame.Type {
		case protocol.FrameStdout, protocol.FrameStderr:
			if builder.Len() < 256*1024 {
				remaining := 256*1024 - builder.Len()
				if len(frame.Data) > remaining {
					frame.Data = frame.Data[:remaining]
				}
				builder.Write(frame.Data)
			}
		case protocol.FrameExit:
			close(done)
			exitCode = protocol.ExitCode(frame)
			if exitCode != 0 {
				return builder.String(), fmt.Errorf("agent command exited %d", exitCode)
			}
			return builder.String(), nil
		}
	}
}

const linuxSystemProbeCommand = `printf '__GOSSHD_SYSTEM_V1__\n'
printf 'os=linux\n'
printf 'hostname=%s\n' "$(hostname 2>/dev/null || uname -n 2>/dev/null)"
ipaddr="$(hostname -I 2>/dev/null | awk '{print $1}')"
if [ -z "$ipaddr" ]; then ipaddr="$(ip -4 addr show scope global 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2; exit}')"; fi
printf 'ip=%s\n' "$ipaddr"
public_ipv4="$(curl -4 -A curl/8.0.0 -fsS --max-time 3 https://ifconfig.me/ip 2>/dev/null || curl -4 -A curl/8.0.0 -fsS --max-time 3 https://api.ipify.org 2>/dev/null)"
public_ipv4="$(printf '%s' "$public_ipv4" | tr -d '\r\n' | awk '{print $1}')"
public_ipv6="$(curl -6 -A curl/8.0.0 -fsS --max-time 3 https://ifconfig.me/ip 2>/dev/null || curl -6 -A curl/8.0.0 -fsS --max-time 3 https://api64.ipify.org 2>/dev/null)"
public_ipv6="$(printf '%s' "$public_ipv6" | tr -d '\r\n' | awk '{print $1}')"
printf 'public_ipv4=%s\n' "$public_ipv4"
printf 'public_ipv6=%s\n' "$public_ipv6"
printf 'uptime=%s\n' "$(uptime -p 2>/dev/null || uptime 2>/dev/null)"
printf 'load=%s\n' "$(awk '{print $1 ", " $2 ", " $3}' /proc/loadavg 2>/dev/null)"
awk '/^cpu /{print "cpu1="$0}' /proc/stat 2>/dev/null
sleep 1
awk '/^cpu /{print "cpu2="$0}' /proc/stat 2>/dev/null
awk '/MemTotal/{mt=$2}/MemAvailable/{ma=$2}/MemFree/{mf=$2}/SwapTotal/{st=$2}/SwapFree/{sf=$2} END{if(ma==0)ma=mf; printf "memory_kb=%d %d\nswap_kb=%d %d\n", mt, mt-ma, st, st-sf}' /proc/meminfo 2>/dev/null
ps -eo rss,pcpu,comm --sort=-rss 2>/dev/null | awk 'NR>1 && NR<=6{printf "process=%s|%s|%s\n",$1,$2,$3}'
awk 'NR>2{line=$0; sub(/^[[:space:]]*/,"",line); split(line, p, ":"); name=p[1]; if(name=="lo" || name=="") next; split(p[2], v, /[[:space:]]+/); printf "net=%s|%s|%s\n", name, v[2], v[10]}' /proc/net/dev 2>/dev/null | head -4
df -Pk 2>/dev/null | awk 'NR>1 && NR<=24{gsub("%","",$5); printf "disk=%s|%s|%s|%s\n",$6,$3,$2,$5}'`

const linuxSystemMetricsCommand = `printf '__GOSSHD_SYSTEM_V1__\n'
printf 'uptime=%s\n' "$(uptime -p 2>/dev/null || uptime 2>/dev/null)"
printf 'load=%s\n' "$(awk '{print $1 ", " $2 ", " $3}' /proc/loadavg 2>/dev/null)"
awk '/^cpu /{print "cpu1="$0}' /proc/stat 2>/dev/null
sleep 1
awk '/^cpu /{print "cpu2="$0}' /proc/stat 2>/dev/null
awk '/MemTotal/{mt=$2}/MemAvailable/{ma=$2}/MemFree/{mf=$2}/SwapTotal/{st=$2}/SwapFree/{sf=$2} END{if(ma==0)ma=mf; printf "memory_kb=%d %d\nswap_kb=%d %d\n", mt, mt-ma, st, st-sf}' /proc/meminfo 2>/dev/null
ps -eo rss,pcpu,comm --sort=-rss 2>/dev/null | awk 'NR>1 && NR<=6{printf "process=%s|%s|%s\n",$1,$2,$3}'
awk 'NR>2{line=$0; sub(/^[[:space:]]*/,"",line); split(line, p, ":"); name=p[1]; if(name=="lo" || name=="") next; split(p[2], v, /[[:space:]]+/); printf "net=%s|%s|%s\n", name, v[2], v[10]}' /proc/net/dev 2>/dev/null | head -4`

const linuxSystemFilesystemsCommand = `printf '__GOSSHD_SYSTEM_V1__\n'
printf 'os=linux\n'
df -Pk 2>/dev/null | awk 'NR>1 && NR<=24{gsub("%","",$5); printf "disk=%s|%s|%s|%s\n",$6,$3,$2,$5}'`

func windowsSystemProbeCommand() string {
	script := `$ci = [System.Globalization.CultureInfo]::InvariantCulture
$ErrorActionPreference = "SilentlyContinue"
Write-Output "__GOSSHD_SYSTEM_V1__"
Write-Output "os=windows"
Write-Output ("hostname={0}" -f $env:COMPUTERNAME)
$ips = Get-CimInstance Win32_NetworkAdapterConfiguration | Where-Object { $_.IPEnabled -and $_.IPAddress } | Select-Object -First 1
if ($ips) { Write-Output ("ip={0}" -f ($ips.IPAddress | Where-Object { $_ -match '^\d+\.\d+\.\d+\.\d+$' } | Select-Object -First 1)) }
$publicIPv4 = (& curl.exe -4 -A curl/8.0.0 -fsS --max-time 3 https://ifconfig.me/ip) -join ""
if (-not $publicIPv4) { $publicIPv4 = (& curl.exe -4 -A curl/8.0.0 -fsS --max-time 3 https://api.ipify.org) -join "" }
if (-not $publicIPv4) { $publicIPv4 = Invoke-RestMethod -Uri "https://ifconfig.me/ip" -Headers @{ "User-Agent" = "curl/8.0.0"; "Accept" = "*/*" } -TimeoutSec 3 }
$publicIPv6 = (& curl.exe -6 -A curl/8.0.0 -fsS --max-time 3 https://ifconfig.me/ip) -join ""
if (-not $publicIPv6) { $publicIPv6 = (& curl.exe -6 -A curl/8.0.0 -fsS --max-time 3 https://api64.ipify.org) -join "" }
if ($publicIPv4) { Write-Output ("public_ipv4={0}" -f (($publicIPv4 -join "").Trim())) }
if ($publicIPv6) { Write-Output ("public_ipv6={0}" -f (($publicIPv6 -join "").Trim())) }
$os = Get-CimInstance Win32_OperatingSystem
if ($os) {
  $uptime = (Get-Date) - $os.LastBootUpTime
  Write-Output ("uptime={0} days {1:hh\:mm}" -f [int]$uptime.TotalDays, $uptime)
  $totalKB=[int64]$os.TotalVisibleMemorySize; $freeKB=[int64]$os.FreePhysicalMemory
  Write-Output ("memory_kb={0} {1}" -f $totalKB, ($totalKB-$freeKB))
  $totalVirtual=[int64]$os.TotalVirtualMemorySize; $freeVirtual=[int64]$os.FreeVirtualMemory
  Write-Output ("swap_kb={0} {1}" -f $totalVirtual, ($totalVirtual-$freeVirtual))
}
$cpu = Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average
if ($cpu) { Write-Output ("cpu_percent={0}" -f ([math]::Round([double]$cpu.Average, 1)).ToString($ci)) }
Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 5 | ForEach-Object { Write-Output ("process={0}|{1}|{2}" -f [int64]($_.WorkingSet64/1KB), 0, $_.ProcessName) }
Get-NetAdapterStatistics | Select-Object -First 4 | ForEach-Object { Write-Output ("net={0}|{1}|{2}" -f $_.Name, [int64]$_.ReceivedBytes, [int64]$_.SentBytes) }
Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | ForEach-Object { $total=[int64]$_.Size; $free=[int64]$_.FreeSpace; if($total -gt 0){ $pct = [math]::Round(((($total-$free)/$total)*100), 0); Write-Output ("disk={0}|{1}|{2}|{3}" -f $_.DeviceID, [int64](($total-$free)/1KB), [int64]($total/1KB), $pct.ToString($ci)) } }`
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + utf16LEBase64(script)
}

func windowsSystemMetricsCommand() string {
	script := `$ci = [System.Globalization.CultureInfo]::InvariantCulture
$ErrorActionPreference = "SilentlyContinue"
Write-Output "__GOSSHD_SYSTEM_V1__"
$os = Get-CimInstance Win32_OperatingSystem
if ($os) {
  $uptime = (Get-Date) - $os.LastBootUpTime
  Write-Output ("uptime={0} days {1:hh\:mm}" -f [int]$uptime.TotalDays, $uptime)
  $totalKB=[int64]$os.TotalVisibleMemorySize; $freeKB=[int64]$os.FreePhysicalMemory
  Write-Output ("memory_kb={0} {1}" -f $totalKB, ($totalKB-$freeKB))
  $totalVirtual=[int64]$os.TotalVirtualMemorySize; $freeVirtual=[int64]$os.FreeVirtualMemory
  Write-Output ("swap_kb={0} {1}" -f $totalVirtual, ($totalVirtual-$freeVirtual))
}
$cpu = Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average
if ($cpu) { Write-Output ("cpu_percent={0}" -f ([math]::Round([double]$cpu.Average, 1)).ToString($ci)) }
Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 5 | ForEach-Object { Write-Output ("process={0}|{1}|{2}" -f [int64]($_.WorkingSet64/1KB), 0, $_.ProcessName) }
Get-NetAdapterStatistics | Select-Object -First 4 | ForEach-Object { Write-Output ("net={0}|{1}|{2}" -f $_.Name, [int64]$_.ReceivedBytes, [int64]$_.SentBytes) }`
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + utf16LEBase64(script)
}

func windowsSystemFilesystemsCommand() string {
	script := `$ci = [System.Globalization.CultureInfo]::InvariantCulture
$ErrorActionPreference = "SilentlyContinue"
Write-Output "__GOSSHD_SYSTEM_V1__"
Write-Output "os=windows"
Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | ForEach-Object { $total=[int64]$_.Size; $free=[int64]$_.FreeSpace; if($total -gt 0){ $pct = [math]::Round(((($total-$free)/$total)*100), 0); Write-Output ("disk={0}|{1}|{2}|{3}" -f $_.DeviceID, [int64](($total-$free)/1KB), [int64]($total/1KB), $pct.ToString($ci)) } }`
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + utf16LEBase64(script)
}

func utf16LEBase64(script string) string {
	encoded := utf16.Encode([]rune(script))
	data := make([]byte, len(encoded)*2)
	for i, value := range encoded {
		binary.LittleEndian.PutUint16(data[i*2:], value)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func parseTargetSystemProbe(out string) (apiTargetSystemResponse, bool) {
	if !strings.Contains(out, "__GOSSHD_SYSTEM_V1__") {
		return apiTargetSystemResponse{}, false
	}
	snapshot := apiTargetSystemResponse{CollectedAt: time.Now().UTC().Format(time.RFC3339)}
	var cpuStart, cpuEnd string
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line == "__GOSSHD_SYSTEM_V1__" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "os":
			snapshot.OS = value
		case "hostname":
			snapshot.Hostname = value
		case "ip":
			snapshot.IP = value
		case "public_ip":
			snapshot.PublicIP = normalizePublicIP(value)
			if isIPv4Literal(snapshot.PublicIP) {
				snapshot.PublicIPv4 = snapshot.PublicIP
			} else if isIPv6Literal(snapshot.PublicIP) {
				snapshot.PublicIPv6 = snapshot.PublicIP
			}
		case "public_ipv4":
			snapshot.PublicIPv4 = normalizePublicIPv4(value)
		case "public_ipv6":
			snapshot.PublicIPv6 = normalizePublicIPv6(value)
		case "uptime":
			snapshot.Uptime = value
		case "load":
			snapshot.Load = value
		case "cpu_percent":
			snapshot.CPUPercent = parseFloat(value)
		case "cpu1":
			cpuStart = value
		case "cpu2":
			cpuEnd = value
		case "memory_kb":
			snapshot.Memory = parseUsageKB(value)
		case "swap_kb":
			snapshot.Swap = parseUsageKB(value)
		case "process":
			if item, ok := parseSystemProcess(value); ok {
				snapshot.Processes = append(snapshot.Processes, item)
			}
		case "net":
			if item, ok := parseSystemNetwork(value); ok {
				snapshot.Network = append(snapshot.Network, item)
			}
		case "disk":
			if item, ok := parseSystemFilesystem(value); ok {
				snapshot.Filesystems = append(snapshot.Filesystems, item)
			}
		}
	}
	if snapshot.CPUPercent == 0 && cpuStart != "" && cpuEnd != "" {
		snapshot.CPUPercent = parseCPUPercent(cpuStart, cpuEnd)
	}
	if snapshot.OS == "" {
		snapshot.OS = "unknown"
	}
	if snapshot.PublicIP == "" {
		if snapshot.PublicIPv4 != "" {
			snapshot.PublicIP = snapshot.PublicIPv4
		} else {
			snapshot.PublicIP = snapshot.PublicIPv6
		}
	}
	return snapshot, true
}

func parseUsageKB(value string) apiTargetSystemUsage {
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return apiTargetSystemUsage{}
	}
	total := parseInt64(parts[0]) * 1024
	used := parseInt64(parts[1]) * 1024
	return apiTargetSystemUsage{UsedBytes: used, TotalBytes: total, Percent: percent(used, total)}
}

func parseSystemProcess(value string) (apiTargetSystemProcess, bool) {
	parts := strings.SplitN(value, "|", 3)
	if len(parts) != 3 {
		return apiTargetSystemProcess{}, false
	}
	command := strings.TrimSpace(parts[2])
	if command == "" {
		return apiTargetSystemProcess{}, false
	}
	return apiTargetSystemProcess{
		RSSBytes:   parseInt64(parts[0]) * 1024,
		CPUPercent: parseFloat(parts[1]),
		Command:    command,
	}, true
}

func parseSystemNetwork(value string) (apiTargetSystemNetwork, bool) {
	parts := strings.SplitN(value, "|", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" {
		return apiTargetSystemNetwork{}, false
	}
	return apiTargetSystemNetwork{
		Interface: strings.TrimSpace(parts[0]),
		RXBytes:   parseInt64(parts[1]),
		TXBytes:   parseInt64(parts[2]),
	}, true
}

func parseSystemFilesystem(value string) (apiTargetSystemFilesystem, bool) {
	parts := strings.SplitN(value, "|", 4)
	if len(parts) != 4 || strings.TrimSpace(parts[0]) == "" {
		return apiTargetSystemFilesystem{}, false
	}
	used := parseInt64(parts[1]) * 1024
	total := parseInt64(parts[2]) * 1024
	return apiTargetSystemFilesystem{
		Path:       strings.TrimSpace(parts[0]),
		UsedBytes:  used,
		TotalBytes: total,
		Percent:    firstPositive(parseFloat(parts[3]), percent(used, total)),
	}, true
}

func normalizePublicIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "<") || strings.Contains(value, ">") {
		return ""
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	candidate := strings.Trim(fields[0], " \t\r\n,;")
	if isIPv4Literal(candidate) || isIPv6Literal(candidate) {
		return candidate
	}
	return ""
}

func normalizePublicIPv4(value string) string {
	value = normalizePublicIP(value)
	if isIPv4Literal(value) {
		return value
	}
	return ""
}

func normalizePublicIPv6(value string) string {
	value = normalizePublicIP(value)
	if isIPv6Literal(value) {
		return value
	}
	return ""
}

func isIPv4Literal(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

func isIPv6Literal(value string) bool {
	if !strings.Contains(value, ":") {
		return false
	}
	for _, r := range value {
		if !(r == ':' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func parseCPUPercent(start, end string) float64 {
	startTotal, startIdle := parseCPULine(start)
	endTotal, endIdle := parseCPULine(end)
	totalDelta := endTotal - startTotal
	idleDelta := endIdle - startIdle
	if totalDelta <= 0 {
		return 0
	}
	return clampPercent(float64(totalDelta-idleDelta) * 100 / float64(totalDelta))
}

func parseCPULine(line string) (int64, int64) {
	fields := strings.Fields(line)
	var total int64
	var idle int64
	for i, field := range fields {
		if i == 0 && field == "cpu" {
			continue
		}
		value := parseInt64(field)
		total += value
		if i == 4 || i == 5 {
			idle += value
		}
	}
	return total, idle
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(value, ",", "")), 64)
	return clampPercent(parsed)
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(strings.ReplaceAll(value, ",", "")), 10, 64)
	return parsed
}

func percent(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return clampPercent(float64(used) * 100 / float64(total))
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (a *App) handleTargetTerminalWS(w http.ResponseWriter, r *http.Request, user store.User) {
	ctx := r.Context()
	target, err := a.targetForUser(ctx, r.PathValue("id"), user)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	sourceIP := sshSourceIPFromRequest(r)
	decision, err := a.bastion.EvaluateAccess(ctx, user.ID, target.ID, store.RequestShell, sourceIP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if decision.Action == store.DecisionDeny {
		code := 126
		now := time.Now().UTC()
		_, _ = a.createAuditLog(context.Background(), store.CreateCommandAuditLogParams{
			UserID:         user.ID,
			TargetID:       target.ID,
			OrganizationID: organizationIDForTarget(target),
			PublicKeyName:  webConsolePublicKeyName,
			SessionID:      newAuditSessionID(),
			Command:        "web terminal",
			RequestType:    store.RequestShell,
			PolicyDecision: store.DecisionDeny,
			PolicyReason:   decision.Reason,
			ExitCode:       &code,
			StartedAt:      now,
			EndedAt:        &now,
			RemoteAddress:  sourceIP,
		})
		writeError(w, http.StatusForbidden, "interactive terminal denied: "+decision.Reason)
		return
	}

	cols, rows := terminalSizeFromQuery(r)
	sessionID := newAuditSessionID()
	startedAt := time.Now().UTC()
	recorder, err := newTerminalRecorder(a.auditRecordingsPath, sessionID, cols, rows, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "terminal recording unavailable: "+err.Error())
		return
	}
	auditLog, err := a.createAuditLog(context.Background(), store.CreateCommandAuditLogParams{
		UserID:         user.ID,
		TargetID:       target.ID,
		OrganizationID: organizationIDForTarget(target),
		PublicKeyName:  webConsolePublicKeyName,
		SessionID:      sessionID,
		Command:        "web terminal",
		RequestType:    store.RequestShell,
		PolicyDecision: decision.Action,
		PolicyReason:   decision.Reason,
		StartedAt:      startedAt,
		RemoteAddress:  sourceIP,
	})
	if err != nil {
		_, _ = recorder.Close()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		a.completeShellAuditAsync(recorder, auditLog.ID, 255, time.Now().UTC())
		return
	}
	defer ws.Close()
	writer := &terminalWSWriter{ws: ws}
	session := a.terminalSessions.create(sessionID, user.ID, target, sourceIP, cols, rows, recorder)
	session.startedAt = startedAt
	session.auditLogID = auditLog.ID
	log.Printf("web terminal session created: user=%s target=%s alias=%s session=%s type=%s source=%s clients=1", user.ID, target.ID, target.Alias, session.id, target.TargetType, sourceIP)
	session.attach(writer)
	_ = writer.write(terminalWSMessage{Type: "session", SessionID: session.id})
	defer func() {
		session.detach(writer)
		log.Printf("web terminal websocket detached: user=%s target=%s alias=%s session=%s", user.ID, target.ID, target.Alias, session.id)
	}()
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		exitCode := a.webTerminalOnTarget(session)
		endedAt := time.Now().UTC()
		log.Printf("web terminal session ended: user=%s target=%s alias=%s session=%s exit=%d", user.ID, target.ID, target.Alias, session.id, exitCode)
		session.close("")
		a.terminalSessions.remove(session.id)
		a.completeShellAuditAsync(recorder, auditLog.ID, exitCode, endedAt)
		session.mu.Lock()
		session.broadcastLocked(terminalWSMessage{Type: "exit", Code: exitCode})
		session.mu.Unlock()
	}()
	for {
		var msg terminalWSMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "input":
			_ = session.writeInput(msg.Data)
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				session.resizeTo(msg.Cols, msg.Rows)
			}
		case "heartbeat":
			session.heartbeat()
		case "ai-collaboration":
			if msg.Enabled != nil {
				session.setAICollaborationEnabled(*msg.Enabled)
			}
		case "close":
			session.close("browser closed")
			return
		}
	}
}

func (a *App) webTerminalOnTarget(session *terminalSession) int {
	if session.target.TargetType == store.TargetAgent {
		return a.webAgentTerminal(session)
	}
	return a.webDirectTerminal(session)
}

func (a *App) webDirectTerminal(terminalSession *terminalSession) int {
	client, err := a.openTargetSSHClient(terminalSession.ctx, terminalSession.target)
	if err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	if err := session.RequestPty("xterm-256color", terminalSession.rows, terminalSession.cols, gossh.TerminalModes{gossh.ECHO: 0}); err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	if err := session.Shell(); err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	if _, err := io.WriteString(stdin, bashShellIntegrationCommand()+"\n"); err != nil {
		terminalSession.writeOutput("error", []byte(err.Error()))
		return 255
	}
	terminalSession.setDirectInput(stdin, session)

	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go copyReaderToTerminalSession(&outputWG, terminalSession, "output", stdout)
	go copyReaderToTerminalSession(&outputWG, terminalSession, "error", stderr)
	err = session.Wait()
	_ = closeWriter(stdin)
	outputWG.Wait()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*gossh.ExitError); ok {
		return exit.ExitStatus()
	}
	return 255
}

func (a *App) webAgentTerminal(session *terminalSession) int {
	reader, stream, err := a.openAgentStream(session.target.AgentID, protocol.StreamRequest{Type: protocol.StreamShell, Width: session.cols, Height: session.rows})
	if err != nil {
		session.writeOutput("error", []byte(err.Error()))
		return 255
	}
	defer stream.Close()
	session.setAgentInput(stream)
	exitCode := 255
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			break
		}
		switch frame.Type {
		case protocol.FrameStdout:
			session.writeOutput("output", frame.Data)
		case protocol.FrameStderr:
			session.writeOutput("error", frame.Data)
		case protocol.FrameExit:
			exitCode = protocol.ExitCode(frame)
			return exitCode
		}
	}
	return exitCode
}

func copyReaderToTerminalSession(wg *sync.WaitGroup, session *terminalSession, typ string, reader io.Reader) {
	defer wg.Done()
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			session.writeOutput(typ, chunk)
		}
		if err != nil {
			return
		}
	}
}

type apiTargetFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

type apiTargetFilesResponse struct {
	Path    string               `json:"path"`
	Entries []apiTargetFileEntry `json:"entries"`
}

type apiTargetFileStatResponse struct {
	apiTargetFileEntry
	DiskUsage int64 `json:"disk_usage"`
	Items     int64 `json:"items"`
}

type apiTargetFileReadResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Modified string `json:"modified_at,omitempty"`
}

func (a *App) handleTargetFiles(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "list")
	if !ok {
		return
	}
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp list "+remotePathFromQuery(r), store.DecisionDeny, "download/list is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP list is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	dir := remotePathFromQuery(r)
	infos, err := client.ReadDir(dir)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp list "+dir, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := apiTargetFilesResponse{Path: dir}
	for _, info := range infos {
		entryPath := remoteJoin(dir, info.Name())
		var targetInfo fs.FileInfo
		if info.Mode()&fs.ModeSymlink != 0 {
			if stat, err := client.Stat(entryPath); err == nil {
				targetInfo = stat
			}
		}
		out.Entries = append(out.Entries, apiFileEntry(dir, info, targetInfo))
	}
	sortFileEntries(out.Entries, r.URL.Query().Get("sort"), r.URL.Query().Get("order"))
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp list "+dir, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleTargetFileDownload(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "download")
	if !ok {
		return
	}
	filePath := remotePathFromQuery(r)
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp download "+filePath, store.DecisionDeny, "download is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP download is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	file, err := client.Open(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp download "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+downloadName(filePath)+`"`)
	if _, err := io.Copy(w, file); err != nil {
		a.auditWebSFTP(context.Background(), user, target, decision, "sftp download "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		return
	}
	a.auditWebSFTP(context.Background(), user, target, decision, "sftp download "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
}

func (a *App) handleTargetFileOpen(w http.ResponseWriter, r *http.Request, user store.User) {
	if !a.cfg.ClientMode || !isLoopbackRequest(r) {
		writeError(w, http.StatusBadRequest, "native file open is only available in the local client")
		return
	}
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "open")
	if !ok {
		return
	}
	filePath := remotePathFromQuery(r)
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp open "+filePath, store.DecisionDeny, "download/open is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP open is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	info, err := client.Stat(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp open "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "cannot open a directory with a native app")
		return
	}
	file, err := client.Open(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp open "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer file.Close()
	localPath, err := copyRemoteFileToOpenTemp(file, downloadName(filePath))
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp open "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := openLocalFile(localPath); err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp open "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp open "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, map[string]any{"path": localPath})
}

func (a *App) handleTargetFileUpload(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowUpload, _, ok := a.authorizeTargetSFTP(w, r, user, "upload")
	if !ok {
		return
	}
	dir := remotePathFromQuery(r)
	if !allowUpload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+dir, store.DecisionDeny, "upload is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP upload is not allowed by policy")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	name := pathpkg.Base(header.Filename)
	if name == "." || name == "/" || name == "" {
		writeError(w, http.StatusBadRequest, "invalid file name")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	destPath := remoteJoin(dir, name)
	dst, err := client.Create(destPath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+destPath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	written, copyErr := io.Copy(dst, file)
	closeErr := dst.Close()
	if copyErr != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+destPath, decision.Action, copyErr.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, copyErr.Error())
		return
	}
	if closeErr != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp upload "+destPath, decision.Action, closeErr.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, closeErr.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, fmt.Sprintf("sftp upload %s (%d bytes)", destPath, written), decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusCreated, map[string]any{"path": destPath, "size": written})
}

func (a *App) handleTargetFileRead(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "read")
	if !ok {
		return
	}
	filePath := remotePathFromQuery(r)
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp read "+filePath, store.DecisionDeny, "download/read is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP read is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	info, err := client.Stat(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp read "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "cannot edit a directory")
		return
	}
	if info.Size() > webFileEditorMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large for web editor")
		return
	}
	file, err := client.Open(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp read "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, webFileEditorMaxBytes+1))
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp read "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(content) > webFileEditorMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large for web editor")
		return
	}
	if !utf8.Valid(content) {
		writeError(w, http.StatusUnsupportedMediaType, "file is not valid UTF-8 text")
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp read "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, apiTargetFileReadResponse{Path: filePath, Content: string(content), Modified: info.ModTime().UTC().Format(time.RFC3339)})
}

func (a *App) handleTargetFileWrite(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowUpload, _, ok := a.authorizeTargetSFTP(w, r, user, "write")
	if !ok {
		return
	}
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	filePath := cleanRemoteBodyPath(body.Path)
	if filePath == "" || filePath == "." || filePath == "/" {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if !allowUpload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp write "+filePath, store.DecisionDeny, "upload/write is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP write is not allowed by policy")
		return
	}
	if len([]byte(body.Content)) > webFileEditorMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large for web editor")
		return
	}
	if !utf8.ValidString(body.Content) {
		writeError(w, http.StatusUnsupportedMediaType, "content is not valid UTF-8 text")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	file, err := client.Create(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp write "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	_, writeErr := io.WriteString(file, body.Content)
	closeErr := file.Close()
	if writeErr != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp write "+filePath, decision.Action, writeErr.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, writeErr.Error())
		return
	}
	if closeErr != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp write "+filePath, decision.Action, closeErr.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, closeErr.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp write "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, map[string]any{"path": filePath, "size": len([]byte(body.Content))})
}

func (a *App) handleTargetFileTouch(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowUpload, _, ok := a.authorizeTargetSFTP(w, r, user, "touch")
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	filePath := cleanRemoteBodyPath(body.Path)
	if filePath == "" || filePath == "." || filePath == "/" {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if !allowUpload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp touch "+filePath, store.DecisionDeny, "upload/write is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP touch is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	if _, err := client.Stat(filePath); err == nil {
		writeError(w, http.StatusConflict, "file already exists")
		return
	}
	file, err := client.Create(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp touch "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := file.Close(); err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp touch "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp touch "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusCreated, map[string]any{"path": filePath})
}

func (a *App) handleTargetFileStat(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, _, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, "stat")
	if !ok {
		return
	}
	filePath := remotePathFromQuery(r)
	if !allowDownload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp stat "+filePath, store.DecisionDeny, "download/list is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP stat is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	info, err := client.Stat(filePath)
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp stat "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	entry := apiFileEntry(pathpkg.Dir(filePath), fileInfoWithName{FileInfo: info, name: pathpkg.Base(filePath)}, nil)
	out := apiTargetFileStatResponse{apiTargetFileEntry: entry, DiskUsage: info.Size(), Items: 1}
	if info.IsDir() {
		size, items, err := sftpDiskUsage(client, filePath)
		if err != nil {
			a.auditWebSFTP(r.Context(), user, target, decision, "sftp stat "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		out.DiskUsage = size
		out.Items = items
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp stat "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleTargetFileMkdir(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowUpload, _, ok := a.authorizeTargetSFTP(w, r, user, "mkdir")
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	dir := pathpkg.Clean(strings.TrimSpace(body.Path))
	if dir == "." || dir == "/" || dir == "" {
		writeError(w, http.StatusBadRequest, "invalid directory path")
		return
	}
	if !allowUpload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp mkdir "+dir, store.DecisionDeny, "upload/write is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP mkdir is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	if err := client.MkdirAll(dir); err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp mkdir "+dir, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp mkdir "+dir, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusCreated, map[string]any{"path": dir})
}

func (a *App) handleTargetFileDelete(w http.ResponseWriter, r *http.Request, user store.User) {
	target, decision, allowUpload, _, ok := a.authorizeTargetSFTP(w, r, user, "delete")
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	filePath := cleanRemoteBodyPath(body.Path)
	if filePath == "" || filePath == "/" || filePath == "." {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if !allowUpload {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp delete "+filePath, store.DecisionDeny, "upload/write is not allowed", 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP delete is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	if err := sftpRemoveAll(client, filePath); err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp delete "+filePath, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp delete "+filePath, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, map[string]any{"path": filePath})
}

func (a *App) handleTargetFileMove(w http.ResponseWriter, r *http.Request, user store.User) {
	a.handleTargetFileTransfer(w, r, user, "move")
}

func (a *App) handleTargetFileCopy(w http.ResponseWriter, r *http.Request, user store.User) {
	a.handleTargetFileTransfer(w, r, user, "copy")
}

func (a *App) handleTargetFileTransfer(w http.ResponseWriter, r *http.Request, user store.User, action string) {
	target, decision, allowUpload, allowDownload, ok := a.authorizeTargetSFTP(w, r, user, action)
	if !ok {
		return
	}
	var body struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	source := cleanRemoteBodyPath(body.Source)
	destination := cleanRemoteBodyPath(body.Destination)
	if source == "" || source == "/" || source == "." || destination == "" || destination == "." {
		writeError(w, http.StatusBadRequest, "invalid source or destination")
		return
	}
	if !allowUpload || (action == "copy" && !allowDownload) {
		reason := "upload/write is not allowed"
		if action == "copy" && !allowDownload {
			reason = "download/read is not allowed"
		}
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp "+action+" "+source+" "+destination, store.DecisionDeny, reason, 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP "+action+" is not allowed by policy")
		return
	}
	client, closeClient, err := a.openSFTPClient(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer closeClient()
	if action == "move" {
		err = client.Rename(source, destination)
	} else {
		err = sftpCopyPath(client, source, destination)
	}
	if err != nil {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp "+action+" "+source+" "+destination, decision.Action, err.Error(), 255, sshSourceIPFromRequest(r))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.auditWebSFTP(r.Context(), user, target, decision, "sftp "+action+" "+source+" "+destination, decision.Action, decision.Reason, 0, sshSourceIPFromRequest(r))
	writeJSON(w, http.StatusOK, map[string]any{"source": source, "destination": destination})
}

type fileInfoWithName struct {
	fs.FileInfo
	name string
}

func (info fileInfoWithName) Name() string {
	return info.name
}

func cleanRemoteBodyPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return pathpkg.Clean(value)
}

func sftpDiskUsage(client *sftp.Client, remotePath string) (int64, int64, error) {
	info, err := client.Stat(remotePath)
	if err != nil {
		return 0, 0, err
	}
	if !info.IsDir() {
		return info.Size(), 1, nil
	}
	var total int64
	var items int64 = 1
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		child := remoteJoin(remotePath, entry.Name())
		if entry.IsDir() {
			size, count, err := sftpDiskUsage(client, child)
			if err != nil {
				return 0, 0, err
			}
			total += size
			items += count
			continue
		}
		total += entry.Size()
		items++
	}
	return total, items, nil
}

func sftpRemoveAll(client *sftp.Client, remotePath string) error {
	info, err := client.Stat(remotePath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return client.Remove(remotePath)
	}
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := sftpRemoveAll(client, remoteJoin(remotePath, entry.Name())); err != nil {
			return err
		}
	}
	return client.RemoveDirectory(remotePath)
}

func sftpCopyPath(client *sftp.Client, source, destination string) error {
	info, err := client.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := client.MkdirAll(destination); err != nil {
			return err
		}
		entries, err := client.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := sftpCopyPath(client, remoteJoin(source, entry.Name()), remoteJoin(destination, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	src, err := client.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := client.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func sortFileEntries(entries []apiTargetFileEntry, sortKey, order string) {
	sortKey = strings.ToLower(strings.TrimSpace(sortKey))
	desc := strings.EqualFold(strings.TrimSpace(order), "desc")
	if sortKey == "" {
		sortKey = "name"
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		var less bool
		switch sortKey {
		case "size":
			if left.Size == right.Size {
				less = strings.ToLower(left.Name) < strings.ToLower(right.Name)
			} else {
				less = left.Size < right.Size
			}
		case "mode", "permission", "permissions":
			if left.Mode == right.Mode {
				less = strings.ToLower(left.Name) < strings.ToLower(right.Name)
			} else {
				less = left.Mode < right.Mode
			}
		case "modified", "modified_at", "mtime":
			if left.ModifiedAt == right.ModifiedAt {
				less = strings.ToLower(left.Name) < strings.ToLower(right.Name)
			} else {
				less = left.ModifiedAt < right.ModifiedAt
			}
		default:
			if left.Type != right.Type {
				less = left.Type == "dir"
			} else {
				less = strings.ToLower(left.Name) < strings.ToLower(right.Name)
			}
		}
		if desc {
			return !less && !sameFileEntrySortValue(left, right, sortKey)
		}
		return less
	})
}

func sameFileEntrySortValue(left, right apiTargetFileEntry, sortKey string) bool {
	switch sortKey {
	case "size":
		return left.Size == right.Size && strings.EqualFold(left.Name, right.Name)
	case "mode", "permission", "permissions":
		return left.Mode == right.Mode && strings.EqualFold(left.Name, right.Name)
	case "modified", "modified_at", "mtime":
		return left.ModifiedAt == right.ModifiedAt && strings.EqualFold(left.Name, right.Name)
	default:
		return left.Type == right.Type && strings.EqualFold(left.Name, right.Name)
	}
}

func copyRemoteFileToOpenTemp(reader io.Reader, name string) (string, error) {
	root := filepath.Join(os.TempDir(), "gosshd-open-files")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	localName := sanitizeOpenTempName(name)
	if localName == "" {
		localName = "download"
	}
	file, err := os.CreateTemp(root, "open-*-"+filepath.Base(localName))
	if err != nil {
		return "", err
	}
	localPath := file.Name()
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		_ = os.Remove(localPath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(localPath)
		return "", err
	}
	return localPath, nil
}

func sanitizeOpenTempName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	return strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		default:
			return r
		}
	}, name)
}

func (a *App) authorizeTargetSFTP(w http.ResponseWriter, r *http.Request, user store.User, action string) (store.SSHTarget, bastion.Decision, bool, bool, bool) {
	target, err := a.targetForUser(r.Context(), r.PathValue("id"), user)
	if err != nil {
		writeOwnerError(w, err)
		return store.SSHTarget{}, bastion.Decision{}, false, false, false
	}
	decision, allowUpload, allowDownload, err := a.bastion.EvaluateSFTPAccess(r.Context(), user.ID, target.ID, sshSourceIPFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return store.SSHTarget{}, bastion.Decision{}, false, false, false
	}
	if decision.Action == store.DecisionDeny {
		a.auditWebSFTP(r.Context(), user, target, decision, "sftp "+action+" "+remotePathFromQuery(r), store.DecisionDeny, decision.Reason, 126, sshSourceIPFromRequest(r))
		writeError(w, http.StatusForbidden, "SFTP denied: "+decision.Reason)
		return store.SSHTarget{}, bastion.Decision{}, false, false, false
	}
	return target, decision, allowUpload, allowDownload, true
}

func (a *App) openSFTPClient(ctx context.Context, target store.SSHTarget) (*sftp.Client, func(), error) {
	if target.TargetType == store.TargetAgent {
		reader, stream, err := a.openAgentStream(target.AgentID, protocol.StreamRequest{Type: protocol.StreamSFTP})
		if err != nil {
			return nil, nil, err
		}
		client, err := sftp.NewClientPipe(reader, stream)
		if err != nil {
			_ = stream.Close()
			return nil, nil, err
		}
		return client, func() {
			_ = client.Close()
			_ = stream.Close()
		}, nil
	}
	sshClient, err := a.openTargetSSHClient(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, err
	}
	return client, func() {
		_ = client.Close()
		_ = sshClient.Close()
	}, nil
}

func (a *App) auditWebSFTP(ctx context.Context, user store.User, target store.SSHTarget, decision bastion.Decision, command, action, reason string, code int, sourceIP string) {
	now := time.Now().UTC()
	_, _ = a.createAuditLog(ctx, store.CreateCommandAuditLogParams{
		UserID:         user.ID,
		TargetID:       target.ID,
		OrganizationID: organizationIDForTarget(target),
		PublicKeyName:  webConsolePublicKeyName,
		SessionID:      newAuditSessionID(),
		Command:        command,
		RequestType:    store.RequestSFTP,
		PolicyDecision: action,
		PolicyReason:   firstNonEmpty(reason, decision.Reason),
		ExitCode:       &code,
		StartedAt:      now,
		EndedAt:        &now,
		RemoteAddress:  sourceIP,
	})
}

func (a *App) targetForUser(ctx context.Context, targetID string, user store.User) (store.SSHTarget, error) {
	if err := a.ensureServices(ctx); err != nil {
		return store.SSHTarget{}, err
	}
	target, err := a.store.Repository().GetSSHTarget(ctx, strings.TrimSpace(targetID))
	if err != nil {
		return store.SSHTarget{}, err
	}
	if _, _, err := a.resolveOwner(ctx, target.OwnerType, target.OwnerID, user.ID); err != nil {
		return store.SSHTarget{}, err
	}
	return target, nil
}

func apiFileEntry(dir string, info fs.FileInfo, targetInfo fs.FileInfo) apiTargetFileEntry {
	typ := "file"
	if info.IsDir() {
		typ = "dir"
	} else if targetInfo != nil && targetInfo.IsDir() {
		typ = "dir"
	} else if info.Mode()&fs.ModeSymlink != 0 {
		typ = "symlink"
	}
	return apiTargetFileEntry{
		Name:       info.Name(),
		Path:       remoteJoin(dir, info.Name()),
		Type:       typ,
		Size:       info.Size(),
		Mode:       info.Mode().String(),
		ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
	}
}

func terminalSizeFromQuery(r *http.Request) (int, int) {
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	return cols, rows
}

func remotePathFromQuery(r *http.Request) string {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	if raw == "" {
		return "."
	}
	return pathpkg.Clean(raw)
}

func remoteJoin(dir, name string) string {
	if strings.TrimSpace(dir) == "" || dir == "." {
		return pathpkg.Clean(name)
	}
	return pathpkg.Join(dir, name)
}

func downloadName(remotePath string) string {
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return "download"
	}
	return strings.ReplaceAll(name, `"`, "")
}

func sshSourceIPFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return sshSourceIP(dummyRemoteAddr(r.RemoteAddr))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type dummyRemoteAddr string

func (a dummyRemoteAddr) Network() string { return "tcp" }
func (a dummyRemoteAddr) String() string  { return string(a) }
