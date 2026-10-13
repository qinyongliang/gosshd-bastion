//go:build windows

package desktop

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

//go:embed native/windows/DesktopHelper.cs
var windowsSource []byte

func platformCommand(ctx context.Context, cfg Config) (*exec.Cmd, func(), error) {
	if cfg.HelperPath != "" {
		if !filepath.IsAbs(cfg.HelperPath) {
			return nil, nil, errors.New("desktop helper path must be absolute")
		}
		return exec.CommandContext(ctx, cfg.HelperPath), func() {}, nil
	}
	dir, err := os.MkdirTemp("", "gosshd-desktop-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	source := filepath.Join(dir, "DesktopHelper.cs")
	binary := filepath.Join(dir, "gosshd-desktop-helper.exe")
	if err = os.WriteFile(source, windowsSource, 0600); err != nil {
		cleanup()
		return nil, nil, err
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	framework := filepath.Join(root, "Microsoft.NET", "Framework64", "v4.0.30319")
	if _, err = os.Stat(filepath.Join(framework, "csc.exe")); err != nil {
		framework = filepath.Join(root, "Microsoft.NET", "Framework", "v4.0.30319")
	}
	args := []string{"/nologo", "/target:exe", "/out:" + binary, "/reference:" + filepath.Join(framework, "System.Web.Extensions.dll"), "/reference:" + filepath.Join(framework, "WPF", "UIAutomationClient.dll"), "/reference:" + filepath.Join(framework, "WPF", "UIAutomationTypes.dll"), "/reference:" + filepath.Join(framework, "WPF", "WindowsBase.dll"), "/reference:System.Drawing.dll", source}
	compiler := exec.CommandContext(ctx, filepath.Join(framework, "csc.exe"), args...)
	compiler.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := compiler.CombinedOutput(); err != nil {
		cleanup()
		return nil, nil, errors.New("compile Windows desktop helper: " + string(output))
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd, cleanup, nil
}

func startNativeProcess(ctx context.Context, cfg Config) (*process, error) {
	return startProcess(ctx, cfg, platformCommand)
}
