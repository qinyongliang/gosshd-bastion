package forward

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type logJob struct {
	events     []Event
	done       chan error
	accessOnly bool
}
type Logs struct {
	db            *sql.DB
	jobs          chan logJob
	mu            sync.RWMutex
	closed        bool
	done          chan struct{}
	pruneWake     chan struct{}
	retention     atomic.Int64
	dropped       atomic.Uint64
	failures      atomic.Uint64
	overflowMu    sync.Mutex
	overflow      map[trafficKey]Stats
	overflowTotal Stats
}

func OpenLogs(dir, id string, days int) (*Logs, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, errors.New("log configuration ID must be a UUID")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, id+".sqlite")
	db, err := sql.Open("sqlite", filepath.ToSlash(p)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	_, err = db.Exec(`PRAGMA auto_vacuum=INCREMENTAL;
 CREATE TABLE IF NOT EXISTS access_logs (seq INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT NOT NULL UNIQUE, time INTEGER NOT NULL, source_ip TEXT NOT NULL, path TEXT NOT NULL, route_id TEXT NOT NULL, status INTEGER NOT NULL, up INTEGER NOT NULL, down INTEGER NOT NULL, concurrent INTEGER NOT NULL, payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS access_time ON access_logs(time);
 CREATE INDEX IF NOT EXISTS access_source ON access_logs(source_ip,time);
 CREATE INDEX IF NOT EXISTS access_route ON access_logs(route_id,time);
 CREATE TABLE IF NOT EXISTS traffic_buckets (source_ip TEXT NOT NULL,bucket INTEGER NOT NULL,requests INTEGER NOT NULL,up INTEGER NOT NULL,down INTEGER NOT NULL,direct_up INTEGER NOT NULL,direct_down INTEGER NOT NULL,peak INTEGER NOT NULL,errors INTEGER NOT NULL,updated_seq INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(source_ip,bucket));
 CREATE INDEX IF NOT EXISTS traffic_bucket_time ON traffic_buckets(bucket);
 CREATE TABLE IF NOT EXISTS peer_totals (agent_id TEXT PRIMARY KEY,requests INTEGER NOT NULL,up INTEGER NOT NULL,down INTEGER NOT NULL,direct_up INTEGER NOT NULL,direct_down INTEGER NOT NULL,peak INTEGER NOT NULL,errors INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS peer_traffic (agent_id TEXT NOT NULL,source_ip TEXT NOT NULL,bucket INTEGER NOT NULL,requests INTEGER NOT NULL,up INTEGER NOT NULL,down INTEGER NOT NULL,direct_up INTEGER NOT NULL,direct_down INTEGER NOT NULL,peak INTEGER NOT NULL,errors INTEGER NOT NULL,PRIMARY KEY(agent_id,source_ip,bucket));
 CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL);
 INSERT OR IGNORE INTO meta VALUES ('ack',0),('traffic_ack',0),('traffic_seq',0);
 CREATE TABLE IF NOT EXISTS totals (id INTEGER PRIMARY KEY CHECK(id=1), requests INTEGER NOT NULL, up INTEGER NOT NULL, down INTEGER NOT NULL, peak INTEGER NOT NULL, errors INTEGER NOT NULL, direct_up INTEGER NOT NULL DEFAULT 0, direct_down INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO totals(id,requests,up,down,peak,errors) VALUES(1,0,0,0,0,0);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := upgradeLogTotals(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := upgradeTrafficSequence(db); err != nil {
		db.Close()
		return nil, err
	}
	l := &Logs{db: db, jobs: make(chan logJob, 4096), done: make(chan struct{}), pruneWake: make(chan struct{}, 1)}
	l.SetRetention(days)
	if err := l.Prune(context.Background(), time.Now()); err != nil {
		db.Close()
		return nil, err
	}
	go l.loop()
	return l, nil
}
func (l *Logs) SetRetention(days int) {
	if days < 1 {
		days = 7
	}
	if old := l.retention.Swap(int64(days)); old != 0 && old != int64(days) {
		select {
		case l.pruneWake <- struct{}{}:
		default:
		}
	}
}

// Record never waits for disk, a transaction or queue capacity on the data plane.
func (l *Logs) Record(e Event) {
	e.Path = clip(e.Path, 8192)
	e.Host = clip(e.Host, 253)
	e.Error = clip(e.Error, 2048)
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return
	}
	select {
	case l.jobs <- logJob{events: []Event{e}}:
	default:
		l.dropped.Add(1)
		l.keepAccounting(e)
	}
}

// Import acknowledges only after durable commit; caller must retry a failed batch.
func (l *Logs) Import(ctx context.Context, events []Event) error {
	return l.importEvents(ctx, events, false)
}
func (l *Logs) ImportAccess(ctx context.Context, events []Event) error {
	return l.importEvents(ctx, events, true)
}
func (l *Logs) importEvents(ctx context.Context, events []Event, accessOnly bool) error {
	job := logJob{events: events, done: make(chan error, 1), accessOnly: accessOnly}
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return errors.New("logs closed")
	}
	select {
	case l.jobs <- job:
		l.mu.RUnlock()
	case <-ctx.Done():
		l.mu.RUnlock()
		return ctx.Err()
	}
	select {
	case err := <-job.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (l *Logs) Flush(ctx context.Context) error { return l.Import(ctx, nil) }
func (l *Logs) loop() {
	defer close(l.done)
	flush := time.NewTicker(250 * time.Millisecond)
	defer flush.Stop()
	clean := time.NewTicker(time.Hour)
	defer clean.Stop()
	jobs := make([]logJob, 0, 256)
	n := 0
	commit := func() {
		if len(jobs) == 0 {
			_ = l.flushAccounting()
			return
		}
		err := l.commit(jobs)
		if err != nil {
			l.failures.Add(uint64(n))
			for _, j := range jobs {
				if j.done == nil {
					for _, e := range j.events {
						l.keepAccounting(e)
						l.dropped.Add(1)
					}
				}
			}
		}
		_ = l.flushAccounting()
		for _, j := range jobs {
			if j.done != nil {
				j.done <- err
			}
		}
		jobs = jobs[:0]
		n = 0
	}
	for {
		select {
		case j, ok := <-l.jobs:
			if !ok {
				commit()
				return
			}
			jobs = append(jobs, j)
			n += len(j.events)
			if n >= 256 || j.done != nil {
				commit()
			}
		case <-flush.C:
			commit()
		case <-l.pruneWake:
			if err := l.Prune(context.Background(), time.Now()); err != nil {
				l.failures.Add(1)
			}
		case <-clean.C:
			if err := l.Prune(context.Background(), time.Now()); err != nil {
				l.failures.Add(1)
			}
		}
	}
}
func (l *Logs) commit(jobs []logJob) error {
	hasEvents := false
	for _, j := range jobs {
		if len(j.events) > 0 {
			hasEvents = true
			break
		}
	}
	if !hasEvents {
		return nil
	}
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO access_logs(event_id,time,source_ip,path,route_id,status,up,down,concurrent,payload) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	var inserted []Event
	var requests, up, down, peak, fail, directUp, directDown int64
	for _, j := range jobs {
		for _, e := range j.events {
			if e.ID == "" {
				return errors.New("event ID required")
			}
			p, err := json.Marshal(e)
			if err != nil {
				return err
			}
			result, err := stmt.Exec(e.ID, e.Time, e.SourceIP, e.Path, e.RouteID, e.Status, e.Up, e.Down, e.Concurrent, string(p))
			if err != nil {
				return err
			}
			n, _ := result.RowsAffected()
			if n == 0 {
				continue
			}
			if j.accessOnly {
				continue
			}
			inserted = append(inserted, e)
			requests++
			directUp += e.DirectUp
			directDown += e.DirectDown
			up += e.Up
			down += e.Down
			peak = max(peak, e.Concurrent)
			if e.Status >= 500 || e.Error != "" {
				fail++
			}
		}
	}
	if _, err = tx.Exec(`UPDATE totals SET requests=requests+?,up=up+?,down=down+?,peak=max(peak,?),errors=errors+?,direct_up=direct_up+?,direct_down=direct_down+? WHERE id=1`, requests, up, down, peak, fail, directUp, directDown); err != nil {
		return err
	}
	if err := writeTraffic(tx, inserted); err != nil {
		return err
	}
	return tx.Commit()
}
func scanEvents(rows *sql.Rows) ([]Event, error) {
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var seq int64
		var p string
		if err := rows.Scan(&seq, &p); err != nil {
			return nil, err
		}
		var e Event
		if err := json.Unmarshal([]byte(p), &e); err != nil {
			return nil, err
		}
		e.Sequence = seq
		out = append(out, e)
	}
	return out, rows.Err()
}
func (l *Logs) Pending(ctx context.Context, limit int) ([]Event, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT seq,payload FROM access_logs WHERE seq>(SELECT value FROM meta WHERE key='ack') ORDER BY seq LIMIT ?`, min(max(limit, 1), 256))
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}
func (l *Logs) Ack(ctx context.Context, seq int64) error {
	_, err := l.db.ExecContext(ctx, `UPDATE meta SET value=max(value,?) WHERE key='ack'`, seq)
	return err
}
func filterSQL(f LogFilter) (string, []any) {
	var clauses []string
	var args []any
	add := func(s string, v any) { clauses = append(clauses, s); args = append(args, v) }
	if f.Before > 0 {
		add("seq<?", f.Before)
	}
	if f.From > 0 {
		add("time>=?", f.From)
	}
	if f.To > 0 {
		add("time<?", f.To)
	}
	if f.SourceIP != "" {
		add("source_ip=?", f.SourceIP)
	}
	if f.RouteID != "" {
		add("route_id=?", f.RouteID)
	}
	if f.Status > 0 {
		add("status=?", f.Status)
	}
	if f.Path != "" {
		add("instr(path,?)>0", f.Path)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
func (l *Logs) Query(ctx context.Context, f LogFilter) ([]Event, error) {
	q, args := filterSQL(f)
	limit := f.Limit
	if limit == 0 {
		limit = 100
	}
	args = append(args, min(max(limit, 1), 500))
	rows, err := l.db.QueryContext(ctx, "SELECT seq,payload FROM access_logs"+q+" ORDER BY seq DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}
func (l *Logs) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := l.db.QueryRowContext(ctx, "SELECT requests,up,down,peak,errors,direct_up,direct_down FROM totals WHERE id=1").Scan(&s.Requests, &s.Up, &s.Down, &s.Peak, &s.Errors, &s.DirectUp, &s.DirectDown)
	if err == nil {
		var peer Stats
		err = l.db.QueryRowContext(ctx, "SELECT coalesce(sum(requests),0),coalesce(sum(up),0),coalesce(sum(down),0),coalesce(sum(direct_up),0),coalesce(sum(direct_down),0),coalesce(max(peak),0),coalesce(sum(errors),0) FROM peer_totals").Scan(&peer.Requests, &peer.Up, &peer.Down, &peer.DirectUp, &peer.DirectDown, &peer.Peak, &peer.Errors)
		s.Requests += peer.Requests
		s.Up += peer.Up
		s.Down += peer.Down
		s.DirectUp += peer.DirectUp
		s.DirectDown += peer.DirectDown
		s.Peak = max(s.Peak, peer.Peak)
		s.Errors += peer.Errors
	}
	s.Dropped = l.dropped.Load()
	s.WriteFailures = l.failures.Load()
	return s, err
}
func (l *Logs) Sources(ctx context.Context, f LogFilter) ([]SourceStats, error) {
	q, args := filterSQL(f)
	rows, err := l.db.QueryContext(ctx, "SELECT source_ip,count(*),sum(up),sum(down),max(coalesce(json_extract(payload,'$.source_concurrent'),concurrent)),sum(CASE WHEN status>=500 THEN 1 ELSE 0 END) FROM access_logs"+q+" GROUP BY source_ip ORDER BY sum(up+down) DESC LIMIT 500", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceStats{}
	for rows.Next() {
		var s SourceStats
		if err := rows.Scan(&s.SourceIP, &s.Requests, &s.Up, &s.Down, &s.Peak, &s.Errors); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (l *Logs) Prune(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-time.Duration(l.retention.Load()) * 24 * time.Hour).UnixMilli()
	for {
		res, err := l.db.ExecContext(ctx, "DELETE FROM access_logs WHERE seq IN (SELECT seq FROM access_logs WHERE time<? LIMIT 1000)", cutoff)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n < 1000 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	_, err := l.db.ExecContext(ctx, "PRAGMA incremental_vacuum(256)")
	return err
}
func (l *Logs) Close() error {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.jobs)
	}
	l.mu.Unlock()
	<-l.done
	return l.db.Close()
}
func (l *Logs) Health() string {
	if n := l.failures.Load(); n > 0 {
		return fmt.Sprintf("%d failed log writes", n)
	}
	return ""
}
