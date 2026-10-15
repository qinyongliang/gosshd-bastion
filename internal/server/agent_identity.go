package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

const agentReconnectProbeTimeout = 5 * time.Second

func validAgentBootID(value string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	return id, err == nil && id != uuid.Nil
}

func (a *App) isSameAgentInstance(agent store.Agent, hello protocol.AgentHello) bool {
	if hello.ID != agent.CurrentRuntimeID {
		return false
	}
	info, ok := a.registry.Info(agent.ID)
	if !ok {
		return false
	}
	boot, hasBoot := validAgentBootID(hello.BootID)
	previous, hadBoot := validAgentBootID(info.BootID)
	if hasBoot && hadBoot {
		return boot == previous
	}
	// Older peers have only process-local continuity. Do not merge empty IDs.
	instance := strings.TrimSpace(hello.InstanceID)
	return protocol.IsValidID(instance) && info.InstanceID == instance
}

// A different boot is only a potential clone: the old registration may outlive
// a normal reboot or transport failure. Only an answering old peer proves that
// two different boots are alive. Caller must already validate enrollment/owner.
func (a *App) agentConnectionCanResume(ctx context.Context, agent store.Agent, hello protocol.AgentHello) (bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		previous, err := a.registry.Get(agent.ID)
		if errors.Is(err, ErrAgentOffline) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if a.isSameAgentInstance(agent, hello) {
			return true, nil
		}
		alive, err := probeAgentConnection(ctx, previous, agentReconnectProbeTimeout)
		if err != nil {
			return false, err
		}
		if alive {
			return false, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !a.registry.Unregister(agent.ID, previous) {
			continue
		}
		_ = previous.Close()
		return true, nil
	}
	return false, errors.New("Agent connection changed during reconnect; retry")
}

func probeAgentConnection(ctx context.Context, session *yamux.Session, timeout time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	done := make(chan error, 1)
	go func() { _, err := session.Ping(); done <- err }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err == nil, nil
	case <-timer.C:
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
