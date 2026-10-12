package server

import (
	"context"
	"errors"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"io"
	"net"
	"strconv"
)

func (m *edgeForwardManager) serverDial(ctx context.Context, targetID string, routes []protocol.ForwardRoute) (forward.DialFunc, []io.Closer, error) {
	allowed := map[string]protocol.ForwardRoute{}
	for _, r := range routes {
		if r.Network != "tcp" && r.Network != "udp" {
			return nil, nil, errors.New("invalid route network")
		}
		allowed[r.ID] = r
	}
	base := forward.DialNetwork
	var closers []io.Closer
	if targetID != "" {
		target, err := m.app.store.Repository().GetSSHTarget(ctx, targetID)
		if err != nil {
			return nil, nil, err
		}
		if target.TargetType == store.TargetAgent {
			base = func(ctx context.Context, network, address string) (net.Conn, error) {
				session, err := m.app.registry.Get(target.AgentID)
				if err != nil {
					return nil, err
				}
				return dialTunnelAgentNetwork(ctx, session, address, network)
			}
		} else {
			for _, r := range routes {
				if r.Network == "udp" {
					return nil, nil, errors.New("SSH endpoints do not support UDP forwarding")
				}
			}
			client, err := m.app.openTargetSSHClient(ctx, target)
			if err != nil {
				return nil, nil, err
			}
			closers = append(closers, client)
			base = func(ctx context.Context, network, address string) (net.Conn, error) {
				return client.DialContext(ctx, network, address)
			}
		}
	}
	return func(ctx context.Context, id string) (net.Conn, error) {
		route, ok := allowed[id]
		if !ok {
			return nil, errors.New("unauthorized route")
		}
		return base(ctx, route.Network, route.Address)
	}, closers, nil
}
func (m *edgeForwardManager) ensureServerRun(t store.Tunnel) {
	m.mu.Lock()
	old := m.runs[t.ID]
	if old != nil && old.revision == t.Revision && old.err == "" && old.runtime != nil && old.runtime.Alive() {
		m.mu.Unlock()
		return
	}
	delete(m.runs, t.ID)
	m.mu.Unlock()
	if old != nil {
		old.close()
	}
	r := &forwardServerRun{revision: t.Revision}
	ctx, cancel := context.WithCancel(m.ctx)
	if t.ExpiresAt != nil {
		cancel()
		ctx, cancel = context.WithDeadline(m.ctx, *t.ExpiresAt)
	}
	r.cancel = cancel
	start := func() error {
		logs, err := m.logStore(t.ID)
		if err != nil {
			return err
		}
		address := net.JoinHostPort(t.ListenHost, strconv.Itoa(t.ListenPort))
		listenTCP := func() (net.Listener, error) { return net.Listen("tcp", address) }
		if t.EntryTargetID != "" {
			target, e := m.app.store.Repository().GetSSHTarget(ctx, t.EntryTargetID)
			if e != nil {
				return e
			}
			client, e := m.app.openTargetSSHClient(ctx, target)
			if e != nil {
				return e
			}
			r.closers = append(r.closers, client)
			listenTCP = func() (net.Listener, error) { return client.Listen("tcp", address) }
		}
		dials := map[string]forward.DialFunc{}
		groups := map[string][]protocol.ForwardRoute{}
		destination := forward.Location{ID: "port", ExitTargetID: t.ExitTargetID, Host: t.DestinationHost, Port: t.DestinationPort}
		if t.Protocol == "http" {
			for _, s := range t.HTTP.Servers {
				for _, l := range s.Locations {
					groups[l.ExitTargetID] = append(groups[l.ExitTargetID], protocol.ForwardRoute{ID: l.ID, Network: "tcp", Address: l.Address()})
				}
			}
		} else {
			for _, network := range t.Transports {
				groups[t.ExitTargetID] = append(groups[t.ExitTargetID], protocol.ForwardRoute{ID: network, Network: network, Address: destination.Address()})
			}
		}
		for target, routes := range groups {
			dial, closers, e := m.serverDial(ctx, target, routes)
			if e != nil {
				return e
			}
			r.closers = append(r.closers, closers...)
			for _, route := range routes {
				dials[route.ID] = dial
			}
		}
		dial := func(ctx context.Context, id string) (net.Conn, error) {
			fn := dials[id]
			if fn == nil {
				return nil, errors.New("route unavailable")
			}
			return fn(ctx, id)
		}
		if t.Protocol == "port" {
			r.runtime, err = forward.StartPort(ctx, address, t.Transports, destination, dial, logs, nil, listenTCP)
		} else {
			listener, e := listenTCP()
			if e != nil {
				return e
			}
			r.runtime, err = forward.Start(ctx, listener, t.HTTP, nil, dial, logs, nil)
			if err != nil {
				listener.Close()
			}
		}
		return err
	}
	if err := start(); err != nil {
		r.err = err.Error()
		r.close()
	}
	m.mu.Lock()
	m.runs[t.ID] = r
	m.mu.Unlock()
}
