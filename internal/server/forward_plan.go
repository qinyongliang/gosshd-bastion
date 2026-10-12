package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"net"
	"strconv"
)

func (a *App) forwardAgentID(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	for depth := 0; depth < 4; depth++ {
		t, err := a.store.Repository().GetSSHTarget(ctx, id)
		if err != nil {
			return "", err
		}
		if t.TargetType == store.TargetAgent {
			return t.AgentID, nil
		}
		if t.ProxyTargetID == "" {
			return "", nil
		}
		id = t.ProxyTargetID
	}
	return "", errors.New("SSH chain too deep")
}
func (a *App) forwardHops(ctx context.Context, id string) ([]protocol.TunnelSSHHop, error) {
	if id == "" {
		return nil, nil
	}
	t, err := a.store.Repository().GetSSHTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.TargetType == store.TargetAgent {
		return nil, nil
	}
	ep, err := a.tunnelAgentEndpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	if ep == nil {
		return nil, nil
	}
	return ep.hops, nil
}
func (m *edgeForwardManager) build(t store.Tunnel) (forwardPlan, error) {
	entry, err := m.app.forwardAgentID(m.ctx, t.EntryTargetID)
	if err != nil {
		return forwardPlan{}, err
	}
	d := protocol.ForwardDeployment{ID: t.ID, Revision: t.Revision, Protocol: t.Protocol, ListenAddress: net.JoinHostPort(t.ListenHost, strconv.Itoa(t.ListenPort)), HTTP: t.HTTP}
	if t.ExpiresAt != nil {
		d.ExpiresAt = t.ExpiresAt.UnixMilli()
	}
	p := forwardPlan{deployment: d, entryAgent: entry, links: map[string]forwardLinkSpec{}}
	var locations []forward.Location
	if t.Protocol == "http" {
		for _, s := range t.HTTP.Servers {
			locations = append(locations, s.Locations...)
		}
	} else {
		l := forward.Location{ID: "tcp", ExitTargetID: t.ExitTargetID, Host: t.DestinationHost, Port: t.DestinationPort}
		p.deployment.TCP = &l
		locations = []forward.Location{l}
	}
	if entry != "" {
		p.deployment.ListenHops, err = m.app.forwardHops(m.ctx, t.EntryTargetID)
		if err != nil {
			return p, err
		}
	}
	for _, l := range locations {
		route := protocol.ForwardRoute{ID: l.ID, Address: l.Address()}
		exit, err := m.app.forwardAgentID(m.ctx, l.ExitTargetID)
		if err != nil {
			return p, err
		}
		if entry != "" && entry == exit {
			route.Hops, err = m.app.forwardHops(m.ctx, l.ExitTargetID)
			if err != nil {
				return p, err
			}
		} else if entry != "" {
			route.LinkID = fmt.Sprintf("%s:%d:%s", t.ID, t.Revision, l.ExitTargetID)
			spec := p.links[route.LinkID]
			spec.exitAgent = exit
			spec.targetID = l.ExitTargetID
			spec.peer.LinkID = route.LinkID
			spec.peer.ConfigID = t.ID
			spec.peer.Revision = t.Revision
			spec.peer.ExpiresAt = p.deployment.ExpiresAt
			exitRoute := protocol.ForwardRoute{ID: l.ID, Address: l.Address()}
			if exit != "" {
				exitRoute.Hops, err = m.app.forwardHops(m.ctx, l.ExitTargetID)
				if err != nil {
					return p, err
				}
			}
			spec.peer.Routes = append(spec.peer.Routes, exitRoute)
			p.links[route.LinkID] = spec
		}
		p.deployment.Routes = append(p.deployment.Routes, route)
	}
	return p, nil
}
