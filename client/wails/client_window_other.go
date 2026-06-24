//go:build !windows

package main

import "os/exec"

func configureClientWindowCommand(_ *exec.Cmd) {}
