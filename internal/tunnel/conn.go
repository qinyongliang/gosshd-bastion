package tunnel

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type pendingPacket struct {
	packet Packet
	last   time.Time
}
type directPath interface {
	Send(Packet) error
	Close() error
}
type Counters struct {
	SentDirect     uint64
	ReceivedDirect uint64
}

type Conn struct {
	finReadSeq                 uint64
	relay                      *Relay
	done                       chan struct{}
	once                       sync.Once
	mu                         sync.Mutex
	changed                    chan struct{}
	next                       uint64
	receiveNext                uint64
	pending                    map[uint64]pendingPacket
	buffered                   map[uint64]Packet
	direct                     directPath
	useDirect                  bool
	sentDirect, receivedDirect atomic.Uint64
	writeMu                    sync.Mutex
	receiveMu                  sync.Mutex
	delivered                  [window * 2]struct {
		seq  uint64
		path byte
	}
	readEOF       bool
	readMu        sync.Mutex
	readBuffer    []byte
	incoming      chan Packet
	closedWrite   bool
	SignalHandler func([]byte)
	// File operations carry server-authorized paths and must never arrive via P2P.
	FileOperationHandler func([]byte)
}

func NewConn(relay *Relay) *Conn {
	c := &Conn{relay: relay, done: make(chan struct{}), changed: make(chan struct{}), next: 1, receiveNext: 1, pending: map[uint64]pendingPacket{}, buffered: map[uint64]Packet{}, incoming: make(chan Packet, window)}
	return c
}
func (c *Conn) Done() <-chan struct{} { return c.done }
func (c *Conn) Run() {
	go c.retransmit()
	for {
		p, err := c.relay.Read()
		if err != nil {
			c.shutdown()
			return
		}
		c.receive(p, false)
	}
}
func (c *Conn) notify() { close(c.changed); c.changed = make(chan struct{}) }
func (c *Conn) send(p Packet) error {
	c.mu.Lock()
	path := c.direct
	active := c.useDirect
	c.mu.Unlock()
	if active && path != nil {
		if err := path.Send(p); err == nil {
			return nil
		}
		c.DisableDirect()
	}
	return c.relay.Send(p)
}
func (c *Conn) queue(kind byte, body []byte) error {
	for {
		c.mu.Lock()
		if len(c.pending) < window {
			select {
			case <-c.done:
				c.mu.Unlock()
				return net.ErrClosed
			default:
			}
			p := Packet{Kind: kind, Seq: c.next, Body: append([]byte(nil), body...)}
			c.next++
			c.pending[p.Seq] = pendingPacket{p, time.Now()}
			c.mu.Unlock()
			if err := c.send(p); err != nil {
				c.shutdown()
				return err
			}
			return nil
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-c.done:
			return net.ErrClosed
		}
	}
}
func (c *Conn) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closedWrite {
		return 0, io.ErrClosedPipe
	}
	n := 0
	for len(b) > 0 {
		k := min(len(b), MaxPayload)
		if err := c.queue(Data, b[:k]); err != nil {
			return n, err
		}
		n += k
		b = b[k:]
	}
	return n, nil
}
func (c *Conn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(b) == 0 {
		return 0, nil
	}
	if c.readEOF {
		return 0, io.EOF
	}
	for len(c.readBuffer) == 0 {
		select {
		case p := <-c.incoming:
			if p.Kind == Fin {
				c.readEOF = true
				c.receiveMu.Lock()
				c.finReadSeq = p.Seq
				slot := c.delivered[p.Seq%uint64(len(c.delivered))]
				c.receiveMu.Unlock()
				path := byte(0)
				if slot.seq == p.Seq {
					path = slot.path
				}
				_ = c.relay.Send(Packet{Kind: Ack, Seq: p.Seq, Body: []byte{path}})
				return 0, io.EOF
			}
			c.readBuffer = p.Body
		case <-c.done:
			return 0, io.EOF
		}
	}
	n := copy(b, c.readBuffer)
	c.readBuffer = c.readBuffer[n:]
	return n, nil
}
func (c *Conn) CloseWrite() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closedWrite {
		return nil
	}
	c.closedWrite = true
	return c.queue(Fin, nil)
}
func (c *Conn) receive(p Packet, direct bool) {
	switch p.Kind {
	case Data, Fin:
		if len(p.Body) > MaxPayload {
			c.shutdown()
			return
		}
		c.receiveMu.Lock()
		if p.Seq >= c.receiveNext+window {
			c.receiveMu.Unlock()
			return
		}
		_, exists := c.buffered[p.Seq]
		fresh := p.Seq >= c.receiveNext && !exists
		slot := &c.delivered[p.Seq%uint64(len(c.delivered))]
		if fresh {
			c.buffered[p.Seq] = p
			slot.seq = p.Seq
			slot.path = 0
			if direct {
				slot.path = 1
				if p.Kind == Data {
					c.receivedDirect.Add(uint64(len(p.Body)))
				}
			}
		}
		path := byte(2)
		if slot.seq == p.Seq {
			path = slot.path
		}
		for {
			item, ok := c.buffered[c.receiveNext]
			if !ok {
				break
			}
			select {
			case c.incoming <- item:
				delete(c.buffered, c.receiveNext)
				c.receiveNext++
			case <-c.done:
				c.receiveMu.Unlock()
				return
			}
		}
		finConsumed := p.Seq <= c.finReadSeq
		c.receiveMu.Unlock()
		if p.Kind != Fin || finConsumed {
			c.acknowledge(p.Seq, path, direct && p.Kind == Data && path == 1)
		}
	case Ack:
		c.mu.Lock()
		if pending, ok := c.pending[p.Seq]; ok {
			if len(p.Body) == 1 && p.Body[0] == 1 && pending.packet.Kind == Data {
				c.sentDirect.Add(uint64(len(pending.packet.Body)))
			}
			delete(c.pending, p.Seq)
			c.notify()
		}
		c.mu.Unlock()
	case Signal:
		if c.SignalHandler != nil {
			c.SignalHandler(p.Body)
		}
	case FileOperation:
		if !direct && c.FileOperationHandler != nil {
			c.FileOperationHandler(p.Body)
		}
	case Probe:
		c.mu.Lock()
		path := c.direct
		c.mu.Unlock()
		if path != nil {
			_ = path.Send(Packet{Kind: ProbeAck})
		}
	case ProbeAck:
		c.mu.Lock()
		if c.direct != nil {
			c.useDirect = true
		}
		c.mu.Unlock()
	}
}

