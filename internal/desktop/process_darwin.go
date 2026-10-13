//go:build darwin

package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func platformCommand(ctx context.Context, cfg Config) (*exec.Cmd, func(), error) {
	path := cfg.HelperPath
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		path = filepath.Join(filepath.Dir(exe), "GOSSHD Desktop Helper.app", "Contents", "MacOS", "gosshd-desktop-helper")
	}
	if !filepath.IsAbs(path) {
		return nil, nil, errors.New("desktop helper path must be absolute")
	}
	return exec.CommandContext(ctx, path), func() {}, nil
}
