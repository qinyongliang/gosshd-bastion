package server

import (
	"context"
	"io"
	"log"
	"net"
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
	buckets map[tunnelTrafficKey]store.TunnelTraffic
	sources map[string]int
	active  int
	direct  int
}

type tunnelTrafficKey struct {
	bucket   int64
	sourceIP string
}

func tunnelSourceIP(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	// TCP IPv6 addresses may include an interface zone.
	if host, _, ok := strings.Cut(address, "%"); ok {
		address = host
	}
	if ip := net.ParseIP(address); ip != nil {
		return ip.String()
	}
	return "unknown"
}

func newTunnelMetrics(id, org string) *tunnelMetrics {
	return &tunnelMetrics{paths: map[string]tunnelConnectionPath{}, id: id, org: org, buckets: map[tunnelTrafficKey]store.TunnelTraffic{}, sources: map[string]int{}}
}

// Caller holds mu. Keep a separate global peak: per-IP peaks cannot be summed.
func (m *tunnelMetrics) change(sourceIP string, update func(*store.TunnelTraffic)) {
	bucket := time.Now().UTC().Truncate(store.TunnelTrafficInterval).Unix()
	for _, ip := range []string{"", sourceIP} {
		key := tunnelTrafficKey{bucket, ip}
		v := m.buckets[key]
		v.BucketStart, v.SourceIP = bucket, ip
		update(&v)
		active := m.active
		if ip != "" {
			active = m.sources[ip]
		}
		v.PeakConnections = max(v.PeakConnections, int64(active))
		m.buckets[key] = v
	}
}
func (m *tunnelMetrics) bytes(sourceIP string, up bool, direct bool, n int64) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.change(sourceIP, func(v *store.TunnelTraffic) {
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
func (m *tunnelMetrics) opened(sourceIP string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active++
	m.sources[sourceIP]++
	m.change(sourceIP, func(v *store.TunnelTraffic) { v.ConnectionsOpened++ })
}
func (m *tunnelMetrics) closed(sourceIP string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.change(sourceIP, func(*store.TunnelTraffic) {})
	m.active--
	m.sources[sourceIP]--
	if m.sources[sourceIP] == 0 {
		delete(m.sources, sourceIP)
	}
}
func (m *tunnelMetrics) flush(repo *store.AuditRepository, all bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC().Truncate(store.TunnelTrafficInterval).Unix()
	for sourceIP := range m.sources {
		m.change(sourceIP, func(*store.TunnelTraffic) {})
	}
	for bucket, v := range m.buckets {
		if !all && bucket.bucket >= now {
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
	metrics  *tunnelMetrics
	up       bool
	sourceIP string
}

func (w trafficWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.metrics.bytes(w.sourceIP, w.up, false, int64(n))
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
		if v.SourceIP != "" {
			continue
		}
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
