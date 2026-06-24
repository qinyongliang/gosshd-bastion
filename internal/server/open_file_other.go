//go:build !windows

package server

import (
	"os/exec"
	"runtime"
)

func openLocalFile(path string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	return exec.Command(command, path).Start()
}
