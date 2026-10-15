//go:build darwin

package agent

import "golang.org/x/sys/unix"

func readSystemBootID() (string, error) { return unix.Sysctl("kern.bootsessionuuid") }
