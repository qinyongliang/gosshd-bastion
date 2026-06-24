//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
)

func runAgent(cfg agent.Config) error {
	client, err := agent.New(cfg)
	if err != nil {
		return err
	}
	fmt.Println(client.SSHAddress())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return client.Run(ctx)
}
