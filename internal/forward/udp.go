package forward

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const udpIdleTimeout = 60 * time.Second
const maxUDPSessions = 1024
const maxUDPQueuedBytes = 8 << 20

type udpSource struct {
	address net.Addr
	packets chan []byte
}
type udpSessions struct {
	mu      sync.Mutex
	sources map[string]*udpSource
	queued  atomic.Int64
}

func startUDP(parent context.Context, packet net.PacketConn, destination Location, dial DialFunc, logs EventRecorder, direct func(string) bool, idle time.Duration, counters *runtimeCounters) *Runtime {
	ctx, cancel := context.WithCancel(parent)
	r := &Runtime{packet: packet, logs: logs, ctx: ctx, cancel: cancel, done: make(chan struct{}), conns: map[net.Conn]bool{}, runtimeCounters: counters}
	if direct == nil {
		direct = func(string) bool { return false }
	}
	sessions := &udpSessions{sources: map[string]*udpSource{}}
	go func() {
		defer close(r.done)
		buf := make([]byte, maxDatagramSize)
		for {
			n, address, err := packet.ReadFrom(buf)
			if err != nil {
				return
			}
			key := address.String()
			sessions.mu.Lock()
			source := sessions.sources[key]
			if source == nil && len(sessions.sources) < maxUDPSessions && ctx.Err() == nil {
				source = &udpSource{address: address, packets: make(chan []byte, 16)}
				sessions.sources[key] = source
				r.wg.Add(1)
				go func() { defer r.wg.Done(); r.udp(source, destination, dial, direct, idle, sessions, key) }()
			}
			if source != nil {
				if sessions.queued.Add(int64(n+1)) <= maxUDPQueuedBytes {
					payload := append([]byte(nil), buf[:n]...)
					select {
					case source.packets <- payload:
					default:
						sessions.queued.Add(-int64(n + 1))
					}
				} else {
					sessions.queued.Add(-int64(n + 1))
				}
			}
			sessions.mu.Unlock()
		}
	}()
	go func() { <-ctx.Done(); r.closeSockets() }()
	return r
}
func (r *Runtime) udp(source *udpSource, l Location, dial DialFunc, direct func(string) bool, idle time.Duration, sessions *udpSessions, key string) {
	started := time.Now()
	e := Event{Time: started.UnixMilli(), SourceIP: SourceIP(source.address.String()), Method: "UDP", RouteID: l.ID, Upstream: l.ExitTargetID + "@" + l.Address(), Concurrent: r.open(), Direct: direct(l.ID)}
	e.SourceConcurrent = r.openSource(e.SourceIP)
	counts := &byteCounts{inflightUp: &r.inflightUp, inflightDown: &r.inflightDown, inflightDirectUp: &r.inflightDirectUp, inflightDirectDown: &r.inflightDirectDown, direct: func() bool { return direct(l.ID) }}
	defer func() {
		sessions.mu.Lock()
		delete(sessions.sources, key)
		for {
			select {
			case b := <-source.packets:
				sessions.queued.Add(-int64(len(b) + 1))
			default:
				sessions.mu.Unlock()
				goto drained
			}
		}
	drained:
		r.active.Add(-1)
		r.closeSource(e.SourceIP)
		e.DurationMS = time.Since(started).Milliseconds()
		e.Up = counts.up.Load()
		e.Down = counts.down.Load()
		e.DirectUp = counts.directUp.Load()
		e.DirectDown = counts.directDown.Load()
		counts.finish()
		r.logs.Record(e)
	}()
	conn, err := dial(r.ctx, l.ID)
	if err != nil {
		e.Error = err.Error()
		return
	}
	if !r.track(conn) {
		return
	}
	defer r.untrack(conn)
	defer conn.Close()
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	_ = conn.SetDeadline(time.Now().Add(idle))
	sendDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				sendDone <- nil
				return
			case b := <-source.packets:
				sessions.queued.Add(-int64(len(b) + 1))
				_ = conn.SetDeadline(time.Now().Add(idle))
				if err := writeDatagram(conn, b); err != nil {
					conn.Close()
					sendDone <- err
					return
				}
				counts.addUp(int64(len(b)))
			}
		}
	}()
	buf := make([]byte, maxDatagramSize)
	for {
		n, err := readDatagram(conn, buf)
		if err != nil {
			var timeout net.Error
			if r.ctx.Err() == nil && !(errors.As(err, &timeout) && timeout.Timeout()) {
				e.Error = err.Error()
			}
			break
		}
		_ = conn.SetDeadline(time.Now().Add(idle))
		written, err := r.packet.WriteTo(buf[:n], source.address)
		if err != nil {
			if r.ctx.Err() == nil {
				e.Error = err.Error()
			}
			break
		}
		counts.addDown(int64(written))
	}
	cancel()
	conn.Close()
	if err := <-sendDone; err != nil && r.ctx.Err() == nil {
		var timeout net.Error
		if !(errors.As(err, &timeout) && timeout.Timeout()) {
			e.Error = err.Error()
		}
	}
}

// StartPort binds every selected transport atomically: a failed UDP bind cannot
// leave a TCP listener running. TCP listeners may be supplied by an SSH client.
func StartPort(parent context.Context, address string, transports []string, destination Location, dial DialFunc, logs EventRecorder, direct func(string) bool, listenTCP func() (net.Listener, error)) (*Runtime, error) {
	if len(transports) == 0 {
		return nil, errors.New("port transports required")
	}
	var listener net.Listener
	var packet net.PacketConn
	cleanup := func() {
		if listener != nil {
			listener.Close()
		}
		if packet != nil {
			packet.Close()
		}
	}
	selected := map[string]bool{}
	for _, network := range transports {
		if (network != "tcp" && network != "udp") || selected[network] {
			return nil, errors.New("invalid port transports")
		}
		selected[network] = true
	}
	var err error
	if selected["tcp"] {
		if listenTCP != nil {
			listener, err = listenTCP()
		} else {
			listener, err = net.Listen("tcp", address)
		}
		if err != nil {
			return nil, err
		}
		address = listener.Addr().String()
	}
	if selected["udp"] {
		packet, err = net.ListenPacket("udp", address)
		if err != nil {
			cleanup()
			return nil, err
		}
	}
	r := &Runtime{runtimeCounters: newRuntimeCounters()}
	if listener != nil {
		route := destination
		route.ID = "tcp"
		child, e := start(parent, listener, nil, &route, dial, logs, direct, r.runtimeCounters)
		if e != nil {
			cleanup()
			return nil, e
		}
		r.children = append(r.children, child)
	}
	if packet != nil {
		route := destination
		route.ID = "udp"
		r.children = append(r.children, startUDP(parent, packet, route, dial, logs, direct, udpIdleTimeout, r.runtimeCounters))
	}
	return r, nil
}
