package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/qinyongliang/gosshd/internal/server"
)

var version = server.DefaultVersion

func main() {
	var cfg server.Config
	flag.StringVar(&cfg.HTTPListen, "http-listen", ":80", "HTTP listen address")
	flag.StringVar(&cfg.SSHListen, "ssh-listen", ":22", "SSH listen address")
	flag.StringVar(&cfg.HostKeyPath, "host-key-path", "gosshd_host_key", "SSH host private key path; generated if missing")
	flag.StringVar(&cfg.DatabasePath, "database-path", "gosshd.db", "SQLite database path")
	flag.StringVar(&cfg.SecretKey, "secret-key", "", "secret key material used to encrypt stored credentials")
	flag.StringVar(&cfg.SecretKeyPath, "secret-key-path", "", "path to secret key material used to encrypt stored credentials")
	flag.StringVar(&cfg.SessionCookieName, "session-cookie-name", "", "HTTP session cookie name")
	flag.StringVar(&cfg.PublicHost, "public-host", "", "public host override used in run scripts; defaults to the request Host")
	flag.StringVar(&cfg.PublicSSHHost, "public-ssh-host", "", "public SSH host shown by agents; defaults to public host or the request Host")
	flag.StringVar(&cfg.PublicSSHPort, "public-ssh-port", "", "public SSH port shown by agents; defaults to ssh-listen port")
	flag.StringVar(&cfg.AgentToken, "agent-token", "", "optional shared token required from agents")
	flag.StringVar(&cfg.AgentPath, "agent-path", "dist/agent", "directory containing agent binaries by goos/goarch")
	flag.StringVar(&cfg.AgentCachePath, "agent-cache-path", "", "directory used to cache downloaded agent binaries")
	flag.StringVar(&cfg.Version, "version", version, "release version used when proxy-downloading agent binaries")
	flag.StringVar(&cfg.ReleaseBaseURL, "release-base-url", "", "base URL for release downloads")
	flag.StringVar(&cfg.ReleaseProxyURL, "release-proxy-url", "", "fallback proxy prefix for slow or failed release downloads")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := server.NewApp(cfg)
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
