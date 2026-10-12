package forward

import (
	"context"
	"strings"
	"time"
)

// Grants retain authorization to deliver an earlier entry's outbox after a rule
// moves to another Agent. They are small metadata in the configuration's log DB.
func (l *Logs) GrantAgent(ctx context.Context, id string) error {
	_, err := l.db.ExecContext(ctx, "INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE value<?", "agent:"+id, time.Now().UnixMilli(), time.Now().Add(-time.Hour).UnixMilli())
	return err
}
func (l *Logs) GrantedAgents(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := l.db.QueryContext(ctx, "SELECT key FROM meta WHERE key LIKE 'agent:%' AND value>=?", since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		ids = append(ids, strings.TrimPrefix(key, "agent:"))
	}
	return ids, rows.Err()
}
