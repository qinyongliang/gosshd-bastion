//go:build !windows && !darwin

package desktop

import (
	"context"
	"errors"
	"os/exec"
)

func platformCommand(context.Context, Config) (*exec.Cmd, func(), error) {
	return nil, nil, errors.New("desktop helpers are supported only on Windows and macOS")
}

func startNativeProcess(ctx context.Context, cfg Config) (*process, error) {
	return startProcess(ctx, cfg, platformCommand)
}
