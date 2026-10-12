package server

import (
	"github.com/qinyongliang/gosshd-bastion/internal/forward"
	"os"
	"path/filepath"
	"strings"
)

func (m *edgeForwardManager) pruneArchived(days int) {
	dir := filepath.Join(filepath.Dir(m.app.cfg.DatabasePath), "forward-logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if m.ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sqlite") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".sqlite")
		m.mu.Lock()
		open := m.logs[id] != nil
		m.mu.Unlock()
		if open {
			continue
		}
		l, err := forward.OpenLogs(dir, id, days)
		if err == nil {
			_ = l.Close()
		}
	}
}
