//go:build !windows

package agent

import "syscall"

func installReplacement(currentExe, tmpPath string) (string, error) {
	if err := syscall.Rename(tmpPath, currentExe); err != nil {
		return "", err
	}
	return currentExe, nil
}

func syscallExec(currentExe string, args, env []string) error {
	return syscall.Exec(currentExe, args, env)
}
