//go:build windows

package server

import (
	"os/exec"
	"syscall"
)

func openLocalFile(path string) error {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}
