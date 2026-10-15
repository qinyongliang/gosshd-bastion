//go:build linux || android

package agent

import "os"

func readSystemBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return string(data), err
}
