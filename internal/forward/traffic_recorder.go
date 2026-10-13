package forward

import "time"

// TrafficRecorder retains lifetime and per-source traffic aggregates without
// creating an access row, request payload or access-log outbox entry.
func (l *Logs) TrafficRecorder() EventRecorder { return trafficRecorder{logs: l} }

type trafficRecorder struct{ logs *Logs }

func (r trafficRecorder) Record(e Event) {
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	r.logs.mu.RLock()
	defer r.logs.mu.RUnlock()
	if !r.logs.closed {
		r.logs.keepAccounting(e)
	}
}
