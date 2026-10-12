package forward

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EventRecorder decouples the data plane from SQLite/outbox storage.
type EventRecorder interface{ Record(Event) }
type DialFunc func(context.Context, string) (net.Conn, error)
type Runtime struct {
	*runtimeCounters
	children   []*Runtime
	packet     net.PacketConn
	listener   net.Listener
	server     *http.Server
	logs       EventRecorder
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
	conns      map[net.Conn]bool
	transports []*http.Transport
	closing    bool
}

type runtimeCounters struct {
	inflightUp         atomic.Int64
	inflightDown       atomic.Int64
	inflightDirectUp   atomic.Int64
	inflightDirectDown atomic.Int64
	sourceMu           sync.Mutex
	sources            map[string]int64
	active             atomic.Int64
	peak               atomic.Int64
	requests           atomic.Int64
}

func newRuntimeCounters() *runtimeCounters { return &runtimeCounters{sources: map[string]int64{}} }

var buffers = sync.Pool{New: func() any { return make([]byte, 32*1024) }}

type bufferPool struct{}

func (bufferPool) Get() []byte { return buffers.Get().([]byte) }
func (bufferPool) Put(b []byte) {
	if cap(b) == 32*1024 {
		buffers.Put(b[:32*1024])
	}
}
func Start(parent context.Context, listener net.Listener, httpConfig *HTTPConfig, tcpRoute *Location, dial DialFunc, logs EventRecorder, direct func(string) bool) (*Runtime, error) {
	return start(parent, listener, httpConfig, tcpRoute, dial, logs, direct, newRuntimeCounters())
}
func start(parent context.Context, listener net.Listener, httpConfig *HTTPConfig, tcpRoute *Location, dial DialFunc, logs EventRecorder, direct func(string) bool, counters *runtimeCounters) (*Runtime, error) {
	ctx, cancel := context.WithCancel(parent)
	r := &Runtime{listener: listener, logs: logs, ctx: ctx, cancel: cancel, done: make(chan struct{}), conns: map[net.Conn]bool{}, runtimeCounters: counters}
	if direct == nil {
		direct = func(string) bool { return false }
	}
	if httpConfig != nil {
		router, err := Compile(httpConfig)
		if err != nil {
			cancel()
			return nil, err
		}
		proxies := map[string]*httputil.ReverseProxy{}
		for _, s := range httpConfig.Servers {
			for _, l := range s.Locations {
				tr := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx, l.ID) }, MaxIdleConns: 64, MaxIdleConnsPerHost: 32, MaxConnsPerHost: 0, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second, ExpectContinueTimeout: time.Second, ForceAttemptHTTP2: false}
				r.transports = append(r.transports, tr)
				proxies[l.ID] = &httputil.ReverseProxy{Transport: tr, BufferPool: bufferPool{}, Rewrite: func(p *httputil.ProxyRequest) {
					// Preserve a real Close that unblocks reads. Go 1.26.4 wraps the
					// cloned body in a close-marker reader which can race its final
					// writer read after the upstream responds; use the counted inbound body.
					if p.Out.Body != nil {
						p.Out.Body = p.In.Body
					}
					p.Out.URL.Scheme = l.Scheme
					p.Out.URL.Host = l.Address()
					p.Out.Host = l.Address()
					if l.PreserveHost {
						p.Out.Host = p.In.Host
					}
					if l.StripPrefix {
						p.Out.URL.Path = strings.TrimPrefix(p.In.URL.Path, l.Path)
						if !strings.HasPrefix(p.Out.URL.Path, "/") {
							p.Out.URL.Path = "/" + p.Out.URL.Path
						}
						p.Out.URL.RawPath = ""
					}
					p.Out.Header.Del("Forwarded")
					p.SetXForwarded()
					p.Out.Header.Set("X-Real-IP", SourceIP(p.In.RemoteAddr))
					for k, v := range l.Headers {
						p.Out.Header.Set(k, v)
					}
				}, ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
					if o, ok := w.(*observedWriter); ok {
						o.event.Error = err.Error()
					}
					http.Error(w, "upstream unavailable", http.StatusBadGateway)
				}}
			}
		}
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			r.mu.Lock()
			if r.closing {
				r.mu.Unlock()
				http.Error(w, "forward stopped", 503)
				return
			}
			r.wg.Add(1)
			r.mu.Unlock()
			defer r.wg.Done()
			n := r.open()
			defer r.active.Add(-1)
			started := time.Now()
			e := Event{Time: started.UnixMilli(), SourceIP: SourceIP(req.RemoteAddr), Method: req.Method, Host: req.Host, Path: req.URL.RequestURI(), Concurrent: n}
			e.SourceConcurrent = r.openSource(e.SourceIP)
			defer r.closeSource(e.SourceIP)
			counts := &byteCounts{inflightUp: &r.inflightUp, inflightDown: &r.inflightDown, inflightDirectUp: &r.inflightDirectUp, inflightDirectDown: &r.inflightDirectDown}
			ow := &observedWriter{ResponseWriter: w, event: &e, counts: counts}
			defer func() {
				panicValue := recover()
				if panicValue != nil {
					e.Error = fmt.Sprint(panicValue)
					if e.Status == 0 {
						e.Status = 500
					}
				}
				e.DurationMS = time.Since(started).Milliseconds()
				e.Up = counts.up.Load()
				e.Down = counts.down.Load()
				e.DirectUp = counts.directUp.Load()
				e.DirectDown = counts.directDown.Load()
				counts.finish()
				if e.Status == 0 {
					e.Status = 200
				}
				logs.Record(e)
				if panicValue != nil {
					panic(panicValue)
				}
			}()
			if req.Body != nil {
				req.Body = &countBody{ReadCloser: req.Body, counts: counts, remaining: req.ContentLength}
			}
			l, sid := router.Match(req.Host, req.URL.Path)
			e.ServerID = sid
			if l == nil {
				http.NotFound(ow, req)
				return
			}
			e.RouteID = l.ID
			e.Upstream = l.ExitTargetID + "@" + l.Scheme + "://" + l.Address()
			e.Direct = direct(l.ID)
			counts.direct = func() bool { return direct(l.ID) }
			proxies[l.ID].ServeHTTP(ow, req)
		})
		r.server = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20, BaseContext: func(net.Listener) context.Context { return ctx }}
		go func() { defer close(r.done); _ = r.server.Serve(&trackedListener{Listener: listener, r: r}) }()
	} else {
		if tcpRoute == nil {
			cancel()
			return nil, errors.New("TCP destination required")
		}
		go func() {
			defer close(r.done)
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				if !r.track(conn) {
					continue
				}
				r.wg.Add(1)
				go func() {
					defer r.wg.Done()
					defer r.untrack(conn)
					defer conn.Close()
					r.tcp(conn, *tcpRoute, dial, direct)
				}()
			}
		}()
	}
	go func() { <-ctx.Done(); r.closeSockets() }()
	return r, nil
}
func (r *Runtime) open() int64 {
	n := r.active.Add(1)
	r.requests.Add(1)
	for {
		old := r.peak.Load()
		if n <= old || r.peak.CompareAndSwap(old, n) {
			break
		}
	}
	return n
}
func (r *Runtime) Alive() bool {
	if len(r.children) > 0 {
		for _, c := range r.children {
			if !c.Alive() {
				return false
			}
		}
		return true
	}
	if r.ctx.Err() != nil {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}
func (r *Runtime) State() RuntimeState {
	if len(r.children) > 0 {
		return r.children[0].State()
	}
	address := ""
	if r.listener != nil {
		address = r.listener.Addr().String()
	} else {
		address = r.packet.LocalAddr().String()
	}

	r.sourceMu.Lock()
	sources := map[string]int64{}
	for ip, n := range r.sources {
		sources[ip] = n
	}
	r.sourceMu.Unlock()
	return RuntimeState{Sources: sources, InflightUp: r.inflightUp.Load(), InflightDown: r.inflightDown.Load(), InflightDirectUp: r.inflightDirectUp.Load(), InflightDirectDown: r.inflightDirectDown.Load(), Active: r.active.Load(), Peak: r.peak.Load(), Requests: r.requests.Load(), ListenAddress: address}
}
func (r *Runtime) openSource(ip string) int64 {
	r.sourceMu.Lock()
	defer r.sourceMu.Unlock()
	r.sources[ip]++
	return r.sources[ip]
}
func (r *Runtime) closeSource(ip string) {
	r.sourceMu.Lock()
	defer r.sourceMu.Unlock()
	r.sources[ip]--
	if r.sources[ip] <= 0 {
		delete(r.sources, ip)
	}
}
func (r *Runtime) track(c net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		c.Close()
		return false
	}
	r.conns[c] = true
	return true
}
func (r *Runtime) untrack(c net.Conn) { r.mu.Lock(); delete(r.conns, c); r.mu.Unlock() }
func (r *Runtime) closeSockets() {
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return
	}
	r.closing = true
	conns := make([]net.Conn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.Unlock()
	if r.listener != nil {
		_ = r.listener.Close()
	}
	if r.packet != nil {
		_ = r.packet.Close()
	}
	if r.server != nil {
		_ = r.server.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	for _, t := range r.transports {
		t.CloseIdleConnections()
	}
}
func (r *Runtime) Close() error {
	if len(r.children) > 0 {
		for _, c := range r.children {
			_ = c.Close()
		}
		return nil
	}
	r.cancel()
	r.closeSockets()
	<-r.done
	r.wg.Wait()
	if r.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.server.Shutdown(ctx)
	}
	return nil
}
func (r *Runtime) tcp(source net.Conn, l Location, dial DialFunc, direct func(string) bool) {
	n := r.open()
	defer r.active.Add(-1)
	started := time.Now()
	e := Event{Time: started.UnixMilli(), SourceIP: SourceIP(source.RemoteAddr().String()), Method: "TCP", RouteID: l.ID, Upstream: l.ExitTargetID + "@" + l.Address(), Concurrent: n, Direct: direct(l.ID)}
	e.SourceConcurrent = r.openSource(e.SourceIP)
	defer r.closeSource(e.SourceIP)
	counts := &byteCounts{inflightUp: &r.inflightUp, inflightDown: &r.inflightDown, inflightDirectUp: &r.inflightDirectUp, inflightDirectDown: &r.inflightDirectDown, direct: func() bool { return direct(l.ID) }}
	defer func() {
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
	var wg sync.WaitGroup
	wg.Add(2)
	copySide := func(dst, src net.Conn, n *atomic.Int64) {
		defer wg.Done()
		buf := bufferPool{}.Get()
		defer bufferPool{}.Put(buf)
		_, err := io.CopyBuffer(countWriter{w: dst, n: n, counts: counts, up: n == &counts.up}, src, buf)
		if err != nil {
			source.Close()
			conn.Close()
			return
		}
		if w, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = w.CloseWrite()
		} else {
			source.Close()
			conn.Close()
		}
	}
	go copySide(conn, source, &counts.up)
	go copySide(source, conn, &counts.down)
	wg.Wait()
}