// Direct payload ACKs stay on the direct path so the relay RTT does not limit throughput.
// ACKs for relayed packets and their duplicates still cross the server for exact accounting.
func (c *Conn) acknowledge(seq uint64, path byte, preferDirect bool) {
	packet := Packet{Kind: Ack, Seq: seq, Body: []byte{path}}
	if preferDirect {
		c.mu.Lock()
		direct := c.direct
		c.mu.Unlock()
		if direct != nil && direct.Send(packet) == nil {
			return
		}
	}
	_ = c.relay.Send(packet)
}
func (c *Conn) retransmit() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-c.done:
			return
		case now := <-ticker.C:
			c.mu.Lock()
			var retry []Packet
			for seq, p := range c.pending {
				if now.Sub(p.last) > time.Second {
					retry = append(retry, p.packet)
					p.last = now
					c.pending[seq] = p
					c.useDirect = false
				}
			}
			path := c.direct
			c.mu.Unlock()
			for _, p := range retry {
				if c.relay.Send(p) != nil {
					c.shutdown()
					return
				}
			}
			ticks++
			if path != nil && ticks%4 == 0 {
				_ = path.Send(Packet{Kind: Probe})
			}
		}
	}
}
func (c *Conn) SetDirect(path directPath) {
	c.mu.Lock()
	old := c.direct
	c.direct = path
	c.useDirect = true
	c.mu.Unlock()
	if old != nil && old != path {
		_ = old.Close()
	}
}
func (c *Conn) DisableDirect()     { c.mu.Lock(); c.useDirect = false; c.mu.Unlock() }
func (c *Conn) Direct() bool       { c.mu.Lock(); defer c.mu.Unlock(); return c.useDirect && c.direct != nil }
func (c *Conn) Counters() Counters { return Counters{c.sentDirect.Load(), c.receivedDirect.Load()} }
func (c *Conn) ReportStats() error {
	n := c.Counters()
	b := make([]byte, 17)
	binary.BigEndian.PutUint64(b, n.SentDirect)
	binary.BigEndian.PutUint64(b[8:], n.ReceivedDirect)
	if c.Direct() {
		b[16] = 1
	}
	return c.relay.Send(Packet{Kind: Stats, Body: b})
}
func (c *Conn) shutdown() {
	c.once.Do(func() {
		close(c.done)
		_ = c.relay.Close()
		c.mu.Lock()
		d := c.direct
		c.direct = nil
		c.notify()
		c.mu.Unlock()
		if d != nil {
			_ = d.Close()
		}
	})
}
func (c *Conn) Close() error {
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		c.mu.Lock()
		empty := len(c.pending) == 0
		changed := c.changed
		c.mu.Unlock()
		if empty {
			break
		}
		select {
		case <-changed:
		case <-deadline.C:
			c.shutdown()
			return nil
		case <-c.done:
			return nil
		}
	}
	_ = c.ReportStats()
	c.shutdown()
	return nil
}
func (c *Conn) LocalAddr() net.Addr  { return addr("tunnel-local") }
func (c *Conn) RemoteAddr() net.Addr { return addr("tunnel-peer") }
func (c *Conn) SetDeadline(time.Time) error {
	return errors.New("tunnel deadlines are controlled by the owning context")
}
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.SetDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

type addr string

func (a addr) Network() string { return "tunnel" }
func (a addr) String() string  { return string(a) }
