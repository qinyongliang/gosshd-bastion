package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type apiTunnel struct {
	store.Tunnel
	tunnelStatus
}

func (a *App) validateTunnel(ctx context.Context, user store.User, orgID string, c store.TunnelConfig) error {
	if _, err := a.store.Repository().GetOrganization(ctx, orgID); err != nil {
		return errors.New("organization not found")
	}
	if err := a.requireOrganizationAdmin(ctx, orgID, user); err != nil {
		return err
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 128 {
		return errors.New("name must contain 1–128 characters")
	}
	if net.ParseIP(c.ListenHost) == nil {
		return errors.New("listen host must be an IP address")
	}
	if c.ListenPort < 1 || c.ListenPort > 65535 || c.DestinationPort < 1 || c.DestinationPort > 65535 {
		return errors.New("ports must be between 1 and 65535")
	}
	if strings.TrimSpace(c.DestinationHost) == "" || len(c.DestinationHost) > 253 || strings.ContainsAny(c.DestinationHost, " /\\\t\r\n") {
		return errors.New("invalid destination host")
	}
	if c.DurationSeconds < 0 || c.DurationSeconds > 365*24*3600 {
		return errors.New("duration must be 0 (permanent) or at most 365 days")
	}
	for _, id := range []string{c.EntryTargetID, c.ExitTargetID} {
		if id == "" {
			if !user.IsSystemAdmin {
				return errors.New("bastion endpoints require system admin access")
			}
			continue
		}
		target, err := a.store.Repository().GetSSHTarget(ctx, id)
		if err != nil {
			return errors.New("endpoint machine not found")
		}
		if target.OwnerType != "organization" || target.OwnerID != orgID {
			return errors.New("endpoint machine must belong to this organization")
		}
		if target.TargetType != store.TargetAgent && target.TargetType != store.TargetDirect {
			return errors.New("unsupported endpoint machine type")
		}
		// Validate every jump host as well: a saved chain must not cross organizations.
		seen := map[string]bool{id: true}
		for target.ProxyTargetID != "" {
			if seen[target.ProxyTargetID] || len(seen) > 3 {
				return errors.New("invalid jump host chain")
			}
			seen[target.ProxyTargetID] = true
			target, err = a.store.Repository().GetSSHTarget(ctx, target.ProxyTargetID)
			if err != nil || target.OwnerType != "organization" || target.OwnerID != orgID {
				return errors.New("jump host must belong to this organization")
			}
		}
	}
	return nil
}
func (a *App) handleListTunnels(w http.ResponseWriter, r *http.Request, u store.User) {
	org := r.URL.Query().Get("organization_id")
	if org == "" {
		writeError(w, 400, "organization_id required")
		return
	}
	if a.requireOrganizationAdmin(r.Context(), org, u) != nil {
		writeError(w, 403, "organization admin required")
		return
	}
	tunnels, err := a.store.Repository().ListTunnels(r.Context(), org)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]apiTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		out = append(out, apiTunnel{t, a.tunnelAPIStatus(t)})
	}
	writeJSON(w, 200, map[string]any{"tunnels": out})
}
func (a *App) handleSaveTunnel(w http.ResponseWriter, r *http.Request, u store.User) {
	var body struct {
		store.TunnelConfig
		OrganizationID string `json:"organization_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	t := store.Tunnel{OrganizationID: body.OrganizationID, CreatedBy: u.ID}
	if id := r.PathValue("id"); id != "" {
		var err error
		t, err = a.store.Repository().GetTunnel(r.Context(), id)
		if err != nil {
			writeError(w, 404, "tunnel not found")
			return
		}
	}
	if a.requireOrganizationAdmin(r.Context(), t.OrganizationID, u) != nil {
		writeError(w, 403, "organization admin required")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.DestinationHost = strings.TrimSpace(body.DestinationHost)
	body.ListenHost = strings.TrimSpace(body.ListenHost)
	if err := a.validateTunnel(r.Context(), u, t.OrganizationID, body.TunnelConfig); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	t.TunnelConfig = body.TunnelConfig

	t, err := a.store.Repository().SaveTunnel(r.Context(), t)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	a.tunnels.notify()
	writeJSON(w, 200, map[string]any{"tunnel": apiTunnel{t, a.tunnelAPIStatus(t)}})
}
func (a *App) handleTunnelAction(w http.ResponseWriter, r *http.Request, u store.User) {
	repo := a.store.Repository()
	t, err := repo.GetTunnel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "tunnel not found")
		return
	}
	if a.requireOrganizationAdmin(r.Context(), t.OrganizationID, u) != nil {
		writeError(w, 403, "organization admin required")
		return
	}
	if r.Method == http.MethodDelete {
		if err = repo.DeleteTunnel(r.Context(), t.ID); err == nil {
			a.tunnels.stop(t.ID)
		}
	} else {
		enabled := strings.HasSuffix(r.URL.Path, "/enable")
		if enabled {
			if err = a.validateTunnel(r.Context(), u, t.OrganizationID, t.TunnelConfig); err != nil {
				writeError(w, 400, err.Error())
				return
			}
		}
		if enabled {
			t, err = repo.EnableTunnelForUser(r.Context(), t.ID, t.DurationSeconds, u.ID)
		} else {
			t, err = repo.SetTunnelEnabled(r.Context(), t.ID, false, t.DurationSeconds)
		}
		if err == nil {
			a.tunnels.stop(t.ID)
		}
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if r.Method == http.MethodDelete {
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, 200, map[string]any{"tunnel": apiTunnel{t, a.tunnelAPIStatus(t)}})
}

func (a *App) tunnelAPIStatus(t store.Tunnel) tunnelStatus {
	state := a.tunnels.status(t)
	total, active, direct, err := a.tunnels.traffic(t)
	if err != nil {
		state.Error = err.Error()
	}
	state.Traffic = total
	state.Connections = active
	state.DirectConnections = direct
	a.tunnels.mu.Lock()
	metrics := a.tunnels.metrics[t.ID]
	a.tunnels.mu.Unlock()
	state.Paths = []tunnelConnectionPath{}
	if metrics != nil {
		state.Paths = metrics.pathsSnapshot()
	}
	state.Transport = "relay"
	if active > 0 && direct == active {
		state.Transport = "direct"
	} else if direct > 0 {
		state.Transport = "mixed"
	} else if t.Enabled {
		a.tunnels.mu.Lock()
		run := a.tunnels.runs[t.ID]
		eligible := false
		if run != nil {
			run.mu.Lock()
			eligible = run.peerEnabled
			run.mu.Unlock()
		}
		a.tunnels.mu.Unlock()
		if eligible {
			state.Transport = "negotiating"
		}
	}
	return state
}
func (a *App) handleTunnelTraffic(w http.ResponseWriter, r *http.Request, u store.User) {
	t, err := a.store.Repository().GetTunnel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "tunnel not found")
		return
	}
	if a.requireOrganizationAdmin(r.Context(), t.OrganizationID, u) != nil {
		writeError(w, 403, "organization admin required")
		return
	}
	now := time.Now().Unix()
	to := now - now%300 + 300
	from := to - 24*3600
	if v := r.URL.Query().Get("from"); v != "" {
		from, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, 400, "from must be Unix seconds")
			return
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		to, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, 400, "to must be Unix seconds")
			return
		}
	}
	if from < 0 || to <= from || to-from > 31*24*3600+300 {
		writeError(w, 400, "time range must be between 0 and 31 days")
		return
	}
	from -= from % 300
	a.tunnels.mu.Lock()
	metrics := a.tunnels.metrics[t.ID]
	a.tunnels.mu.Unlock()
	if metrics != nil {
		metrics.mu.Lock()
		defer metrics.mu.Unlock()
	}
	rows, err := a.audit.Repository().TunnelTraffic(r.Context(), t.ID, from, to)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	values := map[int64]store.TunnelTraffic{}
	for _, v := range rows {
		values[v.BucketStart] = v
	}
	if metrics != nil {
		for bucket, v := range metrics.buckets {
			if bucket < from || bucket >= to {
				continue
			}
			old := values[bucket]
			old.BucketStart = bucket
			old.RelayUp += v.RelayUp
			old.RelayDown += v.RelayDown
			old.DirectUp += v.DirectUp
			old.DirectDown += v.DirectDown
			old.ConnectionsOpened += v.ConnectionsOpened
			old.PeakConnections = max(old.PeakConnections, v.PeakConnections)
			values[bucket] = old
		}
	}
	out := []store.TunnelTraffic{}
	for bucket := from; bucket < to; bucket += 300 {
		v := values[bucket]
		v.BucketStart = bucket
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"interval_seconds": 300, "buckets": out})
}
