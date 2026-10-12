package forward

import (
	"bufio"
	"context"
	"errors"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
	"io"
	"net"
	"sync"
	"time"
)

// Peer multiplexes new upstream connections over one established negotiated path.
// The exit's dial function resolves immutable route IDs, never caller addresses.
type Peer struct {
	Session    *yamux.Session
	Stable     *tunnel.Conn
	negotiator *tunnel.Negotiator
}

func NewPeer(stream io.ReadWriteCloser, reader *bufio.Reader, entry bool, servers []string) (*Peer, error) {
	stable := tunnel.NewConn(&tunnel.Relay{Reader: reader, Writer: stream, Closer: stream})
	stable.KeepDirectOnRelayLoss()
	n := tunnel.NewNegotiator(stable, entry, servers)
	go stable.Run()
	go n.Run()
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 5 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	var session *yamux.Session
	var err error
	if entry {
		session, err = yamux.Client(stable, cfg)
	} else {
		session, err = yamux.Server(stable, cfg)
	}
	if err != nil {
		n.Close()
		stable.Close()
		return nil, err
	}
	p := &Peer{Session: session, Stable: stable, negotiator: n}
	go func() { <-session.CloseChan(); n.Close(); stable.Close() }()
	return p, nil
}
func (p *Peer) Close() error {
	err := p.Session.Close()
	p.negotiator.Close()
	_ = p.Stable.Close()
	return err
}
func (p *Peer) Snapshot() tunnel.PeerInfo { return p.negotiator.Snapshot() }
func (p *Peer) Alive() bool {
	select {
	case <-p.Session.CloseChan():
		return false
	default:
		return true
	}
}
func (p *Peer) Dial(ctx context.Context, id string) (net.Conn, error) {
	if len(id) > 128 {
		return nil, errors.New("invalid route ID")
	}
	s, err := p.Session.OpenStream()
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	// Route IDs are newline-free by configuration validation.
	if _, err = io.WriteString(s, id+"\n"); err != nil {
		s.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(s, 256)
	answer, err := r.ReadString('\n')
	if err != nil || answer != "ok\n" {
		s.Close()
		return nil, errors.New("upstream route unavailable")
	}
	_ = s.SetDeadline(time.Time{})
	return &peerConn{Stream: s, reader: r}, nil
}
func (p *Peer) Serve(ctx context.Context, dial DialFunc) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	stop := context.AfterFunc(ctx, func() { p.Close() })
	defer stop()
	for {
		s, err := p.Session.AcceptStream()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer s.Close()
			_ = s.SetDeadline(time.Now().Add(10 * time.Second))
			r := bufio.NewReaderSize(s, 256)
			raw, err := r.ReadSlice('\n')
			id := string(raw)
			if err != nil || len(id) > 129 {
				return
			}
			conn, err := dial(ctx, id[:len(id)-1])
			if err != nil {
				io.WriteString(s, "error\n")
				return
			}
			defer conn.Close()
			closeOnCancel := context.AfterFunc(ctx, func() { conn.Close(); s.Close() })
			defer closeOnCancel()
			if _, err := io.WriteString(s, "ok\n"); err != nil {
				return
			}
			_ = s.SetDeadline(time.Time{})
			done := make(chan struct{})
			go func() {
				_, _ = io.Copy(conn, r)
				if cw, ok := conn.(interface{ CloseWrite() error }); ok {
					_ = cw.CloseWrite()
				} else {
					conn.Close()
				}
				close(done)
			}()
			_, _ = io.Copy(s, conn)
			_ = s.Close()
			<-done
		}()
	}
}

type peerConn struct {
	*yamux.Stream
	reader *bufio.Reader
}

func (c *peerConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
func (c *peerConn) CloseWrite() error          { return c.Stream.Close() }
func (c *peerConn) Close() error               { _ = c.Stream.SetReadDeadline(time.Now()); return c.Stream.Close() }
