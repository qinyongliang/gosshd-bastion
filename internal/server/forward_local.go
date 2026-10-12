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
	"time"
)

func (m *edgeForwardManager) serverDial(ctx context.Context, targetID string, routes []protocol.ForwardRoute) (forward.DialFunc, []io.Closer, error) {
	allowed := map[string]string{}
	for _, r := range routes {
		allowed[r.ID] = r.Address
	}
	var base func(context.Context, string) (net.Conn, error)
	var closers []io.Closer
	if targetID == "" {
		base = func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
		}
	} else {
		target, err := m.app.store.Repository().GetSSHTarget(ctx, targetID)
		if err != nil {
			return nil, nil, err
		}
		if target.TargetType == store.TargetAgent {
			base = func(ctx context.Context, a string) (net.Conn, error) {
				s, err := m.app.registry.Get(target.AgentID)
				if err != nil {
					return nil, err
				}
				return dialTunnelAgent(ctx, s, a)
			}
		} else {
			client, err := m.app.openTargetSSHClient(ctx, target)
			if err != nil {
				return nil, nil, err
			}
			closers = append(closers, client)
			base = func(ctx context.Context, a string) (net.Conn, error) { return client.DialContext(ctx, "tcp", a) }
		}
	}
	return func(ctx context.Context, id string) (net.Conn, error) {
		a, ok := allowed[id]
		if !ok {
			return nil, errors.New("unauthorized route")
		}
		return base(ctx, a)
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
		var listener net.Listener
		if t.EntryTargetID == "" {
			listener, err = net.Listen("tcp", net.JoinHostPort(t.ListenHost, strconv.Itoa(t.ListenPort)))
		} else {
			target, e := m.app.store.Repository().GetSSHTarget(ctx, t.EntryTargetID)
			if e != nil {
				return e
			}
			client, e := m.app.openTargetSSHClient(ctx, target)
			if e != nil {
				return e
			}
			r.closers = append(r.closers, client)
			listener, err = client.Listen("tcp", net.JoinHostPort(t.ListenHost, strconv.Itoa(t.ListenPort)))
		}
		if err != nil {
			return err
		}
		dials := map[string]forward.DialFunc{}
		groups := map[string][]protocol.ForwardRoute{}
		var tcp *forward.Location
		if t.Protocol == "http" {
			for _, s := range t.HTTP.Servers {
				for _, l := range s.Locations {
					groups[l.ExitTargetID] = append(groups[l.ExitTargetID], protocol.ForwardRoute{ID: l.ID, Address: l.Address()})
				}
			}
		} else {
			tcp = &forward.Location{ID: "tcp", ExitTargetID: t.ExitTargetID, Host: t.DestinationHost, Port: t.DestinationPort}
			groups[t.ExitTargetID] = []protocol.ForwardRoute{{ID: "tcp", Address: tcp.Address()}}
		}
		for target, routes := range groups {
			dial, closers, e := m.serverDial(ctx, target, routes)
			if e != nil {
				listener.Close()
				return e
			}
			r.closers = append(r.closers, closers...)
			for _, route := range routes {
				dials[route.ID] = dial
			}
		}
		r.runtime, err = forward.Start(ctx, listener, t.HTTP, tcp, func(ctx context.Context, id string) (net.Conn, error) {
			fn := dials[id]
			if fn == nil {
				return nil, errors.New("route unavailable")
			}
			return fn(ctx, id)
		}, logs, nil)
		if err != nil {
			listener.Close()
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
