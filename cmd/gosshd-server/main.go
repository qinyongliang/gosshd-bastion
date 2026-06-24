package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/qinyongliang/gosshd-bastion/internal/server"
)

var version = server.DefaultVersion

func main() {
	var cfg server.Config
	flag.BoolVar(&cfg.ClientMode, "client-mode", false, "run as a local single-user client backend")
	flag.StringVar(&cfg.HTTPListen, "http-listen", ":80", "HTTP listen address")
	flag.StringVar(&cfg.SSHListen, "ssh-listen", ":22", "SSH listen address")
	flag.StringVar(&cfg.HostKeyPath, "host-key-path", "gosshd_host_key", "SSH host private key path; generated if missing")
	flag.StringVar(&cfg.DatabasePath, "database-path", "gosshd.db", "SQLite database path")
	flag.StringVar(&cfg.AuditDatabasePath, "audit-database-path", "", "SQLite audit database path; defaults to gosshd-audit.db next to the main database")
	flag.StringVar(&cfg.AuditRecordingPath, "audit-recording-path", "", "directory for compressed terminal recordings; defaults to audit-recordings next to the main database")
	flag.StringVar(&cfg.SecretKey, "secret-key", "", "secret key material used to encrypt stored credentials")
	flag.StringVar(&cfg.SecretKeyPath, "secret-key-path", "", "path to secret key material used to encrypt stored credentials")
	flag.StringVar(&cfg.BootstrapAdminPassword, "bootstrap-admin-password", "", "password for first-run admin account; falls back to GOSSHD_BOOTSTRAP_ADMIN_PASSWORD or generated")
	flag.StringVar(&cfg.SessionCookieName, "session-cookie-name", "", "HTTP session cookie name")
	flag.StringVar(&cfg.PublicHost, "public-host", "", "public host override used in generated install links; defaults to the request Host")
	flag.IntVar(&cfg.PublicSSHPort, "public-ssh-port", 0, "public SSH port shown in copy commands and agent hints; defaults to ssh-listen port")
	flag.StringVar(&cfg.KnownHostsPath, "known-hosts-path", "", "OpenSSH known_hosts file used to verify target host keys; defaults next to the database")
	flag.StringVar(&cfg.AgentPath, "agent-path", "dist/agent", "directory containing agent binaries by goos/goarch")
	flag.StringVar(&cfg.AgentCachePath, "agent-cache-path", "", "directory used to cache downloaded agent binaries")
	flag.StringVar(&cfg.Version, "version", version, "release version used when proxy-downloading agent binaries")
	flag.StringVar(&cfg.ReleaseBaseURL, "release-base-url", "", "base URL for release downloads")
	flag.StringVar(&cfg.ReleaseProxyURL, "release-proxy-url", "", "explicit proxy prefix for slow or failed release downloads")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := server.NewApp(cfg)
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
