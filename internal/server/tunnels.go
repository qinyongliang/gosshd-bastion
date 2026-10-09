package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	gossh "golang.org/x/crypto/ssh"
)

type tunnelStatus struct {
	Paths             []tunnelConnectionPath `json:"paths"`
	Traffic           store.TunnelTraffic    `json:"traffic"`
	Transport         string                 `json:"transport"`
	DirectConnections int                    `json:"direct_connections"`
	Status            string                 `json:"status"`
	Error             string                 `json:"error,omitempty"`
	ListenAddress     string                 `json:"listen_address,omitempty"`
	Connections       int                    `json:"connections"`
}
type tunnelRun struct {
	peerExit     *tunnelAgentEndpoint
	peerEnabled  bool
	metrics      *tunnelMetrics
	workers      sync.WaitGroup
	config       store.Tunnel
	token        string
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	retryAt      time.Time
	mu           sync.Mutex
	state        tunnelStatus
	resources    []io.Closer
	entryAgent   string
	entrySession *yamux.Session
	dial         func(context.Context) (net.Conn, error)
}

func (r *tunnelRun) track(c io.Closer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil {
		_ = c.Close()
		return false
	}
	r.resources = append(r.resources, c)
	return true
}
func (r *tunnelRun) untrack(c io.Closer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, item := range r.resources {
		if item == c {
			r.resources = append(r.resources[:i], r.resources[i+1:]...)
			return
		}
	}
}
func (r *tunnelRun) closeResources() {
	r.mu.Lock()
	items := r.resources
	r.resources = nil
	r.mu.Unlock()
	for _, c := range items {
		_ = c.Close()
	}
}
func (r *tunnelRun) snapshot() tunnelStatus { r.mu.Lock(); defer r.mu.Unlock(); return r.state }
func (r *tunnelRun) setError(err error) {
	r.mu.Lock()
	r.state.Status = "error"
	r.state.Error = err.Error()
	r.mu.Unlock()
}

type tunnelManager struct {
	closing   bool
	temporary map[string]*temporaryTunnel
	blocked   map[string]string
	metrics   map[string]*tunnelMetrics
	app       *App
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	wake      chan struct{}
	mu        sync.Mutex
	runs      map[string]*tunnelRun
}

