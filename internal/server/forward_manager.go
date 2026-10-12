package server

import (
	"context"
	"errors"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"io"
	"path/filepath"
	"sync"
	"time"
)

func edgeManaged(t store.Tunnel) bool { return t.Protocol == "http" || t.Protocol == "tcp" }

type forwardLinkSpec struct {
	peer      protocol.ForwardPeer
	exitAgent string
	targetID  string
}
type forwardPlan struct {
	deployment protocol.ForwardDeployment
	entryAgent string
	links      map[string]forwardLinkSpec
}
type forwardServerRun struct {
	revision int64
	runtime  *forward.Runtime
	cancel   context.CancelFunc
	closers  []io.Closer
	err      string
}

func (r *forwardServerRun) close() {
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

type forwardObservation struct {
	report     protocol.ForwardReport
	lastReport time.Time
	agentID    string
}
type edgeForwardManager struct {
	app         *App
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	wake        chan struct{}
	mu          sync.Mutex
	plans       map[string]forwardPlan
	runs        map[string]*forwardServerRun
	reports     map[string]forwardObservation
	logs        map[string]*forward.Logs
	trafficAcks map[string]map[string]int64
	acks        map[string]map[string]int64
	lastCleanup time.Time
	links       map[string]bool
	wg          sync.WaitGroup
}

func newEdgeForwardManager(a *App) *edgeForwardManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &edgeForwardManager{app: a, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), plans: map[string]forwardPlan{}, runs: map[string]*forwardServerRun{}, reports: map[string]forwardObservation{}, logs: map[string]*forward.Logs{}, acks: map[string]map[string]int64{}, trafficAcks: map[string]map[string]int64{}, links: map[string]bool{}}
	go m.loop()
	return m
}
func (m *edgeForwardManager) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *edgeForwardManager) close() {
	m.cancel()
	<-m.done
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		r.close()
	}
	for _, l := range m.logs {
		l.Close()
	}
}
func (m *edgeForwardManager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		m.reconcile()
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
	}
}
func (m *edgeForwardManager) logStore(id string) (*forward.Logs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.logs[id]; l != nil {
		return l, nil
	}
	settings, err := m.app.loadForwardSettings(m.ctx)
	if err != nil {
		return nil, err
	}
	l, err := forward.OpenLogs(filepath.Join(filepath.Dir(m.app.cfg.DatabasePath), "forward-logs"), id, settings.RetentionDays)
	if err == nil {
		m.logs[id] = l
	}
	return l, err
}

// Resolve Agent ancestry without requiring that Agent to be online. Existing direct
// peer links must not be revoked merely because the signaling session is offline.
func (m *edgeForwardManager) reconcile() {
	if m.ctx.Err() != nil {
		return
	}
	repo := m.app.store.Repository()
	_ = repo.ExpireTunnels(m.ctx, time.Now())
	configs, err := repo.ListTunnels(m.ctx, "")
	if err != nil {
		return
	}
	settings, err := m.app.loadForwardSettings(m.ctx)
	if err != nil {
		return
	}
	syncs := map[string]*protocol.ForwardSync{}
	byID := map[string]store.Tunnel{}
	wanted := map[string]bool{}
	for _, agentID := range m.app.registry.OnlineIDs() {
		syncs[agentID] = &protocol.ForwardSync{RetentionDays: settings.RetentionDays, Configs: []protocol.ForwardDeployment{}, KnownIDs: []string{}, DesiredRevisions: map[string]int64{}}
	}
	for _, t := range configs {
		if !edgeManaged(t) {
			continue
		}
		byID[t.ID] = t
		entry, e := m.app.forwardAgentID(m.ctx, t.EntryTargetID)
		if e != nil {
			continue
		}
		if syncs[entry] != nil {
			syncs[entry].KnownIDs = append(syncs[entry].KnownIDs, t.ID)
		}
		if logStore, logErr := m.logStore(t.ID); logErr == nil {
			owners, _ := logStore.GrantedAgents(m.ctx, time.Unix(0, 0))
			for _, owner := range owners {
				if owner != entry && syncs[owner] != nil {
					syncs[owner].KnownIDs = append(syncs[owner].KnownIDs, t.ID)
				}
			}
		}
		if !t.Enabled {
			continue
		}
		actor, e := repo.GetUser(m.ctx, t.OperatorID())
		if e == nil && actor.DisabledAt == nil {
			e = m.app.validateTunnel(m.ctx, actor, t.OrganizationID, t.TunnelConfig)
		} else if e == nil {
			e = errors.New("forward operator disabled")
		}
		if e != nil {
			m.observeError(t, e, entry)
			continue
		}
		m.mu.Lock()
		p, cached := m.plans[t.ID]
		m.mu.Unlock()
		if !cached || p.deployment.Revision != t.Revision {
			p, e = m.build(t)
			if e != nil {
				m.observeError(t, e, entry)
				if s := syncs[entry]; s != nil {
					s.DesiredRevisions[t.ID] = t.Revision
				}
				continue
			}
			m.mu.Lock()
			m.plans[t.ID] = p
			m.mu.Unlock()
		}
		if entry != "" {
			if l, err := m.logStore(t.ID); err == nil {
				_ = l.GrantAgent(m.ctx, entry)
			}
		}
		if entry == "" {
			wanted[t.ID] = true
			m.ensureServerRun(t)
		} else if s := syncs[entry]; s != nil {
			m.mu.Lock()
			previous := m.reports[t.ID]
			m.mu.Unlock()
			if previous.report.Revision == t.Revision && previous.report.Status == "running" && previous.agentID == entry && time.Since(previous.lastReport) < 15*time.Second {
				s.DesiredRevisions[t.ID] = t.Revision
			} else {
				s.Configs = append(s.Configs, p.deployment)
			}
		}
	}
	m.mu.Lock()
	var old []*forwardServerRun
	for id, r := range m.runs {
		t, exists := byID[id]
		if !wanted[id] || !exists || t.Revision != r.revision {
			old = append(old, r)
			delete(m.runs, id)
		}
	}
	for _, l := range m.logs {
		l.SetRetention(settings.RetentionDays)
	}
	m.mu.Unlock()
	for _, r := range old {
		r.close()
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for id, in := range syncs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-m.ctx.Done():
				return
			}
			m.syncAgent(id, in, byID)
		}()
	}
	wg.Wait()
	if time.Since(m.lastCleanup) > time.Hour {
		m.pruneArchived(settings.RetentionDays)
		m.lastCleanup = time.Now()
	}
}
func (m *edgeForwardManager) observeError(t store.Tunnel, err error, agent string) {
	m.mu.Lock()
	m.reports[t.ID] = forwardObservation{report: protocol.ForwardReport{ID: t.ID, Revision: t.Revision, Status: "error", Error: err.Error()}, lastReport: time.Now(), agentID: agent}
	m.mu.Unlock()
}
