package server

import (
	"bufio"
	"context"
	"fmt"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"io"
	"time"
)

func (m *edgeForwardManager) syncAgent(id string, in *protocol.ForwardSync, configs map[string]store.Tunnel) {
	for round := 0; round < 32 && m.ctx.Err() == nil; round++ {
		request := *in
		if round > 0 {
			request.ReportOnly = true
			request.Configs = nil
			request.DesiredRevisions = nil
		}
		if !m.syncAgentBatch(id, &request, configs) {
			return
		}
	}
}
func (m *edgeForwardManager) syncAgentBatch(id string, in *protocol.ForwardSync, configs map[string]store.Tunnel) bool {
	session, err := m.app.registry.Get(id)
	if err != nil {
		return false
	}
	stream, err := session.OpenStream()
	if err != nil {
		return false
	}
	defer stream.Close()
	stop := context.AfterFunc(m.ctx, func() { stream.Close() })
	defer stop()
	_ = stream.SetDeadline(time.Now().Add(45 * time.Second))
	m.mu.Lock()
	in.TrafficAcks = map[string]int64{}
	for key, value := range m.trafficAcks[id] {
		in.TrafficAcks[key] = value
	}
	in.Acks = map[string]int64{}
	for key, value := range m.acks[id] {
		in.Acks[key] = value
	}
	m.mu.Unlock()
	if protocol.WriteJSONLine(stream, protocol.StreamRequest{Type: protocol.StreamForwardSync, ForwardSync: in}) != nil {
		return false
	}
	out, err := protocol.ReadJSONLine[protocol.ForwardSyncResponse](bufio.NewReader(io.LimitReader(stream, 32<<20)))
	if err != nil || !out.OK {
		if err == nil && out.Error != "" {
			for _, key := range in.KnownIDs {
				if t, ok := configs[key]; ok && t.Enabled {
					current, _ := m.app.forwardAgentID(m.ctx, t.EntryTargetID)
					if current == id {
						m.observeError(t, fmt.Errorf("Agent forwarding sync failed (upgrade Agent if unsupported): %s", out.Error), id)
					}
				}
			}
		}
		return false
	}
	known := map[string]bool{}
	for _, key := range in.KnownIDs {
		known[key] = true
	}
	for _, report := range out.Reports {
		t, exists := configs[report.ID]
		if !exists || !known[report.ID] {
			continue
		}
		l, err := m.logStore(report.ID)
		if err != nil {
			continue
		}
		// An Agent cannot submit logs for another organization's or entry's configuration.
		if len(report.Events) > 256 {
			continue
		}
		if len(report.Events) > 0 {
			err = l.ImportAccess(m.ctx, report.Events)
		}
		if err != nil {
			continue
		}
		if len(report.Traffic) > 512 {
			continue
		}
		if err := l.ImportTelemetry(m.ctx, id, report.Stats, report.Traffic); err != nil {
			continue
		}
		var trafficAck int64
		for _, b := range report.Traffic {
			trafficAck = max(trafficAck, b.Sequence)
		}
		var ack int64
		for _, e := range report.Events {
			ack = max(ack, e.Sequence)
		}
		m.mu.Lock()
		if m.trafficAcks[id] == nil {
			m.trafficAcks[id] = map[string]int64{}
		}
		m.trafficAcks[id][report.ID] = max(m.trafficAcks[id][report.ID], trafficAck)
		if m.acks[id] == nil {
			m.acks[id] = map[string]int64{}
		}
		m.acks[id][report.ID] = max(m.acks[id][report.ID], ack)
		report.Events = nil
		report.Traffic = nil
		p := m.plans[report.ID]
		if p.entryAgent == id {
			m.reports[report.ID] = forwardObservation{report: report, lastReport: time.Now(), agentID: id}
		}
		m.mu.Unlock()
		if !t.Enabled || report.Revision != t.Revision || p.entryAgent != id {
			continue
		}
		for linkID, spec := range p.links {
			if report.Links[linkID] != "missing" {
				continue
			}
			m.startLink(id, spec)
		}
	}
	return out.More
}
