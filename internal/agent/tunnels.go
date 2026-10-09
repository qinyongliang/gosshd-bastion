package agent

import (
	"bufio"
	"context"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"io"
	"net"
	"sync"
	"time"
)

// The control stream owns the listener: losing it stops accepting connections.
func (c *Client) handleTunnelListen(control io.ReadWriteCloser, reader *bufio.Reader, session *yamux.Session, req protocol.StreamRequest) {
	if session == nil || req.TunnelID == "" {
		_ = protocol.WriteJSONLine(control, protocol.StreamResponse{Error: "tunnel session required"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := openTunnelSSH(ctx, req.TunnelHops)
	if err != nil {
		_ = protocol.WriteJSONLine(control, protocol.StreamResponse{Error: err.Error()})
		return
	}
	if client != nil {
		defer client.Close()
	}
	var listener net.Listener
	if client == nil {
		listener, err = net.Listen("tcp", req.Target)
	} else {
		listener, err = client.Listen("tcp", req.Target)
	}
	if err != nil {
		_ = protocol.WriteJSONLine(control, protocol.StreamResponse{Error: err.Error()})
		return
	}
	defer listener.Close()
	if err = protocol.WriteJSONLine(control, protocol.StreamResponse{OK: true, ListenAddress: listener.Addr().String()}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(io.Discard, reader); _ = listener.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			stream, err := session.OpenStream()
			if err != nil {
				return
			}
			defer stream.Close()
			_ = stream.SetDeadline(time.Now().Add(10 * time.Second))
			if protocol.WriteJSONLine(stream, protocol.StreamRequest{Type: protocol.StreamTunnelConnection, TunnelID: req.TunnelID, Peer: req.Peer}) != nil {
				return
			}
			responseReader := bufio.NewReader(stream)
			response, err := protocol.ReadJSONLine[protocol.StreamResponse](responseReader)
			if err != nil || !response.OK {
				return
			}
			_ = stream.SetDeadline(time.Time{})
			if response.Peer {
				serveTunnelPeer(conn, stream, responseReader, true, response.STUNServers)
			} else {
				bridgeTunnel(conn, &tunnelStream{Reader: responseReader, stream: stream})
			}
		}()
	}
}

// yamux.Close sends a write-side FIN; reading can continue until the peer closes.
type tunnelStream struct {
	Reader io.Reader
	stream *yamux.Stream
}

func (s *tunnelStream) Read(b []byte) (int, error)  { return s.Reader.Read(b) }
func (s *tunnelStream) Write(b []byte) (int, error) { return s.stream.Write(b) }
func (s *tunnelStream) CloseWrite() error           { return s.stream.Close() }
func (s *tunnelStream) Close() error {
	_ = s.stream.SetReadDeadline(time.Now())
	return s.stream.Close()
}

func bridgeTunnel(conn net.Conn, stream io.ReadWriteCloser) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = conn.Close(); _ = stream.Close() }) }
	defer closeBoth()
	var wg sync.WaitGroup
	wg.Add(2)
	copySide := func(dst io.Writer, src io.Reader) {
		defer wg.Done()
		_, err := io.Copy(dst, src)
		if err != nil {
			closeBoth()
			return
		}
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			closeBoth()
		}
	}
	go copySide(stream, conn)
	go copySide(conn, stream)
	wg.Wait()
}
