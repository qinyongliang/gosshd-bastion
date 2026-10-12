package tunnel

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
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
	deadline := time.Now().Add(3 * time.Second)
	for !a.Direct() || !b.Direct() {
		if time.Now().After(deadline) {
			t.Fatal("installed paths did not confirm direct connectivity")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	if a.Direct() || b.Direct() {
		t.Fatal("closed negotiators must fall back to relay immediately")
	}
	transfer(t, a, b, payload)
	transfer(t, b, a, payload)
}
func TestMalformedRelayPacketRejected(t *testing.T) {
	relay := &Relay{Reader: bytes.NewReader([]byte{255, 255, 255, 255})}
	if _, err := relay.Read(); err == nil {
		t.Fatal("unbounded packet accepted")
	}
}

type probeOnlyPath struct {
	packets []Packet
}

func (p *probeOnlyPath) Send(packet Packet) error {
	p.packets = append(p.packets, packet)
	return nil
}
func (p *probeOnlyPath) Close() error { return nil }

func TestDirectPathRequiresDirectProbeConfirmation(t *testing.T) {
	c := NewConn(nil)
	path := &probeOnlyPath{}
	c.SetDirect(path)
	if c.Direct() {
		t.Fatal("installing a local path must not activate it before peer confirmation")
	}
	if len(path.packets) != 1 || path.packets[0].Kind != Probe {
		t.Fatal("new paths must immediately probe the peer")
	}
	c.receive(Packet{Kind: ProbeAck}, false)
	if c.Direct() {
		t.Fatal("a relayed ACK cannot confirm direct connectivity")
	}
	c.receive(Packet{Kind: Probe}, true)
	if len(path.packets) != 3 || path.packets[1].Kind != ProbeAck || path.packets[2].Kind != Probe {
		t.Fatal("remote readiness must trigger immediate re-probing, not a timer wait")
	}
	c.receive(Packet{Kind: ProbeAck}, true)
	if !c.Direct() {
		t.Fatal("direct peer confirmation must activate the installed path")
	}
}

// Captures relay writes without waiting for an ICE/STUN response.
type signalCaptureWriter struct{ frames chan []byte }

func (w signalCaptureWriter) Write(b []byte) (int, error) {
	w.frames <- append([]byte(nil), b...)
	return len(b), nil
}

func TestSDPDoesNotWaitForICEGathering(t *testing.T) {
	frames := make(chan []byte, 64)
	c := NewConn(&Relay{Writer: signalCaptureWriter{frames}})
	n := NewNegotiator(c, true, []string{"stun:127.0.0.1:9"})
	defer n.Close()
	pc, err := n.newPeer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.CreateDataChannel("tcp-tunnel", nil); err != nil {
		t.Fatal(err)
	}
	sdp, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- n.description(pc, sdp) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("SDP startup waited for ICE gathering instead of sending immediately")
	}
}

func TestTrickleCandidatesBufferUntilRemoteDescriptionAndRejectOldGeneration(t *testing.T) {
	c := NewConn(&Relay{Writer: signalCaptureWriter{make(chan []byte, 64)}})
	n := NewNegotiator(c, false, nil)
	defer n.Close()
	mid, index := "0", uint16(0)
	candidate := webrtc.ICECandidateInit{Candidate: "candidate:1 1 udp 2130706431 127.0.0.1 5000 typ host", SDPMid: &mid, SDPMLineIndex: &index}
	n.candidate(signalMessage{Type: "candidate", Candidate: &candidate, Generation: 1})
	if len(n.pending[1]) != 1 {
		t.Fatal("candidate arriving before SDP was not buffered")
	}
	pc, err := n.newPeer(1)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := remote.CreateDataChannel("tcp-tunnel", nil); err != nil {
		t.Fatal(err)
	}
	offer, err := remote.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.remote(pc, offer); err != nil {
		t.Fatal(err)
	}
	if len(n.pending[1]) != 0 {
		t.Fatal("remote SDP did not drain its queued candidates")
	}
	n.candidate(signalMessage{Type: "candidate", Candidate: &candidate, Generation: 0})
	if len(n.pending[0]) != 0 {
		t.Fatal("retired negotiation candidates were retained")
	}
}

func TestPendingTrickleCandidatesAreBounded(t *testing.T) {
	n := NewNegotiator(NewConn(nil), false, nil)
	candidate := webrtc.ICECandidateInit{Candidate: "candidate:1 1 udp 1 127.0.0.1 5000 typ host"}
	for i := 0; i < maxPendingCandidates*2; i++ {
		n.candidate(signalMessage{Type: "candidate", Candidate: &candidate, Generation: uint64(i%3 + 1)})
	}
	count := 0
	for _, candidates := range n.pending {
		count += len(candidates)
	}
	if count != maxPendingCandidates {
		t.Fatalf("candidate buffer is not bounded: got %d", count)
	}
}