func newTunnelManager(a *App) *tunnelManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &tunnelManager{temporary: map[string]*temporaryTunnel{}, metrics: map[string]*tunnelMetrics{}, blocked: map[string]string{}, app: a, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), runs: map[string]*tunnelRun{}}
	go m.loop()
	return m
}
func (m *tunnelManager) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *tunnelManager) close() {
	m.mu.Lock()
	m.closing = true
	items := make([]*temporaryTunnel, 0, len(m.temporary))
	for _, t := range m.temporary {
		items = append(items, t)
	}
	m.mu.Unlock()
	for _, t := range items {
		t.stop()
	}
	for _, t := range items {
		<-t.done
	}
	m.cancel()
	<-m.done
}
func (m *tunnelManager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		m.reconcile()
		select {
		case <-m.ctx.Done():
			m.mu.Lock()
			runs := make([]*tunnelRun, 0, len(m.runs))
			for _, r := range m.runs {
				r.cancel()
				runs = append(runs, r)
			}
			m.mu.Unlock()
			for _, r := range runs {
				<-r.done
				r.workers.Wait()
			}
			m.flushMetrics(true)
			return
		case <-m.wake:
		case <-ticker.C:
		}
	}
}
func (m *tunnelManager) reconcile() {
	if m.ctx.Err() != nil {
		return
	}
	repo := m.app.store.Repository()
	m.flushMetrics(false)
	if repo.ExpireTunnels(m.ctx, time.Now()) != nil {
		return
	}
	configs, err := repo.ListTunnels(m.ctx, "")
	if err != nil {
		return
	}
	wanted := map[string]store.Tunnel{}
	blocked := map[string]string{}
	for _, t := range configs {
		if t.Enabled {
			user, e := repo.GetUser(m.ctx, t.OperatorID())
			if e == nil && user.DisabledAt == nil {
				e = m.app.validateTunnel(m.ctx, user, t.OrganizationID, t.TunnelConfig)
			} else if e == nil {
				e = errors.New("tunnel operator is disabled")
			}
			if e != nil {
				blocked[t.ID] = e.Error()
				m.mu.Lock()
				r := m.runs[t.ID]
				if r != nil {
					r.setError(e)
					r.cancel()
				}
				m.mu.Unlock()
				continue
			}
			wanted[t.ID] = t
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked = blocked
	for id, r := range m.runs {
		t, exists := wanted[id]
		if !exists || t.Revision != r.config.Revision {
			r.cancel()
		}
		select {
		case <-r.done:
			// Keep failed status visible for a short retry interval.
			if exists && t.Revision == r.config.Revision && time.Now().Before(r.retryAt) {
				continue
			}
			r.workers.Wait()
			r.metrics.flush(m.app.audit.Repository(), true)
			delete(m.runs, id)
		default:
		}
	}
	for id, t := range wanted {
		if _, exists := m.runs[id]; exists {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		if t.ExpiresAt != nil {
			cancel()
			ctx, cancel = context.WithDeadline(m.ctx, *t.ExpiresAt)
		}
		r := &tunnelRun{config: t, token: uuid.NewString(), ctx: ctx, cancel: cancel, done: make(chan struct{}), state: tunnelStatus{Status: "starting"}}
		if m.metrics[id] == nil {
			m.metrics[id] = newTunnelMetrics(id, t.OrganizationID)
		}
		r.metrics = m.metrics[id]
		m.runs[id] = r
		go m.start(r)
	}
}
func (m *tunnelManager) status(t store.Tunnel) tunnelStatus {
	if !t.Enabled {
		if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
			return tunnelStatus{Status: "expired"}
		}
		return tunnelStatus{Status: "stopped"}
	}
	m.mu.Lock()
	r := m.runs[t.ID]
	blocked := m.blocked[t.ID]
	m.mu.Unlock()
	if blocked != "" {
		return tunnelStatus{Status: "error", Error: blocked}
	}
	if r == nil || r.config.Revision != t.Revision {
		return tunnelStatus{Status: "starting"}
	}
	return r.snapshot()
}
func (m *tunnelManager) stop(id string) {
	m.mu.Lock()
	r := m.runs[id]
	if r != nil {
		r.cancel()
	}
	metrics := m.metrics[id]
	m.mu.Unlock()
	if r != nil {
		<-r.done
		r.workers.Wait()
	}
	if metrics != nil {
		metrics.flush(m.app.audit.Repository(), true)
	}
	m.notify()
}

func (m *tunnelManager) start(r *tunnelRun) {
	defer func() { r.retryAt = time.Now().Add(3 * time.Second); close(r.done) }()
	defer r.cancel()
	defer r.closeResources()
	closeDone := make(chan struct{})
	defer close(closeDone)
	go func() {
		select {
		case <-r.ctx.Done():
			r.closeResources()
		case <-closeDone:
		}
	}()
	user, err := m.app.store.Repository().GetUser(r.ctx, r.config.OperatorID())
	if err == nil && user.DisabledAt != nil {
		err = errors.New("tunnel operator is disabled")
	}
	if err == nil {
		err = m.app.validateTunnel(r.ctx, user, r.config.OrganizationID, r.config.TunnelConfig)
	}
	if err != nil {
		r.setError(err)
		return
	}
	entryEndpoint, e := m.app.tunnelAgentEndpoint(r.ctx, r.config.EntryTargetID)
	if e != nil {
		r.setError(e)
		return
	}
	exitEndpoint, e := m.app.tunnelAgentEndpoint(r.ctx, r.config.ExitTargetID)
	if e != nil {
		r.setError(e)
		return
	}
	r.mu.Lock()
	r.peerExit = exitEndpoint
	r.peerEnabled = entryEndpoint != nil && exitEndpoint != nil
	r.mu.Unlock()
	// A single SSH client per endpoint is shared by all connections in this run.
	destination := net.JoinHostPort(r.config.DestinationHost, strconv.Itoa(r.config.DestinationPort))
	var exitSession *yamux.Session
	if r.peerEnabled {
		exitSession = exitEndpoint.session
	} else if r.config.ExitTargetID == "" {
		r.dial = func(ctx context.Context) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", destination)
		}
	} else {
		target, e := m.app.store.Repository().GetSSHTarget(r.ctx, r.config.ExitTargetID)
		if e != nil {
			r.setError(e)
			return
		}
		if target.TargetType == store.TargetAgent {
			exitSession, e = m.app.registry.Get(target.AgentID)
			if e != nil {
				r.setError(e)
				return
			}
			r.dial = func(ctx context.Context) (net.Conn, error) { return dialTunnelAgent(ctx, exitSession, destination) }
		} else {
			client, e := m.app.openTargetSSHClient(r.ctx, target)
			if e != nil {
				r.setError(e)
				return
			}
			if !r.track(client) {
				return
			}
			r.dial = func(ctx context.Context) (net.Conn, error) { return client.DialContext(ctx, "tcp", destination) }
			go func() { _ = client.Wait(); r.cancel() }()
		}
	}
	address := net.JoinHostPort(r.config.ListenHost, strconv.Itoa(r.config.ListenPort))
	var listener net.Listener
	var control *yamux.Stream
	var entrySession *yamux.Session
	if r.config.EntryTargetID == "" {
		listener, err = net.Listen("tcp", address)
	} else {
		target, e := m.app.store.Repository().GetSSHTarget(r.ctx, r.config.EntryTargetID)
		if e != nil {
			r.setError(e)
			return
		}
		if entryEndpoint != nil {
			entrySession, err = m.app.registry.Get(entryEndpoint.id)
			if err == nil {
				r.mu.Lock()
				r.entryAgent = entryEndpoint.id
				r.entrySession = entrySession
				r.mu.Unlock()
				control, err = entrySession.OpenStream()
				if err == nil {
					if !r.track(control) {
						return
					}
					_ = control.SetDeadline(time.Now().Add(10 * time.Second))
					err = protocol.WriteJSONLine(control, protocol.StreamRequest{Type: protocol.StreamTunnelListen, TunnelID: r.token, Target: address, TunnelHops: entryEndpoint.hops, Peer: r.peerEnabled})
					if err == nil {
						response, e := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(control))
						err = e
						if err == nil && !response.OK {
							err = errors.New(response.Error)
						}
						if err == nil {
							address = response.ListenAddress
						}
					}
					_ = control.SetDeadline(time.Time{})
				}
			}
		} else {
			var client *gossh.Client
			client, err = m.app.openTargetSSHClient(r.ctx, target)
			if err == nil {
				if !r.track(client) {
					return
				}
				listener, err = client.Listen("tcp", address)
				go func() { _ = client.Wait(); r.cancel() }()
			}
		}
	}
	if err != nil {
		r.setError(err)
		return
	}
	if listener != nil {
		if !r.track(listener) {
			return
		}
		address = listener.Addr().String()
	}
	if r.ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	r.state.Status = "running"
	r.state.ListenAddress = address
	r.mu.Unlock()
	if exitSession != nil {
		go func() {
			select {
			case <-exitSession.CloseChan():
				r.setError(ErrAgentOffline)
				r.cancel()
			case <-r.ctx.Done():
			}
		}()
	}
	if entrySession != nil {
		go func() {
			select {
			case <-entrySession.CloseChan():
				r.setError(ErrAgentOffline)
				r.cancel()
			case <-r.ctx.Done():
			}
		}()
	}
	if control != nil {
		go func() {
			_, err := io.Copy(io.Discard, control)
			if r.ctx.Err() == nil {
				if err == nil {
					err = io.EOF
				}
				r.setError(err)
			}
			r.cancel()
		}()
		<-r.ctx.Done()
		return
	}
	for {
		conn, e := listener.Accept()
		if e != nil {
			if r.ctx.Err() == nil {
				r.setError(e)
			}
			return
		}
		r.workers.Add(1)
		go func() { defer r.workers.Done(); m.forward(r, conn) }()
	}
}

