//go:build !windows

package agent

import (
	"strings"
	"syscall"
)

func installReplacement(currentExe, tmpPath string) (string, error) {
	installPath := stableExecutablePath(currentExe)
	if err := syscall.Rename(tmpPath, installPath); err != nil {
		return "", err
	}
	return installPath, nil
}

func syscallExec(currentExe string, args, env []string) error {
	return syscall.Exec(currentExe, args, env)
}

func stableExecutablePath(currentExe string) string {
	if base, _, ok := strings.Cut(currentExe, ".new-"); ok {
		return base
	}
	return currentExe
}
