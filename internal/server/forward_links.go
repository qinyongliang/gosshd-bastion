package server

import (
	"bufio"
	"context"
	"errors"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"io"
	"strings"
	"sync"
	"time"
)

func (m *edgeForwardManager) startLink(entry string, spec forwardLinkSpec) {
	m.mu.Lock()
	if m.links[spec.peer.LinkID] {
		m.mu.Unlock()
		return
	}
	m.links[spec.peer.LinkID] = true
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); delete(m.links, spec.peer.LinkID); m.mu.Unlock() }()
		m.serveLink(entry, spec)
	}()
}
func (m *edgeForwardManager) openPeerStream(agent string, kind string, peer protocol.ForwardPeer) (*yamux.Stream, *bufio.Reader, error) {
	session, err := m.app.registry.Get(agent)
	if err != nil {
		return nil, nil, err
	}
	s, err := session.OpenStream()
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(m.ctx, func() { s.Close() })
	defer stop()
	_ = s.SetDeadline(time.Now().Add(15 * time.Second))
	servers := []string{}
	for _, v := range strings.Split(m.app.cfg.TunnelSTUNServers, ",") {
		if v = strings.TrimSpace(v); v != "" {
			servers = append(servers, v)
		}
	}
	err = protocol.WriteJSONLine(s, protocol.StreamRequest{Type: kind, ForwardPeer: &peer, STUNServers: servers})
	r := bufio.NewReader(s)
	if err == nil {
		var response protocol.StreamResponse
		response, err = protocol.ReadJSONLine[protocol.StreamResponse](r)
		if err == nil && !response.OK {
			err = errors.New(response.Error)
		}
	}
	if err != nil {
		s.Close()
		return nil, nil, err
	}
	_ = s.SetDeadline(time.Time{})
	return s, r, nil
}
func (m *edgeForwardManager) serveLink(entry string, spec forwardLinkSpec) {
	in, reader, err := m.openPeerStream(entry, protocol.StreamForwardLink, spec.peer)
	if err != nil {
		return
	}
	defer in.Close()
	stop := context.AfterFunc(m.ctx, func() { in.Close() })
	defer stop()
	if spec.exitAgent != "" {
		out, outReader, err := m.openPeerStream(spec.exitAgent, protocol.StreamForwardPeer, spec.peer)
		if err != nil {
			return
		}
		defer out.Close()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = io.Copy(out, reader); out.Close(); in.Close() }()
		go func() { defer wg.Done(); _, _ = io.Copy(in, outReader); in.Close(); out.Close() }()
		wg.Wait()
		return
	}
	p, err := forward.NewPeer(in, reader, false, nil)
	if err != nil {
		return
	}
	defer p.Close()
	dial, closers, err := m.serverDial(m.ctx, spec.targetID, spec.peer.Routes)
	if err != nil {
		return
	}
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	p.Serve(m.ctx, dial)
}