func dialTunnelAgent(ctx context.Context, session *yamux.Session, destination string) (net.Conn, error) {
	stream, err := session.OpenStream()
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	_ = stream.SetDeadline(time.Now().Add(10 * time.Second))
	err = protocol.WriteJSONLine(stream, protocol.StreamRequest{Type: protocol.StreamTCP, Target: destination})
	reader := bufio.NewReader(stream)
	if err == nil {
		response, e := protocol.ReadJSONLine[protocol.StreamResponse](reader)
		err = e
		if err == nil && !response.OK {
			err = errors.New(response.Error)
		}
	}
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	_ = stream.SetDeadline(time.Time{})
	return &tunnelStreamConn{readWriteConn: readWriteConn{Reader: reader, Writer: stream, Closer: stream, remoteAddr: dummyAddr(destination)}, stream: stream}, nil
}
func (m *tunnelManager) forward(r *tunnelRun, source net.Conn) {
	defer source.Close()
	if !r.track(source) {
		return
	}
	defer r.untrack(source)
	destination, err := r.dial(r.ctx)
	if err != nil {
		r.mu.Lock()
		r.state.Error = err.Error()
		r.mu.Unlock()
		return
	}
	if !r.track(destination) {
		return
	}
	defer destination.Close()
	defer r.untrack(destination)
	r.mu.Lock()
	r.metrics.opened()
	r.state.Connections++
	r.state.Error = ""
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.metrics.closed()
		r.state.Connections--
		r.mu.Unlock()
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	copySide := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(trafficWriter{Writer: dst, metrics: r.metrics, up: dst == destination}, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = destination.Close()
			_ = source.Close()
		}
	}
	go copySide(destination, source)
	go copySide(source, destination)
	wg.Wait()
}
func (a *App) serveAgentTunnelConnections(agentID string, session *yamux.Session) {
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			return
		}
		go a.acceptAgentTunnelConnection(agentID, session, stream)
	}
}
func (a *App) acceptAgentTunnelConnection(agentID string, session *yamux.Session, stream *yamux.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(stream)
	request, err := protocol.ReadJSONLine[protocol.StreamRequest](reader)
	if err != nil || request.Type != protocol.StreamTunnelConnection {
		return
	}
	m := a.tunnels
	if m == nil {
		return
	}
	m.mu.Lock()
	var run *tunnelRun
	for _, r := range m.runs {
		r.mu.Lock()
		matches := r.token == request.TunnelID && r.entryAgent == agentID && r.entrySession == session && r.ctx.Err() == nil
		r.mu.Unlock()
		if matches {
			run = r
			r.workers.Add(1)
			break
		}
	}
	m.mu.Unlock()
	if run == nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: "inactive tunnel"})
		return
	}
	defer run.workers.Done()
	if request.Peer && run.peerEnabled {
		m.forwardPeer(run, stream, reader)
		return
	}
	if run.peerEnabled {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: "upgrade entry Agent for P2P-capable tunnels"})
		return
	}
	if err = protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}); err != nil {
		return
	}
	_ = stream.SetDeadline(time.Time{})
	m.forward(run, &tunnelStreamConn{readWriteConn: readWriteConn{Reader: reader, Writer: stream, Closer: stream, remoteAddr: dummyAddr(fmt.Sprintf("agent:%s", agentID))}, stream: stream})
}

type tunnelStreamConn struct {
	readWriteConn
	stream *yamux.Stream
}

func (c *tunnelStreamConn) CloseWrite() error { return c.stream.Close() }
func (c *tunnelStreamConn) Close() error {
	_ = c.stream.SetReadDeadline(time.Now())
	return c.stream.Close()
}
func (c *tunnelStreamConn) SetDeadline(t time.Time) error      { return c.stream.SetDeadline(t) }
func (c *tunnelStreamConn) SetReadDeadline(t time.Time) error  { return c.stream.SetReadDeadline(t) }
func (c *tunnelStreamConn) SetWriteDeadline(t time.Time) error { return c.stream.SetWriteDeadline(t) }
