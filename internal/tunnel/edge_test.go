package tunnel

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestEdgeConnSurvivesRelayLoss(t *testing.T) {
	ra, rb := net.Pipe()
	a := NewConn(&Relay{Reader: ra, Writer: ra, Closer: ra})
	b := NewConn(&Relay{Reader: rb, Writer: rb, Closer: rb})
	a.KeepDirectOnRelayLoss()
	b.KeepDirectOnRelayLoss()
	go a.Run()
	go b.Run()
	t.Cleanup(func() { a.shutdown(); b.shutdown() })
	a.SetDirect(&testPath{peer: b})
	b.SetDirect(&testPath{peer: a})
	deadline := time.Now().Add(3 * time.Second)
	for !a.Direct() || !b.Direct() {
		if time.Now().After(deadline) {
			t.Fatal("no direct path")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ra.Close()
	rb.Close()
	time.Sleep(30 * time.Millisecond)
	payload := bytes.Repeat([]byte("new request while server offline"), 3000)
	transfer(t, a, b, payload)
	transfer(t, b, a, payload)
	if err := a.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if _, err := b.Read(one[:]); err != io.EOF {
		t.Fatal(err)
	}
	transfer(t, b, a, payload)
	a.DisableDirect()
	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("offline lost direct path did not terminate")
	}
}
