package agent

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type edgeRun struct {
	deployment protocol.ForwardDeployment
	runtime    *forward.Runtime
	cancel     context.CancelFunc
	closers    []io.Closer
	err        string
}

func (r *edgeRun) close() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.runtime != nil {
		r.runtime.Close()
	}
	for _, c := range r.closers {
		c.Close()
	}
}

type edgeForwarder struct {
	mu           sync.Mutex
	client       *Client
	dir          string
	runs         map[string]*edgeRun
	logs         map[string]*forward.Logs
	peers        sync.Map
	reportCursor int
	snapshotHash [32]byte
	retention    int
	closed       bool
}

func (c *Client) forwarder() (*edgeForwarder, error) {
	c.forwardMu.Lock()
	defer c.forwardMu.Unlock()
	if c.edge != nil {
		return c.edge, nil
	}
	idPath, err := protocol.ResolveIDFilePath(c.cfg.IDFile)
	if c.cfg.Server == "" && c.cfg.IDFile == "" {
		idPath = filepath.Join(c.cfg.Root, ".gosshd", "embedded-agent.json")
	}
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(c.cfg.Server + "\x00" + c.id))
	dir := filepath.Join(filepath.Dir(idPath), "forwarding", hex.EncodeToString(hash[:8]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	m := &edgeForwarder{client: c, dir: dir, runs: map[string]*edgeRun{}, logs: map[string]*forward.Logs{}, retention: 7}
	c.edge = m
	configs, err := m.restore()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return m, err
	}
	for _, d := range configs {
		m.apply(d)
	}
	// Reopen previous outboxes, including stopped configurations, after an Agent restart.
	entries, _ := os.ReadDir(filepath.Join(dir, "logs"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".sqlite" {
			id := e.Name()[:len(e.Name())-7]
			if m.logs[id] == nil {
				if l, err := forward.OpenLogs(filepath.Join(dir, "logs"), id, m.retention); err == nil {
					m.logs[id] = l
				}
			}
		}
	}
	return m, nil
}
func (c *Client) closeForwarder() {
	c.forwardMu.Lock()
	m := c.edge
	c.forwardMu.Unlock()
	if m != nil {
		m.close()
	}
}
func (m *edgeForwarder) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	for _, r := range m.runs {
		r.close()
	}
	m.peers.Range(func(_, value any) bool { value.(*forward.Peer).Close(); return true })
	for _, l := range m.logs {
		l.Close()
	}
}
func (m *edgeForwarder) apply(d protocol.ForwardDeployment) {
	if old := m.runs[d.ID]; old != nil {
		if old.deployment.Revision == d.Revision && old.runtime != nil && old.runtime.Alive() && old.err == "" {
			return
		}
		old.close()
		delete(m.runs, d.ID)
		for _, route := range old.deployment.Routes {
			if p := m.peer(route.LinkID); p != nil {
				p.Close()
				m.peers.Delete(route.LinkID)
			}
		}
	}
	if d.ExpiresAt > 0 && time.Now().UnixMilli() >= d.ExpiresAt {
		return
	}
	r := &edgeRun{deployment: d}
	m.runs[d.ID] = r
	l := m.logs[d.ID]
	if l == nil {
		var err error
		l, err = forward.OpenLogs(filepath.Join(m.dir, "logs"), d.ID, m.retention)
		if err != nil {
			r.err = err.Error()
			return
		}
		m.logs[d.ID] = l
	}
	ctx, cancel := context.WithCancel(context.Background())
	if d.ExpiresAt > 0 {
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), time.UnixMilli(d.ExpiresAt))
	}
	r.cancel = cancel
	listenSSH, err := openTunnelSSH(ctx, d.ListenHops)
	if err != nil {
		r.err = err.Error()
		return
	}
	listenTCP := func() (net.Listener, error) {
		if listenSSH != nil {
			return listenSSH.Listen("tcp", d.ListenAddress)
		}
		return net.Listen("tcp", d.ListenAddress)
	}
	if listenSSH != nil {
		r.closers = append(r.closers, listenSSH)
	}
	if d.Protocol != "port" && d.Protocol != "http" {
		r.err = "unsupported forwarding protocol"
		r.close()
		return
	}
	if d.Protocol == "port" && d.Destination == nil {
		r.err = "port destination required"
		r.close()
		return
	}
	for _, network := range d.Transports {
		if network == "udp" && listenSSH != nil {
			r.err = "SSH endpoints do not support UDP forwarding"
			r.close()
			return
		}
	}
	local := map[string]forward.DialFunc{}
	links := map[string]string{}
	sshDials := map[string]forward.DialFunc{}
	for _, route := range d.Routes {
		if route.LinkID != "" {
			links[route.ID] = route.LinkID
			continue
		}
		if len(route.Hops) == 0 {
			local[route.ID] = func(c context.Context, _ string) (net.Conn, error) {
				return forward.DialNetwork(c, route.Network, route.Address)
			}
			continue
		}
		if route.Network != "tcp" {
			r.err = "SSH endpoints do not support UDP forwarding"
			r.close()
			return
		}
		keyBytes, _ := json.Marshal(route.Hops)
		key := string(keyBytes)
		base := sshDials[key]
		if base == nil {
			client, e := openTunnelSSH(ctx, route.Hops)
			if e != nil {
				r.err = e.Error()
				r.close()
				return
			}
			r.closers = append(r.closers, client)
			base = func(c context.Context, address string) (net.Conn, error) {
				return client.DialContext(c, "tcp", address)
			}
			sshDials[key] = base
		}
		local[route.ID] = func(c context.Context, _ string) (net.Conn, error) { return base(c, route.Address) }
	}
	dial := func(c context.Context, id string) (net.Conn, error) {
		if fn := local[id]; fn != nil {
			return fn(c, id)
		}
		p := m.peer(links[id])
		if p == nil || !p.Alive() {
			return nil, errors.New("edge exit channel unavailable")
		}
		return p.Dial(c, id)
	}
	direct := func(id string) bool {
		p := m.peer(links[id])
		return links[id] == "" || (p != nil && p.Stable.Direct())
	}
	if d.Protocol == "port" {
		r.runtime, err = forward.StartPort(ctx, d.ListenAddress, d.Transports, *d.Destination, dial, l.TrafficRecorder(), direct, listenTCP)
	} else {
		var listener net.Listener
		listener, err = listenTCP()
		if err == nil {
			r.runtime, err = forward.Start(ctx, listener, d.HTTP, nil, dial, l, direct)
			if err != nil {
				listener.Close()
			}
		}
	}
	if err != nil {
		r.err = err.Error()
		r.close()
	} else {
		context.AfterFunc(ctx, func() {
			for _, closer := range r.closers {
				closer.Close()
			}
			for _, linkID := range links {
				if p := m.peer(linkID); p != nil {
					p.Close()
					m.peers.CompareAndDelete(linkID, p)
				}
			}
		})
	}
}
func (c *Client) handleForwardSync(stream io.ReadWriteCloser, req protocol.StreamRequest) {
	if req.ForwardSync == nil {
		protocol.WriteJSONLine(stream, protocol.ForwardSyncResponse{Error: "forward sync required"})
		return
	}
	m, err := c.forwarder()
	if err != nil {
		protocol.WriteJSONLine(stream, protocol.ForwardSyncResponse{Error: err.Error()})
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	in := req.ForwardSync
	if m.closed {
		return
	}
	if in.RetentionDays >= 1 && in.RetentionDays <= 365 {
		m.retention = in.RetentionDays
	}
	for _, l := range m.logs {
		l.SetRetention(m.retention)
	}
	if !in.ReportOnly {
		wanted := map[string]bool{}
		for _, d := range in.Configs {
			wanted[d.ID] = true
		}
		for id, r := range m.runs {
			if !wanted[id] && in.DesiredRevisions[id] != r.deployment.Revision {
				r.close()
				for _, route := range r.deployment.Routes {
					if p := m.peer(route.LinkID); p != nil {
						p.Close()
						m.peers.Delete(route.LinkID)
					}
				}
				delete(m.runs, id)
			}
		}
		for _, d := range in.Configs {
			m.apply(d)
		}
		snapshots := []protocol.ForwardDeployment{}
		for _, run := range m.runs {
			snapshots = append(snapshots, run.deployment)
		}
		sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ID < snapshots[j].ID })
		if err := m.persist(snapshots); err != nil {
			protocol.WriteJSONLine(stream, protocol.ForwardSyncResponse{Error: err.Error()})
			return
		}
	}
	out := protocol.ForwardSyncResponse{OK: true, Reports: []protocol.ForwardReport{}}
	known := map[string]bool{}
	for _, id := range in.KnownIDs {
		known[id] = true
	}
	budget := 2048
	trafficBudget := 2048
	keys := append([]string(nil), in.KnownIDs...)
	sort.Strings(keys)
	if len(keys) > 0 {
		offset := m.reportCursor % len(keys)
		keys = append(keys[offset:], keys[:offset]...)
		m.reportCursor = (offset + 8) % len(keys)
	}
	for _, id := range keys {
		l := m.logs[id]
		if l == nil {
			out.Reports = append(out.Reports, protocol.ForwardReport{ID: id, Status: "missing"})
			continue
		}
		if !known[id] {
			continue
		}
		if ack := in.Acks[id]; ack > 0 {
			if err := l.Ack(context.Background(), ack); err != nil {
				protocol.WriteJSONLine(stream, protocol.ForwardSyncResponse{Error: err.Error()})
				return
			}
		}
		if ack := in.TrafficAcks[id]; ack > 0 {
			if err := l.AckTraffic(context.Background(), ack); err != nil {
				protocol.WriteJSONLine(stream, protocol.ForwardSyncResponse{Error: err.Error()})
				return
			}
		}
		report := protocol.ForwardReport{Paths: map[string]tunnel.PeerInfo{}, ID: id, Status: "stopped", Links: map[string]string{}}
		if r := m.runs[id]; r != nil {
			report.Revision = r.deployment.Revision
			report.Status = "running"
			report.Error = r.err
			if r.err != "" {
				report.Status = "error"
			}
			if r.runtime != nil {
				report.State = r.runtime.State()
			}
			if r.deployment.ExpiresAt > 0 && time.Now().UnixMilli() >= r.deployment.ExpiresAt {
				report.Status = "expired"
			}
			for _, route := range r.deployment.Routes {
				if route.LinkID != "" {
					state := "missing"
					if p := m.peer(route.LinkID); p != nil && p.Alive() {
						state = "relay"
						report.Paths[route.LinkID] = p.Snapshot()
						if p.Stable.Direct() {
							state = "direct"
						}
					}
					report.Links[route.LinkID] = state
				}
			}
		}
		if trafficBudget > 0 {
			limit := min(trafficBudget, 512)
			report.Traffic, err = l.PendingTraffic(context.Background(), limit)
			if err != nil {
				report.Error = err.Error()
			}
			if len(report.Traffic) == limit {
				out.More = true
			}
			trafficBudget -= len(report.Traffic)
		} else {
			out.More = true
		}
		report.Stats, err = l.Stats(context.Background())
		if err != nil {
			report.Error = err.Error()
		}
		if budget > 0 {
			batchLimit := min(budget, 256)
			report.Events, err = l.Pending(context.Background(), batchLimit)
			if len(report.Events) == batchLimit {
				out.More = true
			}
			if err != nil {
				report.Error = err.Error()
			}
			budget -= len(report.Events)
		} else {
			out.More = true
		}
		out.Reports = append(out.Reports, report)
	}
	protocol.WriteJSONLine(stream, out)
}
func (c *Client) handleForwardLink(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	in := req.ForwardPeer
	if in == nil {
		return
	}
	m, err := c.forwarder()
	if err != nil {
		return
	}
	m.mu.Lock()
	r := m.runs[in.ConfigID]
	authorized := false
	if r != nil && r.runtime != nil && r.runtime.Alive() && r.deployment.Revision == in.Revision {
		for _, route := range r.deployment.Routes {
			if route.LinkID == in.LinkID {
				authorized = true
			}
		}
	}
	if old := m.peer(in.LinkID); old != nil && old.Alive() {
		authorized = false
	}
	m.mu.Unlock()
	if !authorized {
		protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: "stale or unauthorized edge link"})
		return
	}
	if protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}) != nil {
		return
	}
	p, err := forward.NewPeer(stream, reader, true, req.STUNServers)
	if err != nil {
		return
	}
	m.mu.Lock()
	r = m.runs[in.ConfigID]
	if m.closed || r == nil || r.deployment.Revision != in.Revision {
		m.mu.Unlock()
		p.Close()
		return
	}
	m.peers.Store(in.LinkID, p)
	m.mu.Unlock()
	<-p.Session.CloseChan()
	m.mu.Lock()
	if m.peer(in.LinkID) == p {
		m.peers.Delete(in.LinkID)
	}
	m.mu.Unlock()
}
func (c *Client) handleForwardPeer(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	in := req.ForwardPeer
	if in == nil {
		return
	}
	m, managerErr := c.forwarder()
	if managerErr != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	if in.ExpiresAt > 0 {
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), time.UnixMilli(in.ExpiresAt))
	}
	defer cancel()
	dials := map[string]forward.DialFunc{}
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	shared := map[string]forward.DialFunc{}
	for _, route := range in.Routes {
		if route.LinkID != "" {
			protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: "nested edge links forbidden"})
			return
		}
		if len(route.Hops) == 0 {
			dials[route.ID] = func(ctx context.Context, _ string) (net.Conn, error) {
				return forward.DialNetwork(ctx, route.Network, route.Address)
			}
		} else {
			if route.Network != "tcp" {
				protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: "SSH endpoints do not support UDP forwarding"})
				return
			}
			keyBytes, _ := json.Marshal(route.Hops)
			base := shared[string(keyBytes)]
			if base == nil {
				client, err := openTunnelSSH(ctx, route.Hops)
				if err != nil {
					protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: err.Error()})
					return
				}
				closers = append(closers, client)
				base = func(ctx context.Context, a string) (net.Conn, error) { return client.DialContext(ctx, "tcp", a) }
				shared[string(keyBytes)] = base
			}
			dials[route.ID] = func(ctx context.Context, _ string) (net.Conn, error) { return base(ctx, route.Address) }
		}
	}
	if protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true}) != nil {
		return
	}
	p, err := forward.NewPeer(stream, reader, false, req.STUNServers)
	if err != nil {
		return
	}
	defer p.Close()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.peers.Store("exit:"+in.LinkID, p)
	m.mu.Unlock()
	defer m.peers.CompareAndDelete("exit:"+in.LinkID, p)
	p.Serve(ctx, func(ctx context.Context, id string) (net.Conn, error) {
		fn := dials[id]
		if fn == nil {
			return nil, errors.New("unauthorized route")
		}
		return fn(ctx, id)
	})
}

