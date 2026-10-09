package tunnel

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type testPath struct {
	peer  *Conn
	drop  atomic.Bool
	count atomic.Uint64
}

func (p *testPath) Send(packet Packet) error {
	if p.drop.Load() {
		return nil
	}
	n := p.count.Add(1)
	if packet.Kind == Data && n%7 == 0 {
		return nil
	}
	packet.Body = append([]byte(nil), packet.Body...)
	go func() {
		if n%3 == 0 {
			time.Sleep(time.Millisecond * 2)
		}
		p.peer.receive(packet, true)
	}()
	return nil
}
func (p *testPath) Close() error { return nil }
func testPair(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	a, b := net.Pipe()
	left := NewConn(&Relay{Reader: a, Writer: a, Closer: a})
	right := NewConn(&Relay{Reader: b, Writer: b, Closer: b})
	go left.Run()
	go right.Run()
	t.Cleanup(func() { left.shutdown(); right.shutdown() })
	return left, right
}
func transfer(t *testing.T, a, b *Conn, data []byte) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := a.Write(data); done <- err }()
	received := make([]byte, len(data))
	if _, err := io.ReadFull(b, received); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, data) {
		t.Fatal("payload changed during path migration")
	}
}
func TestStableConnPathMigrationLossAndHalfClose(t *testing.T) {
	a, b := testPair(t)
	payload := make([]byte, 256*1024)
	rand.Read(payload)
	transfer(t, a, b, payload)
	ab, ba := &testPath{peer: b}, &testPath{peer: a}
	a.SetDirect(ab)
	b.SetDirect(ba)
	transfer(t, a, b, payload)
	transfer(t, b, a, payload)
	ab.drop.Store(true)
	ba.drop.Store(true)
	transfer(t, a, b, payload)
	transfer(t, b, a, payload)
	if err := a.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if _, err := b.Read(one[:]); err != io.EOF {
		t.Fatalf("expected half-close EOF, got %v", err)
	}
	transfer(t, b, a, payload)
	if a.Counters().SentDirect == 0 || a.Counters().ReceivedDirect == 0 {
		t.Fatal("direct traffic was not reported")
	}
}
func TestWebRTCUpgradeAndFallbackPreserveStream(t *testing.T) {
	a, b := testPair(t)
	left, right := NewNegotiator(a, true, nil), NewNegotiator(b, false, nil)
	defer left.Close()
	defer right.Close()
	go left.Run()
	go right.Run()
	payload := bytes.Repeat([]byte("same TCP connection\x00"), 2000)
	transfer(t, a, b, payload)
	deadline := time.Now().Add(15 * time.Second)
	for !a.Direct() || !b.Direct() {
		if time.Now().After(deadline) {
			t.Fatal("LAN ICE direct path did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, n := range []*Negotiator{left, right} {
		info := n.Snapshot()
		if !info.Active || info.Local == nil || info.Remote == nil || info.Local.Interface == "" || info.Local.LocalAddress == "" {
			t.Fatalf("selected route missing interface details: %+v", info)
		}
	}
	transfer(t, a, b, payload)
	transfer(t, b, a, payload)
	left.Close()
	right.Close()
	transfer(t, a, b, payload)
	transfer(t, b, a, payload)
}
func TestMalformedRelayPacketRejected(t *testing.T) {
	relay := &Relay{Reader: bytes.NewReader([]byte{255, 255, 255, 255})}
	if _, err := relay.Read(); err == nil {
		t.Fatal("unbounded packet accepted")
	}
}
