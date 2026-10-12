package server

import (
	"context"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"time"
)

func (m *edgeForwardManager) status(t store.Tunnel) tunnelStatus {
	state := tunnelStatus{Status: "stopped", Transport: "relay", Paths: []tunnelConnectionPath{}}
	if !t.Enabled {
		if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
			state.Status = "expired"
		}
		return state
	}
	m.mu.Lock()
	r := m.runs[t.ID]
	plan := m.plans[t.ID]
	o := m.reports[t.ID]
	m.mu.Unlock()
	var live forward.RuntimeState
	state.Status = "starting"
	if r != nil && r.revision == t.Revision {
		state.Status = "running"
		if r.err != "" {
			state.Status = "error"
			state.Error = r.err
		} else if r.runtime != nil {
			s := r.runtime.State()
			live = s
			state.Connections = int(s.Active)
			state.ListenAddress = s.ListenAddress
		}
	}
	if o.report.Revision == t.Revision {
		for id, info := range o.report.Paths {
			snapshot := info
			state.Paths = append(state.Paths, tunnelConnectionPath{ID: id, EntryAgentID: o.agentID, ExitAgentID: plan.links[id].exitAgent, Entry: &snapshot, UpdatedAt: o.lastReport})
		}
		live = o.report.State
		state.Status = o.report.Status
		state.Error = o.report.Error
		state.Connections = int(o.report.State.Active)
		state.ListenAddress = o.report.State.ListenAddress
		direct, relay, missing := 0, 0, 0
		for _, s := range o.report.Links {
			switch s {
			case "direct":
				direct++
			case "relay":
				relay++
			default:
				missing++
			}
		}
		if (direct > 0 || (o.agentID != "" && len(o.report.Links) == 0)) && relay == 0 && missing == 0 {
			state.Transport = "direct"
			state.DirectConnections = state.Connections
		} else if direct > 0 {
			state.Transport = "mixed"
		} else if missing > 0 {
			state.Transport = "negotiating"
		}
		if time.Since(o.lastReport) > 15*time.Second {
			state.Status = "unknown"
			state.Error = "Agent disconnected; last-reported direct/local paths may continue; runtime is unconfirmed"
		}
	}
	if l, err := m.logStore(t.ID); err == nil {
		if stats, err := l.Stats(context.Background()); err == nil {
			state.Traffic = store.TunnelTraffic{ConnectionsOpened: stats.Requests + live.Active, PeakConnections: max(stats.Peak, live.Peak), RelayUp: stats.Up + live.InflightUp - stats.DirectUp - live.InflightDirectUp, RelayDown: stats.Down + live.InflightDown - stats.DirectDown - live.InflightDirectDown, DirectUp: stats.DirectUp + live.InflightDirectUp, DirectDown: stats.DirectDown + live.InflightDirectDown}
		}
	}
	return state
}
