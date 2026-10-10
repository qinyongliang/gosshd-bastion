package store

import (
	"context"
	"database/sql"
	"time"
)

const TunnelTrafficInterval = 5 * time.Minute

type TunnelTraffic struct {
	SourceIP          string `json:"source_ip,omitempty"`
	BucketStart       int64  `json:"bucket_start"`
	RelayUp           int64  `json:"relay_up"`
	RelayDown         int64  `json:"relay_down"`
	DirectUp          int64  `json:"direct_up"`
	DirectDown        int64  `json:"direct_down"`
	ConnectionsOpened int64  `json:"connections_opened"`
	PeakConnections   int64  `json:"peak_connections"`
}

func (t TunnelTraffic) Empty() bool {
	return t.RelayUp+t.RelayDown+t.DirectUp+t.DirectDown+t.ConnectionsOpened+t.PeakConnections == 0
}
func (r *AuditRepository) AddTunnelTraffic(ctx context.Context, id, org string, t TunnelTraffic) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO tunnel_traffic(tunnel_id,organization_id,source_ip,bucket_start,relay_up,relay_down,direct_up,direct_down,connections_opened,peak_connections) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tunnel_id,source_ip,bucket_start) DO UPDATE SET relay_up=relay_up+excluded.relay_up,relay_down=relay_down+excluded.relay_down,direct_up=direct_up+excluded.direct_up,direct_down=direct_down+excluded.direct_down,connections_opened=connections_opened+excluded.connections_opened,peak_connections=MAX(peak_connections,excluded.peak_connections)`, id, org, t.SourceIP, t.BucketStart, t.RelayUp, t.RelayDown, t.DirectUp, t.DirectDown, t.ConnectionsOpened, t.PeakConnections)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tunnel_traffic_totals(tunnel_id,organization_id,source_ip,relay_up,relay_down,direct_up,direct_down,connections_opened,peak_connections) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tunnel_id,source_ip) DO UPDATE SET relay_up=relay_up+excluded.relay_up,relay_down=relay_down+excluded.relay_down,direct_up=direct_up+excluded.direct_up,direct_down=direct_down+excluded.direct_down,connections_opened=connections_opened+excluded.connections_opened,peak_connections=MAX(peak_connections,excluded.peak_connections)`, id, org, t.SourceIP, t.RelayUp, t.RelayDown, t.DirectUp, t.DirectDown, t.ConnectionsOpened, t.PeakConnections)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AuditRepository) TunnelTraffic(ctx context.Context, id string, from, to int64) ([]TunnelTraffic, error) {
	return r.TunnelTrafficForSource(ctx, id, from, to, "")
}
func (r *AuditRepository) TunnelTrafficForSource(ctx context.Context, id string, from, to int64, sourceIP string) ([]TunnelTraffic, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT bucket_start,relay_up,relay_down,direct_up,direct_down,connections_opened,peak_connections FROM tunnel_traffic WHERE tunnel_id=? AND source_ip=? AND bucket_start>=? AND bucket_start<? ORDER BY bucket_start`, id, sourceIP, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TunnelTraffic{}
	for rows.Next() {
		var v TunnelTraffic
		v.SourceIP = sourceIP
		if err = rows.Scan(&v.BucketStart, &v.RelayUp, &v.RelayDown, &v.DirectUp, &v.DirectDown, &v.ConnectionsOpened, &v.PeakConnections); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *AuditRepository) TunnelTrafficTotal(ctx context.Context, id string) (TunnelTraffic, error) {
	var t TunnelTraffic
	err := r.db.QueryRowContext(ctx, `SELECT relay_up,relay_down,direct_up,direct_down,connections_opened,peak_connections FROM tunnel_traffic_totals WHERE tunnel_id=? AND source_ip=''`, id).Scan(&t.RelayUp, &t.RelayDown, &t.DirectUp, &t.DirectDown, &t.ConnectionsOpened, &t.PeakConnections)
	if err == sql.ErrNoRows {
		err = nil
	}
	return t, err
}

func (r *AuditRepository) TunnelTrafficSources(ctx context.Context, id string, from, to int64, sourceIP string) ([]TunnelTraffic, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT source_ip,SUM(relay_up),SUM(relay_down),SUM(direct_up),SUM(direct_down),SUM(connections_opened),MAX(peak_connections) FROM tunnel_traffic WHERE tunnel_id=? AND source_ip<>'' AND bucket_start>=? AND bucket_start<? AND (?='' OR source_ip=?) GROUP BY source_ip ORDER BY source_ip`, id, from, to, sourceIP, sourceIP)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TunnelTraffic{}
	for rows.Next() {
		var v TunnelTraffic
		if err := rows.Scan(&v.SourceIP, &v.RelayUp, &v.RelayDown, &v.DirectUp, &v.DirectDown, &v.ConnectionsOpened, &v.PeakConnections); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
