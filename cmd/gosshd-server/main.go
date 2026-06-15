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

func main() {
	var cfg server.Config
	flag.StringVar(&cfg.HTTPListen, "http-listen", ":80", "HTTP listen address")
	flag.StringVar(&cfg.SSHListen, "ssh-listen", ":22", "SSH listen address")
	flag.StringVar(&cfg.PublicHost, "public-host", "", "public host used in install scripts and printed SSH addresses")
	flag.StringVar(&cfg.AgentToken, "agent-token", "", "optional shared token required from agents")
	flag.StringVar(&cfg.AgentPath, "agent-path", "dist/agent", "directory containing agent binaries by goos/goarch")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := server.NewApp(cfg)
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
