package forward

import (
	"context"
	"database/sql"
)

type trafficKey struct {
	source string
	bucket int64
}

func writeTraffic(tx *sql.Tx, events []Event) error {
	values := map[trafficKey]Stats{}
	for _, e := range events {
		ips := []string{""}
		if e.SourceIP != "" {
			ips = append(ips, e.SourceIP)
		}
		for _, ip := range ips {
			key := trafficKey{ip, (e.Time / 300000) * 300}
			s := values[key]
			s.Requests++
			s.Up += e.Up
			s.Down += e.Down
			s.DirectUp += e.DirectUp
			s.DirectDown += e.DirectDown
			peak := e.Concurrent
			if ip != "" && e.SourceConcurrent > 0 {
				peak = e.SourceConcurrent
			}
			s.Peak = max(s.Peak, peak)
			if e.Status >= 500 || e.Error != "" {
				s.Errors++
			}
			values[key] = s
		}
	}
	return writeTrafficValues(tx, values)
}
func writeTrafficValues(tx *sql.Tx, values map[trafficKey]Stats) error {
	if len(values) == 0 {
		return nil
	}
	var finalSeq int64
	if err := tx.QueryRow("UPDATE meta SET value=value+? WHERE key='traffic_seq' RETURNING value", len(values)).Scan(&finalSeq); err != nil {
		return err
	}
	nextSeq := finalSeq - int64(len(values))
	stmt, err := tx.Prepare(`INSERT INTO traffic_buckets(source_ip,bucket,requests,up,down,direct_up,direct_down,peak,errors,updated_seq)VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(source_ip,bucket) DO UPDATE SET requests=requests+excluded.requests,up=up+excluded.up,down=down+excluded.down,direct_up=direct_up+excluded.direct_up,direct_down=direct_down+excluded.direct_down,peak=max(peak,excluded.peak),errors=errors+excluded.errors,updated_seq=excluded.updated_seq`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for key, s := range values {
		nextSeq++
		if _, err := stmt.Exec(key.source, key.bucket, s.Requests, s.Up, s.Down, s.DirectUp, s.DirectDown, s.Peak, s.Errors, nextSeq); err != nil {
			return err
		}
	}
	return nil
}

// Traffic aggregates are independent of access-log retention and survive pruning.
func (l *Logs) Traffic(ctx context.Context, from, to int64, source string) ([]TrafficBucket, []SourceStats, error) {
	rows, err := l.db.QueryContext(ctx, "SELECT bucket,sum(requests),sum(up),sum(down),sum(direct_up),sum(direct_down),max(peak),sum(errors) FROM "+trafficUnion+" WHERE source_ip=? AND bucket>=? AND bucket<? GROUP BY bucket ORDER BY bucket", source, from, to)
	if err != nil {
		return nil, nil, err
	}
	buckets := []TrafficBucket{}
	for rows.Next() {
		b := TrafficBucket{SourceIP: source}
		if err := rows.Scan(&b.Bucket, &b.Requests, &b.Up, &b.Down, &b.DirectUp, &b.DirectDown, &b.Peak, &b.Errors); err != nil {
			rows.Close()
			return nil, nil, err
		}
		buckets = append(buckets, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	q := "SELECT source_ip,sum(requests),sum(up),sum(down),sum(direct_up),sum(direct_down),max(peak),sum(errors) FROM " + trafficUnion + " WHERE source_ip<>'' AND bucket>=? AND bucket<?"
	args := []any{from, to}
	if source != "" {
		q += " AND source_ip=?"
		args = append(args, source)
	}
	q += " GROUP BY source_ip ORDER BY source_ip"
	rows, err = l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	sources := []SourceStats{}
	for rows.Next() {
		var s SourceStats
		if err := rows.Scan(&s.SourceIP, &s.Requests, &s.Up, &s.Down, &s.DirectUp, &s.DirectDown, &s.Peak, &s.Errors); err != nil {
			return nil, nil, err
		}
		sources = append(sources, s)
	}
	return buckets, sources, rows.Err()
}
