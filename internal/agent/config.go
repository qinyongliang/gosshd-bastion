package agent

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
)

type Config struct {
	Desktop desktop.Config
	// DesktopDisabled is an explicit opt-out and wins over platform/environment enables.
	DesktopDisabled bool
	Server          string
	EnrollmentToken string
	IDFile          string
	Shell           string
	Root            string
	SSHHost         string
	SSHPort         string
	Version         string
}

// DesktopEnabledByDefault keeps platform defaults out of the native module.
func DesktopEnabledByDefault(goos string) bool {
	return goos == "windows" || goos == "darwin"
}

func configureDesktop(cfg Config) (Config, error) {
	return desktopConfig(cfg, runtime.GOOS, os.LookupEnv)
}

func desktopConfig(cfg Config, goos string, lookup func(string) (string, bool)) (Config, error) {
	enabled := cfg.Desktop.Enabled || DesktopEnabledByDefault(goos)
	disabled := cfg.DesktopDisabled
	for _, name := range []string{"GOSSHD_DESKTOP_ENABLE", "GOSSHD_DESKTOP_DISABLE"} {
		if value, ok := lookup(name); ok && strings.TrimSpace(value) != "" {
			parsed, err := strconv.ParseBool(strings.TrimSpace(value))
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a boolean: %w", name, err)
			}
			if name == "GOSSHD_DESKTOP_DISABLE" {
				disabled = disabled || parsed
			} else {
				enabled = parsed
			}
		}
	}
	cfg.Desktop.Enabled = enabled && !disabled
	cfg.DesktopDisabled = disabled
	if cfg.Desktop.HelperPath == "" {
		value, _ := lookup("GOSSHD_DESKTOP_HELPER")
		cfg.Desktop.HelperPath = strings.TrimSpace(value)
	}
	return cfg, nil
}
