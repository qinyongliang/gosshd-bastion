package main

import (
	"flag"
	"log"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
)

var version = "dev"

func main() {
	var cfg agent.Config
	cfg.Version = version
	flag.StringVar(&cfg.Server, "server", "", "public gosshd server URL (required)")
	flag.StringVar(&cfg.EnrollmentToken, "enrollment-token", "", "owner-scoped bastion enrollment token")
	flag.StringVar(&cfg.IDFile, "id-file", "", "path to stable local agent id file")
	flag.StringVar(&cfg.Shell, "shell", "", "shell executable; defaults to parent process shell, SHELL, or login shell")
	flag.StringVar(&cfg.Root, "root", "", "working directory; defaults to the current user's home directory")
	flag.StringVar(&cfg.SSHHost, "ssh-host", "", "public SSH host shown in connection hints")
	flag.StringVar(&cfg.SSHPort, "ssh-port", "", "public SSH port shown in connection hints")
	flag.BoolVar(&cfg.Desktop.Enabled, "desktop-enable", false, "opt in to desktop RPC; requires a logged-in user desktop")
	flag.StringVar(&cfg.Desktop.HelperPath, "desktop-helper", "", "absolute path to independent desktop helper (macOS app executable or Windows exe)")
	flag.Parse()

	if err := runAgent(cfg); err != nil {
		log.Fatal(err)
	}
}
