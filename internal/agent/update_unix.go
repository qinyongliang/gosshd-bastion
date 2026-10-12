//go:build !windows

package agent

import (
	"fmt"
	"log"
	"os"
	"syscall"
)

func replaceAndRestart(currentExe, tmpPath string) error {
	restartPath, err := installReplacement(currentExe, tmpPath)
	if err != nil {
		return fmt.Errorf("replace: %w", err)
	}
	// This also removes the running old image if it came from a legacy temp path.
	if currentExe != restartPath {
		if err := os.Remove(currentExe); err != nil && !os.IsNotExist(err) {
			log.Printf("agent update cleanup: %v", err)
		}
	}
	if err := cleanupUpdateArtifacts(restartPath, ""); err != nil {
		log.Printf("agent update cleanup: %v", err)
	}
	return syscallExec(restartPath, append([]string{restartPath}, os.Args[1:]...), os.Environ())
}

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
