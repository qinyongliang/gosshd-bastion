package server

import (
	"context"
	"errors"
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	"net/http"
	"strconv"
	"time"
)

type forwardLogsResponse struct {
	Events        []forward.Event       `json:"events"`
	Sources       []forward.SourceStats `json:"sources"`
	Stats         forward.Stats         `json:"stats"`
	Active        int64                 `json:"active"`
	LivePeak      int64                 `json:"live_peak"`
	LastReport    *time.Time            `json:"last_report,omitempty"`
	EdgeStats     *forward.Stats        `json:"edge_stats,omitempty"`
	RetentionDays int                   `json:"retention_days"`
	NextBefore    int64                 `json:"next_before,omitempty"`
}

func (a *App) forwardLogs(ctx context.Context, actor store.User, id string, f forward.LogFilter) (forwardLogsResponse, error) {
	var out forwardLogsResponse
	t, err := a.managedTunnel(ctx, actor, id)
	if err != nil {
		return out, err
	}
	if !edgeManaged(t) {
		return out, tunnelConfigError{errors.New("access logs require port or HTTP forwarding")}
	}
	if f.Limit < 0 || f.Limit > 500 || f.Before < 0 || f.From < 0 || f.To < 0 || (f.From > 0 && f.To > 0 && f.To <= f.From) || f.Status < 0 || f.Status > 599 || (f.Status > 0 && f.Status < 100) || len(f.Path) > 8192 || len(f.SourceIP) > 64 || len(f.RouteID) > 128 {
		return out, tunnelConfigError{errors.New("invalid log filter; times use Unix milliseconds; limit is at most 500")}
	}
	l, err := a.forwards.logStore(id)
	if err != nil {
		return out, err
	}
	out.Events = []forward.Event{}
	out.Sources = []forward.SourceStats{}
	if t.Protocol == "http" {
		out.Events, err = l.Query(ctx, f)
		if err != nil {
			return out, err
		}
		out.Sources, err = l.Sources(ctx, f)
		if err != nil {
			return out, err
		}
	}
	out.Stats, err = l.Stats(ctx)
	if err != nil {
		return out, err
	}
	if len(out.Events) > 0 {
		out.NextBefore = out.Events[len(out.Events)-1].Sequence
	}
	if t.Protocol == "http" {
		settings, err := a.loadForwardSettings(ctx)
		if err != nil {
			return out, err
		}
		out.RetentionDays = settings.RetentionDays
	}
	a.forwards.mu.Lock()
	r := a.forwards.runs[id]
	o := a.forwards.reports[id]
	var live forward.RuntimeState
	if r != nil && r.runtime != nil {
		s := r.runtime.State()
		live = s
		out.Active = s.Active
		out.LivePeak = s.Peak
	}
	if !o.lastReport.IsZero() {
		last := o.lastReport
		out.LastReport = &last
		stats := o.report.Stats
		out.EdgeStats = &stats
		live = o.report.State
		out.Active = o.report.State.Active
		out.LivePeak = o.report.State.Peak
	}
	a.forwards.mu.Unlock()
	out.Stats.Requests += live.Active
	out.Stats.Peak = max(out.Stats.Peak, live.Peak)
	out.Stats.DirectUp += live.InflightDirectUp
	out.Stats.DirectDown += live.InflightDirectDown
	out.Stats.Up += live.InflightUp
	out.Stats.Down += live.InflightDown
	if t.Protocol == "http" {
		seen := map[string]bool{}
		for i := range out.Sources {
			out.Sources[i].Active = live.Sources[out.Sources[i].SourceIP]
			seen[out.Sources[i].SourceIP] = true
		}
		for ip, n := range live.Sources {
			if !seen[ip] && (f.SourceIP == "" || f.SourceIP == ip) {
				out.Sources = append(out.Sources, forward.SourceStats{SourceIP: ip, Active: n})
			}
		}
	}
	return out, nil
}
func (a *App) handleForwardLogs(w http.ResponseWriter, r *http.Request, u store.User) {
	f := forward.LogFilter{SourceIP: r.URL.Query().Get("source_ip"), Path: r.URL.Query().Get("path"), RouteID: r.URL.Query().Get("route_id")}
	for key, dst := range map[string]*int64{"before": &f.Before, "from": &f.From, "to": &f.To} {
		if v := r.URL.Query().Get(key); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeError(w, 400, "invalid "+key)
				return
			}
			*dst = n
		}
	}
	for key, dst := range map[string]*int{"limit": &f.Limit, "status": &f.Status} {
		if v := r.URL.Query().Get(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				writeError(w, 400, "invalid "+key)
				return
			}
			*dst = n
		}
	}
	out, err := a.forwardLogs(r.Context(), u, r.PathValue("id"), f)
	if err != nil {
		if isNotFound(err) || errors.Is(err, store.ErrNotFound) {
			writeError(w, 404, "forward not found")
		} else if _, e := a.managedTunnel(r.Context(), u, r.PathValue("id")); e != nil {
			writeError(w, 403, "organization administrator required")
		} else {
			var invalid tunnelConfigError
			if errors.As(err, &invalid) {
				writeError(w, 400, err.Error())
			} else {
				writeError(w, 500, err.Error())
			}
		}
		return
	}
	writeJSON(w, 200, out)
}
