package server

import (
	"context"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
)

// Buckets are written only at five-minute boundaries or lifecycle transitions.
// Failed writes remain in memory and are retried; counters never enter the main database.
type tunnelConnectionPath struct {
	ID           string           `json:"id"`
	EntryAgentID string           `json:"entry_agent_id"`
	ExitAgentID  string           `json:"exit_agent_id"`
	Entry        *tunnel.PeerInfo `json:"entry,omitempty"`
	Exit         *tunnel.PeerInfo `json:"exit,omitempty"`
	UpdatedAt    time.Time        `json:"updated_at"`
}
type tunnelMetrics struct {
	paths   map[string]tunnelConnectionPath
	mu      sync.Mutex
	id, org string
	buckets map[int64]store.TunnelTraffic
	active  int
	direct  int
}

func newTunnelMetrics(id, org string) *tunnelMetrics {
	return &tunnelMetrics{paths: map[string]tunnelConnectionPath{}, id: id, org: org, buckets: map[int64]store.TunnelTraffic{}}
}
func (m *tunnelMetrics) change(update func(*store.TunnelTraffic)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := time.Now().UTC().Truncate(store.TunnelTrafficInterval).Unix()
	v := m.buckets[bucket]
	v.BucketStart = bucket
	update(&v)
	v.PeakConnections = max(v.PeakConnections, int64(m.active))
	m.buckets[bucket] = v
}
func (m *tunnelMetrics) bytes(up bool, direct bool, n int64) {
	if n <= 0 {
		return
	}
	m.change(func(v *store.TunnelTraffic) {
		if direct {
			if up {
				v.DirectUp += n
			} else {
				v.DirectDown += n
			}
		} else {
			if up {
				v.RelayUp += n
			} else {
				v.RelayDown += n
			}
		}
	})
}
func (m *tunnelMetrics) opened() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active++
	bucket := time.Now().UTC().Truncate(store.TunnelTrafficInterval).Unix()
	v := m.buckets[bucket]
	v.BucketStart = bucket
	v.ConnectionsOpened++
	v.PeakConnections = max(v.PeakConnections, int64(m.active))
	m.buckets[bucket] = v
}
func (m *tunnelMetrics) closed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := time.Now().UTC().Truncate(store.TunnelTrafficInterval).Unix()
	v := m.buckets[bucket]
	v.BucketStart = bucket
	v.PeakConnections = max(v.PeakConnections, int64(m.active))
	m.buckets[bucket] = v
	m.active--
}
func (m *tunnelMetrics) flush(repo *store.AuditRepository, all bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC().Truncate(store.TunnelTrafficInterval).Unix()
	if m.active > 0 {
		v := m.buckets[now]
		v.BucketStart = now
		v.PeakConnections = max(v.PeakConnections, int64(m.active))
		m.buckets[now] = v
	}
	for bucket, v := range m.buckets {
		if !all && bucket >= now {
			continue
		}
		if v.Empty() {
			delete(m.buckets, bucket)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := repo.AddTunnelTraffic(ctx, m.id, m.org, v)
		cancel()
		if err != nil {
			log.Printf("persist tunnel traffic %s: %v", m.id, err)
			continue
		}
		delete(m.buckets, bucket)
	}
}

type trafficWriter struct {
	io.Writer
	metrics *tunnelMetrics
	up      bool
}

func (w trafficWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.metrics.bytes(w.up, false, int64(n))
	return n, err
}
func (m *tunnelManager) flushMetrics(all bool) {
	m.mu.Lock()
	items := make([]*tunnelMetrics, 0, len(m.metrics))
	for _, v := range m.metrics {
		items = append(items, v)
	}
	m.mu.Unlock()
	for _, v := range items {
		v.flush(m.app.audit.Repository(), all)
		if strings.HasPrefix(v.id, "ssh-") {
			m.mu.Lock()
			v.mu.Lock()
			if m.temporary[v.id] == nil && v.active == 0 && len(v.buckets) == 0 {
				delete(m.metrics, v.id)
			}
			v.mu.Unlock()
			m.mu.Unlock()
		}
	}
}
func (m *tunnelManager) traffic(t store.Tunnel) (store.TunnelTraffic, int, int, error) {
	m.mu.Lock()
	metrics := m.metrics[t.ID]
	m.mu.Unlock()
	// Serialize the database snapshot with flush to prevent counting a bucket twice.
	if metrics != nil {
		metrics.mu.Lock()
		defer metrics.mu.Unlock()
	}
	total, err := m.app.audit.Repository().TunnelTrafficTotal(context.Background(), t.ID)
	if err != nil {
		return total, 0, 0, err
	}
	if metrics == nil {
		return total, 0, 0, nil
	}
	for _, v := range metrics.buckets {
		total.RelayUp += v.RelayUp
		total.RelayDown += v.RelayDown
		total.DirectUp += v.DirectUp
		total.DirectDown += v.DirectDown
		total.ConnectionsOpened += v.ConnectionsOpened
		total.PeakConnections = max(total.PeakConnections, v.PeakConnections)
	}
	return total, metrics.active, metrics.direct, nil
}

func (m *tunnelMetrics) pathsSnapshot() []tunnelConnectionPath {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tunnelConnectionPath, 0, len(m.paths))
	for _, path := range m.paths {
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}
