package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
)

var version = "dev"

func main() {
	if len(os.Args) != 2 {
		log.Fatal("private APK configuration path required")
	}
	cfg, err := loadAPKConfig(os.Args[1])
	if err != nil {
		log.Fatal("APK configuration is invalid; open the app and save the connection again")
	}
	cfg.Version = version
	cfg.OnConnectionState = func(online bool) {
		if online {
			fmt.Println("GOSSHD_APK_STATE online")
		} else {
			fmt.Println("GOSSHD_APK_STATE offline")
		}
	}
	if err := os.MkdirAll(cfg.Root, 0700); err != nil {
		log.Fatal("cannot prepare private Agent home")
	}
	client, err := agent.New(cfg)
	if err != nil {
		log.Fatal("cannot initialize APK Agent")
	}
	defer client.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := client.Run(ctx); err != nil {
		log.Fatal("APK Agent stopped unexpectedly")
	}
}