type trackedListener struct {
	net.Listener
	r *Runtime
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if !l.r.track(c) {
		return nil, net.ErrClosed
	}
	return &trackedConn{Conn: c, r: l.r}, nil
}

type trackedConn struct {
	net.Conn
	r *Runtime
}

func (c *trackedConn) Close() error { c.r.untrack(c.Conn); return c.Conn.Close() }

type byteCounts struct {
	up, down, directUp, directDown                                 atomic.Int64
	inflightUp, inflightDown, inflightDirectUp, inflightDirectDown *atomic.Int64
	direct                                                         func() bool
}

func (c *byteCounts) addUp(n int64) {
	c.up.Add(n)
	if c.inflightUp != nil {
		c.inflightUp.Add(n)
	}
	if c.direct != nil && c.direct() {
		c.directUp.Add(n)
		if c.inflightDirectUp != nil {
			c.inflightDirectUp.Add(n)
		}
	}
}
func (c *byteCounts) addDown(n int64) {
	c.down.Add(n)
	if c.inflightDown != nil {
		c.inflightDown.Add(n)
	}
	if c.direct != nil && c.direct() {
		c.directDown.Add(n)
		if c.inflightDirectDown != nil {
			c.inflightDirectDown.Add(n)
		}
	}
}
func (c *byteCounts) finish() {
	if c.inflightDirectUp != nil {
		c.inflightDirectUp.Add(-c.directUp.Load())
	}
	if c.inflightDirectDown != nil {
		c.inflightDirectDown.Add(-c.directDown.Load())
	}
	if c.inflightUp != nil {
		c.inflightUp.Add(-c.up.Load())
	}
	if c.inflightDown != nil {
		c.inflightDown.Add(-c.down.Load())
	}
}

