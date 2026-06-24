//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "gosshd-agent"

func runAgent(cfg agent.Config) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if isService {
		return svc.Run(windowsServiceName, agentService{cfg: cfg})
	}
	return runAgentConsole(cfg)
}

func runAgentConsole(cfg agent.Config) error {
	client, err := agent.New(cfg)
	if err != nil {
		return err
	}
	fmt.Println(client.SSHAddress())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return client.Run(ctx)
}

type agentService struct {
	cfg agent.Config
}

func (s agentService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	client, err := agent.New(s.cfg)
	if err != nil {
		log.Printf("agent service init failed: %v", err)
		return false, 1
	}
	log.Printf("agent service online hint: %s", client.SSHAddress())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx)
	}()

	const accepts = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.Running, Accepts: accepts}
	for {
		select {
		case err := <-done:
			if err != nil {
				log.Printf("agent service stopped with error: %v", err)
				return false, 1
			}
			return false, 0
		case req, ok := <-requests:
			if !ok {
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-done
				if err != nil {
					log.Printf("agent service stopped with error: %v", err)
					return false, 1
				}
				return false, 0
			}
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-done
				if err != nil {
					log.Printf("agent service stopped with error: %v", err)
					return false, 1
				}
				return false, 0
			default:
				log.Printf("unsupported service control request: %v", req.Cmd)
			}
		}
	}
}
