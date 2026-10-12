package forward

// Under log overload, discard only detailed access rows. Compact accounting deltas
// remain in memory and are retried by the writer, as with legacy tunnel metrics.
func addStats(dst *Stats, s Stats) {
	dst.Requests += s.Requests
	dst.Up += s.Up
	dst.Down += s.Down
	dst.DirectUp += s.DirectUp
	dst.DirectDown += s.DirectDown
	dst.Peak = max(dst.Peak, s.Peak)
	dst.Errors += s.Errors
}
func (l *Logs) keepAccounting(e Event) {
	l.overflowMu.Lock()
	defer l.overflowMu.Unlock()
	if l.overflow == nil {
		l.overflow = map[trafficKey]Stats{}
	}
	s := Stats{Requests: 1, Up: e.Up, Down: e.Down, DirectUp: e.DirectUp, DirectDown: e.DirectDown, Peak: e.Concurrent}
	if e.Status >= 500 || e.Error != "" {
		s.Errors = 1
	}
	addStats(&l.overflowTotal, s)
	ips := []string{""}
	if e.SourceIP != "" {
		ips = append(ips, e.SourceIP)
	}
	for _, ip := range ips {
		key := trafficKey{ip, (e.Time / 300000) * 300}
		v := l.overflow[key]
		source := s
		if ip != "" && e.SourceConcurrent > 0 {
			source.Peak = e.SourceConcurrent
		}
		addStats(&v, source)
		l.overflow[key] = v
	}
}
func (l *Logs) flushAccounting() error {
	l.overflowMu.Lock()
	total := l.overflowTotal
	values := l.overflow
	l.overflowTotal = Stats{}
	l.overflow = nil
	l.overflowMu.Unlock()
	if total.Requests == 0 {
		return nil
	}
	persist := func() error {
		tx, err := l.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Exec("UPDATE totals SET requests=requests+?,up=up+?,down=down+?,direct_up=direct_up+?,direct_down=direct_down+?,peak=max(peak,?),errors=errors+? WHERE id=1", total.Requests, total.Up, total.Down, total.DirectUp, total.DirectDown, total.Peak, total.Errors); err != nil {
			return err
		}
		if err = writeTrafficValues(tx, values); err != nil {
			return err
		}
		return tx.Commit()
	}
	err := persist()
	if err != nil {
		l.failures.Add(1)
		l.overflowMu.Lock()
		if l.overflow == nil {
			l.overflow = map[trafficKey]Stats{}
		}
		addStats(&l.overflowTotal, total)
		for key, s := range values {
			v := l.overflow[key]
			addStats(&v, s)
			l.overflow[key] = v
		}
		l.overflowMu.Unlock()
	}
	return err
}