type countBody struct {
	remaining int64
	eof       bool
	io.ReadCloser
	counts *byteCounts
}

func (b *countBody) Read(p []byte) (int, error) {
	if b.eof || b.remaining == 0 {
		return 0, io.EOF
	}
	if b.remaining > 0 && int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	if b.remaining > 0 {
		b.remaining -= int64(n)
	}
	if err == io.EOF {
		b.eof = true
	}
	b.counts.addUp(int64(n))
	return n, err
}

type countWriter struct {
	counts *byteCounts
	up     bool
	w      io.Writer
	n      *atomic.Int64
}

func (w countWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if w.counts != nil {
		if w.up {
			w.counts.addUp(int64(n))
		} else {
			w.counts.addDown(int64(n))
		}
	} else {
		w.n.Add(int64(n))
	}
	return n, err
}

type observedWriter struct {
	http.ResponseWriter
	event  *Event
	counts *byteCounts
}

func (w *observedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *observedWriter) WriteHeader(code int) {
	if code >= 200 && w.event.Status == 0 {
		w.event.Status = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *observedWriter) Write(p []byte) (int, error) {
	if w.event.Status == 0 {
		w.event.Status = 200
	}
	n, err := w.ResponseWriter.Write(p)
	w.counts.addDown(int64(n))
	return n, err
}
func (w *observedWriter) Flush() { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *observedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, b, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.event.Status = 101
	wrapped := &countConn{Conn: c, counts: w.counts}
	buffered, _ := b.Reader.Peek(b.Reader.Buffered())
	prefix := append([]byte(nil), buffered...)
	w.counts.addUp(int64(len(prefix)))
	return wrapped, bufio.NewReadWriter(bufio.NewReader(io.MultiReader(bytes.NewReader(prefix), wrapped)), bufio.NewWriter(wrapped)), nil
}

type countConn struct {
	net.Conn
	counts *byteCounts
}

func (c *countConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.counts.addUp(int64(n))
	return n, err
}
func (c *countConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.counts.addDown(int64(n))
	return n, err
}
