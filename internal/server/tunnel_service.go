package server

import (
	"context"
	"errors"
	"strings"

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
	t.Name = strings.TrimSpace(t.Name)
	t.ListenHost = strings.TrimSpace(t.ListenHost)
	t.DestinationHost = strings.TrimSpace(t.DestinationHost)
	if err := a.validateTunnel(ctx, actor, t.OrganizationID, t.TunnelConfig); err != nil {
		return apiTunnel{}, tunnelConfigError{err}
	}
	saved, err := a.store.Repository().SaveTunnel(ctx, t)
	if err != nil {
		return apiTunnel{}, err
	}
	a.tunnels.notify()
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
