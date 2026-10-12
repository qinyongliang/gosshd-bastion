package tunnel

import (
	"bytes"
	"fmt"
	"github.com/hashicorp/yamux"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type impairedEdgePath struct {
	peer      *Conn
	blackhole atomic.Bool
	sequence  atomic.Uint64
}

func (p *impairedEdgePath) Send(packet Packet) error {
	if p.blackhole.Load() {
		return nil
	}
	n := p.sequence.Add(1)
	// Deterministic occasional loss, delay and reordering, including payload ACKs.
	if packet.Kind == Data && n%19 == 0 {
		return nil
	}
	copy := Packet{Kind: packet.Kind, Seq: packet.Seq, Body: append([]byte(nil), packet.Body...)}
	go func() {
		if n%3 == 0 {
			time.Sleep(2 * time.Millisecond)
		}
		p.peer.receive(copy, true)
	}()
	return nil
}
func (p *impairedEdgePath) Close() error { return nil }
func waitEdgeDirect(t *testing.T, a, b *Conn, want bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if a.Direct() == want && b.Direct() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("direct=(%v,%v), want %v", a.Direct(), b.Direct(), want)
}
func TestEdgeMultiplexedDirectRelayMigrationUnderLoad(t *testing.T) {
	// Use real buffered TCP for the relay, matching the control transport.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	left, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	right := <-accepted
	a := NewConn(&Relay{Reader: left, Writer: left, Closer: left})
	b := NewConn(&Relay{Reader: right, Writer: right, Closer: right})
	a.KeepDirectOnRelayLoss()
	b.KeepDirectOnRelayLoss()
	go a.Run()
	go b.Run()
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	cfg.EnableKeepAlive = false
	client, err := yamux.Client(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server, err := yamux.Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close(); a.shutdown(); b.shutdown() })
	var exitWorkers sync.WaitGroup
	exitDone := make(chan struct{})
	go func() {
		defer close(exitDone)
		for {
			stream, err := server.AcceptStream()
			if err != nil {
				return
			}
			exitWorkers.Add(1)
			go func() { defer exitWorkers.Done(); defer stream.Close(); _, _ = io.Copy(stream, stream) }()
		}
	}()
	phase := func(name string) {
		t.Helper()
		const concurrency = 32
		const size = 8192
		var wg sync.WaitGroup
		errs := make(chan error, concurrency)
		started := time.Now()
		for index := 0; index < concurrency; index++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				stream, err := client.OpenStream()
				if err != nil {
					errs <- err
					return
				}
				defer stream.Close()
				stream.SetDeadline(time.Now().Add(8 * time.Second))
				payload := bytes.Repeat([]byte(fmt.Sprintf("%s-%03d", name, index)), size/len(fmt.Sprintf("%s-%03d", name, index)))
				writer := make(chan error, 1)
				go func() { _, err := stream.Write(payload); writer <- err }()
				received := make([]byte, len(payload))
				if _, err := io.ReadFull(stream, received); err != nil {
					errs <- err
					return
				}
				if err := <-writer; err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(payload, received) {
					errs <- fmt.Errorf("corrupt stream %d in %s", index, name)
				}
			}(index)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if t.Failed() {
			t.FailNow()
		}
		t.Logf("phase=%s multiplexed_streams=%d bytes_per_stream~%d elapsed=%s", name, concurrency, size, time.Since(started))
	}
	ab, ba := &impairedEdgePath{peer: b}, &impairedEdgePath{peer: a}
	a.SetDirect(ab)
	b.SetDirect(ba)
	waitEdgeDirect(t, a, b, true)
	phase("direct")
	ab.blackhole.Store(true)
	ba.blackhole.Store(true)
	phase("fallback")
	waitEdgeDirect(t, a, b, false)
	ab.blackhole.Store(false)
	ba.blackhole.Store(false)
	a.SetDirect(ab)
	b.SetDirect(ba)
	waitEdgeDirect(t, a, b, true)
	phase("recovered")
 // Packet loss during the recovered load can transiently fall back again.
 // Sever signaling only after both peers have reconfirmed their direct path.
 waitEdgeDirect(t,a,b,true)
	left.Close()
	right.Close()
	time.Sleep(50 * time.Millisecond)
	phase("server-offline")
	if a.Counters().SentDirect == 0 || b.Counters().SentDirect == 0 {
		t.Fatal("direct payload was not used")
	}
	a.DisableDirect()
	select {
	case <-client.CloseChan():
	case <-time.After(2 * time.Second):
		t.Fatal("offline lost-direct mux did not terminate")
	}
	server.Close()
	<-exitDone
	exitWorkers.Wait()
}
