//go:build windows

package agent

import "syscall"

func installReplacement(currentExe, tmpPath string) (string, error) {
	return tmpPath, nil
}

func syscallExec(currentExe string, args, env []string) error {
	return syscall.EWINDOWS
}