// Delegated SSH credentials are encrypted at rest with a per-Agent local key.
func (m *edgeForwarder) box() (cipher.AEAD, error) {
	keyPath := filepath.Join(m.dir, "snapshot.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = os.WriteFile(keyPath, key, 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (m *edgeForwarder) persist(configs []protocol.ForwardDeployment) error {
	box, err := m.box()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(edgeSnapshot{Configs: configs, RetentionDays: m.retention})
	if err != nil {
		return err
	}
	hash := sha256.Sum256(plain)
	if hash == m.snapshotHash {
		return nil
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	data := box.Seal(nonce, nonce, plain, nil)
	tmp := filepath.Join(m.dir, "rules.enc.tmp")
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(m.dir, "rules.enc")); err != nil {
		return err
	}
	m.snapshotHash = hash
	return nil
}
func (m *edgeForwarder) restore() ([]protocol.ForwardDeployment, error) {
	data, err := os.ReadFile(filepath.Join(m.dir, "rules.enc"))
	if err != nil {
		return nil, err
	}
	box, err := m.box()
	if err != nil {
		return nil, err
	}
	if len(data) < box.NonceSize() {
		return nil, fmt.Errorf("invalid edge snapshot")
	}
	plain, err := box.Open(nil, data[:box.NonceSize()], data[box.NonceSize():], nil)
	if err != nil {
		return nil, err
	}
	var snapshot edgeSnapshot
	err = json.Unmarshal(plain, &snapshot)
	if err != nil {
		return nil, err
	}
	if snapshot.RetentionDays >= 1 && snapshot.RetentionDays <= 365 {
		m.retention = snapshot.RetentionDays
	}
	m.snapshotHash = sha256.Sum256(plain)
	return snapshot.Configs, nil
}

func (m *edgeForwarder) peer(id string) *forward.Peer {
	value, ok := m.peers.Load(id)
	if !ok {
		return nil
	}
	return value.(*forward.Peer)
}
