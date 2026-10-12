package server

import (
	"context"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"sort"
)

func forwardTraffic(s forward.Stats) store.TunnelTraffic {
	return store.TunnelTraffic{RelayUp: s.Up - s.DirectUp, RelayDown: s.Down - s.DirectDown, DirectUp: s.DirectUp, DirectDown: s.DirectDown, ConnectionsOpened: s.Requests, PeakConnections: s.Peak}
}
func (a *App) forwardTrafficStatistics(ctx context.Context, t store.Tunnel, from, to int64, source string) (tunnelTrafficStatistics, error) {
	out := tunnelTrafficStatistics{IntervalSeconds: 300, Buckets: []store.TunnelTraffic{}, Sources: []tunnelSourceTraffic{}}
	l, err := a.forwards.logStore(t.ID)
	if err != nil {
		return out, err
	}
	buckets, sources, err := l.Traffic(ctx, from, to, source)
	if err != nil {
		return out, err
	}
	values := map[int64]store.TunnelTraffic{}
	for _, b := range buckets {
		v := forwardTraffic(b.Stats)
		v.BucketStart = b.Bucket
		v.SourceIP = source
		values[b.Bucket] = v
	}
	for bucket := from; bucket < to; bucket += 300 {
		v := values[bucket]
		v.BucketStart = bucket
		v.SourceIP = source
		out.Buckets = append(out.Buckets, v)
	}
	active := map[string]int64{}
	a.forwards.mu.Lock()
	if r := a.forwards.runs[t.ID]; r != nil && r.runtime != nil {
		active = r.runtime.State().Sources
	} else {
		active = a.forwards.reports[t.ID].report.State.Sources
	}
	a.forwards.mu.Unlock()
	seen := map[string]bool{}
	for _, s := range sources {
		v := forwardTraffic(s.Stats)
		v.SourceIP = s.SourceIP
		out.Sources = append(out.Sources, tunnelSourceTraffic{TunnelTraffic: v, ActiveConnections: int(active[s.SourceIP])})
		seen[s.SourceIP] = true
	}
	for ip, n := range active {
		if source == "" || source == ip {
			out.ActiveConnections += int(n)
			if !seen[ip] {
				out.Sources = append(out.Sources, tunnelSourceTraffic{TunnelTraffic: store.TunnelTraffic{SourceIP: ip}, ActiveConnections: int(n)})
			}
		}
	}
	sort.Slice(out.Sources, func(i, j int) bool { return out.Sources[i].SourceIP < out.Sources[j].SourceIP })
	return out, nil
}
