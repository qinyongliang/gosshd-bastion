package server

import (
	"context"
	"errors"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

// Shared operations keep the web API and MCP on the same validation and lifecycle rules.
type tunnelConfigError struct{ error }

func (a *App) managedTunnel(ctx context.Context, actor store.User, id string) (store.Tunnel, error) {
	t, err := a.store.Repository().GetTunnel(ctx, id)
	if err != nil {
		return t, err
	}
	if err = a.requireOrganizationAdmin(ctx, t.OrganizationID, actor); err != nil {
		return store.Tunnel{}, errors.New("organization admin required")
	}
	return t, nil
}
func (a *App) saveManagedTunnel(ctx context.Context, actor store.User, t store.Tunnel) (apiTunnel, error) {
	if t.Protocol == "port" && t.Transports == nil {
		t.Transports = []string{"tcp"}
	}
	t.Name = strings.TrimSpace(t.Name)
	t.ListenHost = strings.TrimSpace(t.ListenHost)
	if t.ListenHost == "" {
		t.ListenHost = "0.0.0.0"
	}
	if t.Protocol == "http" {
		listeners, err := forward.HTTPListeners(t.HTTP)
		if err != nil {
			return apiTunnel{}, tunnelConfigError{err}
		}
		httpConfig := *t.HTTP
		httpConfig.Listeners = listeners
		t.HTTP = &httpConfig
		t.ListenHost, t.ListenPort = listeners[0].Host, listeners[0].Port
	}
	t.DestinationHost = strings.TrimSpace(t.DestinationHost)
	if err := a.validateTunnel(ctx, actor, t.OrganizationID, t.TunnelConfig); err != nil {
		return apiTunnel{}, tunnelConfigError{err}
	}
	saved, err := a.store.Repository().SaveTunnel(ctx, t)
	if err != nil {
		return apiTunnel{}, err
	}
	a.tunnels.notify()
	if a.forwards != nil {
		a.forwards.notify()
	}
	return a.tunnelView(saved), nil
}
func (a *App) setManagedTunnelEnabled(ctx context.Context, actor store.User, t store.Tunnel, enabled bool) (apiTunnel, error) {
	var err error
	if enabled {
		if err := a.validateTunnel(ctx, actor, t.OrganizationID, t.TunnelConfig); err != nil {
			return apiTunnel{}, tunnelConfigError{err}
		}
		t, err = a.store.Repository().EnableTunnelForUser(ctx, t.ID, t.DurationSeconds, actor.ID)
	} else {
		t, err = a.store.Repository().SetTunnelEnabled(ctx, t.ID, false, t.DurationSeconds)
	}
	if err != nil {
		return apiTunnel{}, err
	}
	a.tunnels.stop(t.ID)
	return a.tunnelView(t), nil
}
