package server

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configureLocalShellCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
