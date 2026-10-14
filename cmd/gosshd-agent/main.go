package main

import (
	"flag"
	"log"
	"os"
	"runtime"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
)

var version = "dev"

func parseAgentFlags(flags *flag.FlagSet, args []string) (agent.Config, error) {
	var cfg agent.Config
	cfg.Version = version
	flags.StringVar(&cfg.Server, "server", "", "public gosshd server URL (required)")
	flags.StringVar(&cfg.EnrollmentToken, "enrollment-token", "", "owner-scoped bastion enrollment token")
	flags.StringVar(&cfg.IDFile, "id-file", "", "path to stable local agent id file")
	flags.StringVar(&cfg.Shell, "shell", "", "shell executable; defaults to parent process shell, SHELL, or login shell")
	flags.StringVar(&cfg.Root, "root", "", "working directory; defaults to the current user's home directory")
	flags.StringVar(&cfg.SSHHost, "ssh-host", "", "public SSH host shown in connection hints")
	flags.StringVar(&cfg.SSHPort, "ssh-port", "", "public SSH port shown in connection hints")
	flags.BoolVar(&cfg.Desktop.Enabled, "desktop-enable", agent.DesktopEnabledByDefault(runtime.GOOS), "enable desktop RPC (default on Windows/macOS); requires a logged-in user desktop")
	flags.StringVar(&cfg.Desktop.HelperPath, "desktop-helper", "", "absolute path to independent desktop helper (macOS app executable or Windows exe)")
	flags.BoolVar(&cfg.DesktopDisabled, "desktop-disable", false, "disable desktop RPC, including its default Windows/macOS capability")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "desktop-enable" && !cfg.Desktop.Enabled {
			cfg.DesktopDisabled = true
		}
	})
	return cfg, nil
}

func main() {
	cfg, err := parseAgentFlags(flag.CommandLine, os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	if err := runAgent(cfg); err != nil {
		log.Fatal(err)
	}
}
