package forward

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type portEvents struct {
	mu     sync.Mutex
	events []Event
}

func (l *portEvents) Record(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}
func udpEcho(t *testing.T) net.PacketConn {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, a, e := c.ReadFrom(buf)
			if e != nil {
				return
			}
			c.WriteTo(buf[:n], a)
		}
	}()
	return c
}
func TestPortUDPDatagramBoundariesAndSources(t *testing.T) {
	echo := udpEcho(t)
	logs := &portEvents{}
	r, err := StartPort(context.Background(), "127.0.0.1:0", []string{"udp"}, Location{ID: "port"}, func(ctx context.Context, id string) (net.Conn, error) {
		if id != "udp" {
			t.Errorf("route %s", id)
		}
		return DialUDP(ctx, echo.LocalAddr().String())
	}, logs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var clients []net.Conn
	for i := 0; i < 2; i++ {
		c, e := net.Dial("udp", r.State().ListenAddress)
		if e != nil {
			t.Fatal(e)
		}
		clients = append(clients, c)
		defer c.Close()
	}
	for _, payload := range [][]byte{[]byte("first"), {}, bytes.Repeat([]byte("large\x00\xff"), 160), []byte("last")} {
		// UDP bursts may legitimately overflow kernel receive queues (notably
		// two 63KB packets on Linux); verify each source without assuming delivery.
		for _, c := range clients {
			c.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := c.Write(payload); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 65535)
			n, err := c.Read(buf)
			if err != nil || !bytes.Equal(buf[:n], payload) {
				t.Fatalf("datagram len=%d expected=%d: %v", n, len(payload), err)
			}
		}
	}
	if s := r.State(); s.Active != 2 || s.Sources["127.0.0.1"] != 2 {
		t.Fatalf("state %+v", s)
	}
	r.Close()
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if len(logs.events) != 2 {
		t.Fatalf("events %d", len(logs.events))
	}
	for _, e := range logs.events {
		if e.Method != "UDP" || e.Up == 0 || e.Down == 0 {
			t.Fatalf("event %+v", e)
		}
	}
}
func TestDatagramStreamFragmentedAndCoalescedWrites(t *testing.T) {
	echo := udpEcho(t)
	c, e := DialUDP(context.Background(), echo.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	wire := []byte{0, 3, 'o', 'n', 'e', 0, 0, 0, 3, 't', 'w', 'o'}
	for _, part := range [][]byte{wire[:1], wire[1:4], wire[4:]} {
		if n, e := c.Write(part); e != nil || n != len(part) {
			t.Fatalf("write %d %v", n, e)
		}
	}
	got := make([]byte, len(wire))
	if _, e := io.ReadFull(c, got); e != nil || !bytes.Equal(got, wire) {
		t.Fatalf("frames %v %v", got, e)
	}
}

func TestPortUDPIdleExpiryAndSessionRecreation(t *testing.T) {
	echo := udpEcho(t)
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logs := &portEvents{}
	r := startUDP(context.Background(), packet, Location{ID: "udp"}, func(ctx context.Context, _ string) (net.Conn, error) { return DialUDP(ctx, echo.LocalAddr().String()) }, logs, nil, 100*time.Millisecond, newRuntimeCounters())
	defer r.Close()
	c, err := net.Dial("udp", packet.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for attempt := 0; attempt < 2; attempt++ {
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write([]byte("idle"))
		got := make([]byte, 4)
		if _, err := c.Read(got); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for r.State().Active != 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if r.State().Active != 0 {
			t.Fatal("idle session not released")
		}
	}
	r.Close()
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if len(logs.events) != 2 {
		t.Fatalf("session recreation events %d", len(logs.events))
	}
	for _, e := range logs.events {
		if e.Error != "" {
			t.Fatalf("idle is not an error: %+v", e)
		}
	}
}
func TestPortPartialBindFailureReleasesTCP(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, err = StartPort(context.Background(), occupied.LocalAddr().String(), []string{"tcp", "udp"}, Location{}, nil, &portEvents{}, nil, nil)
	if err == nil {
		t.Fatal("accepted occupied UDP port")
	}
	listener, err := net.Listen("tcp", occupied.LocalAddr().String())
	if err != nil {
		t.Fatalf("TCP bind was leaked: %v", err)
	}
	listener.Close()
}

// The packet stub tests the maximum frame independent of OS MTU/firewall rules.
type datagramStub struct {
	net.Conn
	packets [][]byte
}

func (c *datagramStub) Write(b []byte) (int, error) {
	c.packets = append(c.packets, append([]byte(nil), b...))
	return len(b), nil
}
func (c *datagramStub) Read(b []byte) (int, error) {
	if len(c.packets) == 0 {
		return 0, io.EOF
	}
	p := c.packets[0]
	c.packets = c.packets[1:]
	return copy(b, p), nil
}
func TestDatagramMaximumFrame(t *testing.T) {
	raw := &datagramStub{}
	c := &datagramStream{Conn: raw}
	payload := bytes.Repeat([]byte{0xff}, maxDatagramSize)
	if err := writeDatagram(c, payload); err != nil {
		t.Fatal(err)
	}
	if len(raw.packets) != 1 || !bytes.Equal(raw.packets[0], payload) {
		t.Fatal("max datagram truncated")
	}
	got := make([]byte, maxDatagramSize)
	n, err := readDatagram(c, got)
	if err != nil || n != len(payload) || !bytes.Equal(got, payload) {
		t.Fatalf("max frame %d %v", n, err)
	}
	if err := writeDatagram(c, append(payload, 0)); err == nil {
		t.Fatal("oversize datagram accepted")
	}
}

func TestPortCombinedCounters(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		c, e := echo.Accept()
		if e == nil {
			defer c.Close()
			io.Copy(c, c)
		}
	}()
	udp := udpEcho(t)
	r, err := StartPort(context.Background(), "127.0.0.1:0", []string{"tcp", "udp"}, Location{}, func(ctx context.Context, id string) (net.Conn, error) {
		if id == "udp" {
			return DialUDP(ctx, udp.LocalAddr().String())
		}
		return DialNetwork(ctx, "tcp", echo.Addr().String())
	}, &portEvents{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, network := range []string{"tcp", "udp"} {
		c, e := net.Dial(network, r.State().ListenAddress)
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write([]byte("port"))
		if _, e := io.ReadFull(c, make([]byte, 4)); e != nil {
			t.Fatal(e)
		}
	}
	s := r.State()
	if s.Active != 2 || s.Peak != 2 || s.Requests != 2 || s.Sources["127.0.0.1"] != 2 {
		t.Fatalf("shared transport counters %+v", s)
	}
}
