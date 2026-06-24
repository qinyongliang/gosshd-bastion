//go:build !windows

package server

import "os/exec"

func configureLocalShellCommand(_ *exec.Cmd) {}
