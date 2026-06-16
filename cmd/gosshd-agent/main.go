package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/qinyongliang/gosshd/internal/agent"
)

func main() {
	var cfg agent.Config
	flag.StringVar(&cfg.Server, "server", "http://localhost", "public gosshd server URL")
	flag.StringVar(&cfg.Token, "token", "", "optional shared token for server registration")
	flag.StringVar(&cfg.IDFile, "id-file", "", "path to stable local agent id file")
	flag.StringVar(&cfg.Shell, "shell", "", "shell executable")
	flag.StringVar(&cfg.Root, "root", "", "working directory; defaults to the directory where the agent starts")
	flag.StringVar(&cfg.SSHHost, "ssh-host", "", "public SSH host shown in connection hints")
	flag.StringVar(&cfg.SSHPort, "ssh-port", "", "public SSH port shown in connection hints")
	flag.Parse()

	client, err := agent.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(client.SSHAddress())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := client.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
