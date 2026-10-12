package forward

import (
	"context"
	"database/sql"
)

func (l *Logs) PendingTraffic(ctx context.Context, limit int) ([]TrafficBucket, error) {
	rows, err := l.db.QueryContext(ctx, "SELECT updated_seq,source_ip,bucket,requests,up,down,direct_up,direct_down,peak,errors FROM traffic_buckets WHERE updated_seq>(SELECT value FROM meta WHERE key='traffic_ack') ORDER BY updated_seq LIMIT ?", min(max(limit, 1), 512))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrafficBucket{}
	for rows.Next() {
		var b TrafficBucket
		if err := rows.Scan(&b.Sequence, &b.SourceIP, &b.Bucket, &b.Requests, &b.Up, &b.Down, &b.DirectUp, &b.DirectDown, &b.Peak, &b.Errors); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (l *Logs) AckTraffic(ctx context.Context, seq int64) error {
	_, err := l.db.ExecContext(ctx, "UPDATE meta SET value=max(value,?) WHERE key='traffic_ack'", seq)
	return err
}

// Access events and accounting are synchronized independently: retention or a long
// outage never removes lifetime/source traffic. Absolute bucket snapshots are idempotent.
func (l *Logs) ImportTelemetry(ctx context.Context, agent string, total Stats, buckets []TrafficBucket) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO peer_totals(agent_id,requests,up,down,direct_up,direct_down,peak,errors)VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET requests=max(requests,excluded.requests),up=max(up,excluded.up),down=max(down,excluded.down),direct_up=max(direct_up,excluded.direct_up),direct_down=max(direct_down,excluded.direct_down),peak=max(peak,excluded.peak),errors=max(errors,excluded.errors) WHERE requests<excluded.requests OR up<excluded.up OR down<excluded.down OR direct_up<excluded.direct_up OR direct_down<excluded.direct_down OR peak<excluded.peak OR errors<excluded.errors`, agent, total.Requests, total.Up, total.Down, total.DirectUp, total.DirectDown, total.Peak, total.Errors)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO peer_traffic(agent_id,source_ip,bucket,requests,up,down,direct_up,direct_down,peak,errors)VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(agent_id,source_ip,bucket) DO UPDATE SET requests=max(requests,excluded.requests),up=max(up,excluded.up),down=max(down,excluded.down),direct_up=max(direct_up,excluded.direct_up),direct_down=max(direct_down,excluded.direct_down),peak=max(peak,excluded.peak),errors=max(errors,excluded.errors)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, b := range buckets {
		if _, err := stmt.ExecContext(ctx, agent, b.SourceIP, b.Bucket, b.Requests, b.Up, b.Down, b.DirectUp, b.DirectDown, b.Peak, b.Errors); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func upgradeTrafficSequence(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(traffic_buckets)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var index, notnull, primary int
		var name, kind string
		var value any
		if err := rows.Scan(&index, &name, &kind, &notnull, &value, &primary); err != nil {
			rows.Close()
			return err
		}
		if name == "updated_seq" {
			found = true
		}
	}
	rows.Close()
	if !found {
		if _, err := db.Exec("ALTER TABLE traffic_buckets ADD COLUMN updated_seq INTEGER NOT NULL DEFAULT 0;UPDATE traffic_buckets SET updated_seq=rowid;UPDATE meta SET value=(SELECT coalesce(max(updated_seq),0) FROM traffic_buckets) WHERE key='traffic_seq'"); err != nil {
			return err
		}
	}
	return nil
}

const trafficUnion = "(SELECT source_ip,bucket,requests,up,down,direct_up,direct_down,peak,errors FROM traffic_buckets UNION ALL SELECT source_ip,bucket,requests,up,down,direct_up,direct_down,peak,errors FROM peer_traffic)"
