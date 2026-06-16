package protocol

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type shellUser struct {
	UID      string
	Username string
}

type shellProbe struct {
	getenv      func(string) string
	lookPath    func(string) (string, error)
	readFile    func(string) ([]byte, error)
	readlink    func(string) (string, error)
	getppid     func() int
	currentUser func() (shellUser, error)
}

func DefaultShell() string {
	return defaultShell(runtime.GOOS, shellProbe{
		getenv:      os.Getenv,
		lookPath:    exec.LookPath,
		readFile:    os.ReadFile,
		readlink:    os.Readlink,
		getppid:     os.Getppid,
		currentUser: currentShellUser,
	})
}

func defaultShell(goos string, probe shellProbe) string {
	if goos == "windows" {
		if shell := strings.TrimSpace(probe.getenv("COMSPEC")); shell != "" {
			return shell
		}
		if shell := firstAvailableShell(probe.lookPath, "pwsh.exe", "powershell.exe", "cmd.exe"); shell != "" {
			return shell
		}
		return "powershell.exe"
	}

	if shell := processChainShell(goos, probe); shell != "" {
		return shell
	}
	if shell := strings.TrimSpace(probe.getenv("SHELL")); shell != "" {
		return shell
	}
	if shell := passwdLoginShell(probe); shell != "" {
		return shell
	}
	if shell := firstAvailableShell(probe.lookPath,
		"/bin/bash", "/usr/bin/bash",
		"/bin/zsh", "/usr/bin/zsh",
		"/bin/ash", "/usr/bin/ash",
		"/bin/dash", "/usr/bin/dash",
		"/bin/ksh", "/usr/bin/ksh",
		"/bin/sh", "/usr/bin/sh",
	); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func processChainShell(goos string, probe shellProbe) string {
	if goos != "linux" || probe.getppid == nil {
		return ""
	}
	pid := probe.getppid()
	seen := map[int]bool{}
	var genericShell string
	for depth := 0; depth < 16 && pid > 1 && !seen[pid]; depth++ {
		seen[pid] = true
		if shell := procShell(pid, probe); shell != "" {
			if !isGenericShell(shell) {
				return shell
			}
			if genericShell == "" {
				genericShell = shell
			}
		}
		next := procParentPID(pid, probe)
		if next <= 0 || next == pid {
			break
		}
		pid = next
	}
	return genericShell
}

func procShell(pid int, probe shellProbe) string {
	if probe.readlink != nil {
		if exe, err := probe.readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
			exe = strings.TrimSuffix(exe, " (deleted)")
			if isShellName(filepath.Base(exe)) {
				return exe
			}
		}
	}
	if shell := procShellFromFile(pid, "cmdline", probe); shell != "" {
		return shell
	}
	return procShellFromFile(pid, "comm", probe)
}

func procShellFromFile(pid int, name string, probe shellProbe) string {
	if probe.readFile == nil {
		return ""
	}
	data, err := probe.readFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(data))
	if name == "cmdline" {
		if i := strings.IndexByte(value, 0); i >= 0 {
			value = value[:i]
		}
	}
	if value == "" {
		return ""
	}
	base := shellBaseName(value)
	if !isShellName(base) {
		return ""
	}
	if strings.ContainsAny(value, `/\`) {
		return value
	}
	if resolved := firstAvailableShell(probe.lookPath, base); resolved != "" {
		return resolved
	}
	return base
}

func procParentPID(pid int, probe shellProbe) int {
	if probe.readFile == nil {
		return 0
	}
	data, err := probe.readFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	stat := string(data)
	end := strings.LastIndex(stat, ")")
	if end < 0 {
		return 0
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return ppid
}

func currentShellUser() (shellUser, error) {
	u, err := user.Current()
	if err != nil {
		return shellUser{}, err
	}
	return shellUser{UID: u.Uid, Username: u.Username}, nil
}

func passwdLoginShell(probe shellProbe) string {
	if probe.currentUser == nil || probe.readFile == nil {
		return ""
	}
	u, err := probe.currentUser()
	if err != nil {
		return ""
	}
	data, err := probe.readFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		if fields[2] != u.UID && fields[0] != u.Username && fields[0] != shortUsername(u.Username) {
			continue
		}
		shell := strings.TrimSpace(fields[6])
		if isLoginShellDisabled(shell) {
			return ""
		}
		return shell
	}
	return ""
}

func firstAvailableShell(lookPath func(string) (string, error), candidates ...string) string {
	if lookPath == nil {
		return ""
	}
	for _, candidate := range candidates {
		if resolved, err := lookPath(candidate); err == nil {
			return resolved
		}
	}
	return ""
}

func isShellName(name string) bool {
	switch shellBaseName(name) {
	case "bash", "zsh", "fish", "ksh", "mksh", "ash", "dash", "sh", "csh", "tcsh":
		return true
	default:
		return false
	}
}

func isGenericShell(shell string) bool {
	switch shellBaseName(shell) {
	case "sh", "dash", "ash":
		return true
	default:
		return false
	}
}

func shellBaseName(shell string) string {
	base := filepath.Base(strings.TrimSpace(shell))
	base = strings.TrimPrefix(base, "-")
	return strings.TrimSuffix(strings.ToLower(base), ".exe")
}

func isLoginShellDisabled(shell string) bool {
	switch strings.TrimSpace(shell) {
	case "", "/bin/false", "/usr/bin/false", "/sbin/nologin", "/usr/sbin/nologin":
		return true
	default:
		return false
	}
}

func shortUsername(username string) string {
	if i := strings.LastIndexAny(username, `\/`); i >= 0 {
		return username[i+1:]
	}
	return username
}
