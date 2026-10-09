package server

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

// SSH forwards belong to the client's connection, never to the persistent reconciler.
// SSH does not disclose a -L/-D listener, nor the destination behind -R.
type temporaryTunnel struct {
	workers                        sync.WaitGroup
	done                           chan struct{}
	config                         store.Tunnel
	direction, source, fingerprint string
	metrics                        *tunnelMetrics
	mu                             sync.Mutex
	resources                      map[io.Closer]bool
	stopped                        bool
}

func (t *temporaryTunnel) track(c io.Closer) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		_ = c.Close()
		return false
	}
	t.resources[c] = true
	return true
}
func (t *temporaryTunnel) untrack(c io.Closer) { t.mu.Lock(); delete(t.resources, c); t.mu.Unlock() }
func (t *temporaryTunnel) stop() {
	t.mu.Lock()
	t.stopped = true
	items := make([]io.Closer, 0, len(t.resources))
	for c := range t.resources {
		items = append(items, c)
	}
	t.resources = map[io.Closer]bool{}
	t.mu.Unlock()
	for _, c := range items {
		_ = c.Close()
	}
}
func (a *App) registerSSHForward(userID, fingerprint string, target store.SSHTarget, source, direction, host string, port int) (*temporaryTunnel, func()) {
	now := time.Now().UTC()
	t := &temporaryTunnel{config: store.Tunnel{ID: "ssh-" + uuid.NewString(), OrganizationID: organizationIDForTarget(target), CreatedBy: userID, EnabledBy: userID, Enabled: true, CreatedAt: now, UpdatedAt: now, TunnelConfig: store.TunnelConfig{Name: "SSH " + direction, ExitTargetID: target.ID}}, direction: direction, source: source, fingerprint: fingerprint, done: make(chan struct{}), resources: map[io.Closer]bool{}}
	if direction == "remote" {
		t.config.ListenHost = host
		t.config.ListenPort = port
		t.config.ExitTargetID = ""
	} else {
		t.config.DestinationHost = host
		t.config.DestinationPort = port
	}
	t.metrics = newTunnelMetrics(t.config.ID, t.config.OrganizationID)
	a.tunnels.mu.Lock()
	if a.tunnels.closing {
		a.tunnels.mu.Unlock()
		t.stopped = true
		close(t.done)
		return t, func() {}
	}
	a.tunnels.temporary[t.config.ID] = t
	a.tunnels.metrics[t.config.ID] = t.metrics
	a.tunnels.mu.Unlock()
	var once sync.Once
	return t, func() {
		once.Do(func() {
			t.stop()
			t.workers.Wait()
			t.metrics.flush(a.audit.Repository(), true)
			a.tunnels.mu.Lock()
			delete(a.tunnels.temporary, t.config.ID)
			a.tunnels.mu.Unlock()
			close(t.done)
		})
	}
}
func (a *App) temporaryTunnel(ctx context.Context, actor store.User, id string) (*temporaryTunnel, error) {
	a.tunnels.mu.Lock()
	t := a.tunnels.temporary[id]
	a.tunnels.mu.Unlock()
	if t == nil {
		return nil, store.ErrNotFound
	}
	if err := a.requireOrganizationAdmin(ctx, t.config.OrganizationID, actor); err != nil {
		return nil, errors.New("organization admin required")
	}
	return t, nil
}
func (a *App) tunnelView(t store.Tunnel) apiTunnel {
	out := apiTunnel{Tunnel: t, tunnelStatus: a.tunnelAPIStatus(t), Source: "managed"}
	if user, err := a.store.Repository().GetUser(context.Background(), t.CreatedBy); err == nil {
		out.CreatorName = user.DisplayName
		if out.CreatorName == "" {
			out.CreatorName = user.Email
		}
	}
	a.tunnels.mu.Lock()
	temporary := a.tunnels.temporary[t.ID]
	a.tunnels.mu.Unlock()
	if temporary != nil {
		out.Temporary = true
		out.Source = "ssh"
		out.ForwardType = temporary.direction
		out.RemoteAddress = temporary.source
		out.PublicKeyFingerprint = temporary.fingerprint
		out.Status = "running"
		out.Transport = "relay"
		out.Error = ""
		if temporary.direction == "remote" {
			out.ListenAddress = net.JoinHostPort(t.ListenHost, strconv.Itoa(t.ListenPort))
		}
	}
	return out
}
func (a *App) listTunnelViews(ctx context.Context, actor store.User, org string) ([]apiTunnel, error) {
	if org == "" {
		return nil, errors.New("organization_id required")
	}
	if err := a.requireOrganizationAdmin(ctx, org, actor); err != nil {
		return nil, err
	}
	rows, err := a.store.Repository().ListTunnels(ctx, org)
	if err != nil {
		return nil, err
	}
	a.tunnels.mu.Lock()
	for _, t := range a.tunnels.temporary {
		if t.config.OrganizationID == org {
			rows = append(rows, t.config)
		}
	}
	a.tunnels.mu.Unlock()
	out := make([]apiTunnel, 0, len(rows))
	for _, t := range rows {
		out = append(out, a.tunnelView(t))
	}
	return out, nil
}
func (a *App) lookupTunnel(ctx context.Context, actor store.User, id string) (store.Tunnel, error) {
	if len(id) > 4 && id[:4] == "ssh-" {
		t, err := a.temporaryTunnel(ctx, actor, id)
		if err != nil {
			return store.Tunnel{}, err
		}
		return t.config, nil
	}
	return a.managedTunnel(ctx, actor, id)
}

type trackedForwardListener struct {
	net.Listener
	tunnel *temporaryTunnel
	finish func()
	once   sync.Once
}

func (l *trackedForwardListener) Close() error {
	err := l.Listener.Close()
	l.tunnel.untrack(l)
	l.once.Do(l.finish)
	return err
}
func (l *trackedForwardListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if !l.tunnel.track(c) {
		return nil, net.ErrClosed
	}
	return c, nil
}
func bridgeSSHForward(a io.ReadWriteCloser, b io.ReadWriteCloser, t *temporaryTunnel) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		_ = a.Close()
		_ = b.Close()
		return
	}
	t.workers.Add(1)
	t.mu.Unlock()
	defer t.workers.Done()
	if !t.track(a) {
		_ = b.Close()
		return
	}
	if !t.track(b) {
		_ = a.Close()
		return
	}
	defer t.untrack(a)
	defer t.untrack(b)
	t.metrics.opened()
	defer t.metrics.closed()
	var wg sync.WaitGroup
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = a.Close(); _ = b.Close() }) }
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(trafficWriter{Writer: b, metrics: t.metrics, up: true}, a)
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(trafficWriter{Writer: a, metrics: t.metrics, up: false}, b)
		closeBoth()
	}()
	wg.Wait()
}
