package server

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

const (
	localAgentID         = "00000000-0000-0000-0000-000000000001"
	localTerminalAlias   = "local-terminal"
	localTerminalName    = "Local terminal"
	localTerminalRuntime = "embedded-client-agent"
)

func (a *App) ensureClientLocalAgent(ctx context.Context, user store.User) error {
	org, err := a.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		return err
	}
	targetID, err := a.ensureClientLocalTarget(ctx, user, org)
	if err != nil {
		return err
	}
	a.localTargetID = targetID
	if a.localAgentCancel != nil {
		return nil
	}

	serverConn, agentConn := net.Pipe()
	serverSession, err := yamux.Server(serverConn, nil)
	if err != nil {
		_ = serverConn.Close()
		_ = agentConn.Close()
		return err
	}
	agentSession, err := yamux.Client(agentConn, nil)
	if err != nil {
		_ = serverSession.Close()
		_ = agentConn.Close()
		return err
	}

	root := clientLocalShellRoot()
	client, err := agent.NewEmbedded(agent.Config{Root: root}, localAgentID)
	if err != nil {
		_ = serverSession.Close()
		_ = agentSession.Close()
		return err
	}

	agentCtx, cancel := context.WithCancel(context.Background())
	a.localAgentCancel = cancel
	a.registry.Register(localAgentID, serverSession)
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		defer a.registry.Unregister(localAgentID, serverSession)
		defer serverSession.Close()
		defer agentSession.Close()
		if err := client.ServeSession(agentCtx, agentSession); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("embedded local agent stopped: %v", err)
		}
	}()
	log.Printf("embedded local agent ready: target=%s root=%s", targetID, root)
	return nil
}

func (a *App) ensureClientLocalTarget(ctx context.Context, user store.User, org store.Organization) (string, error) {
	targets, err := a.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, org.ID)
	if err != nil {
		return "", err
	}
	for _, target := range targets {
		if target.TargetType == store.TargetAgent && (target.AgentID == localAgentID || target.Alias == localTerminalAlias) {
			if target.AgentID != localAgentID || target.Alias != localTerminalAlias || target.Name == "" {
				updated, err := a.store.Repository().UpdateSSHTarget(ctx, target.ID, store.UpdateSSHTargetParams{
					Name:           localTerminalName,
					Alias:          localTerminalAlias,
					Host:           "localhost",
					Port:           0,
					RemoteUsername: "user",
					AuthType:       store.AuthPassword,
					AgentID:        localAgentID,
				})
				if err != nil {
					return "", err
				}
				return updated.ID, nil
			}
			return target.ID, nil
		}
	}
	target, err := a.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      store.OwnerOrganization,
		OwnerID:        org.ID,
		Name:           localTerminalName,
		Alias:          localTerminalAlias,
		TargetType:     store.TargetAgent,
		Host:           "localhost",
		Port:           0,
		RemoteUsername: "user",
		AuthType:       store.AuthPassword,
		AgentID:        localAgentID,
		CreatedBy:      user.ID,
	})
	if err != nil {
		return "", err
	}
	return target.ID, nil
}

func clientLocalShellRoot() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		return wd
	}
	return filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)
}
