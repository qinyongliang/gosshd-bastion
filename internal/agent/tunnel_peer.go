package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/tunnel"
	ssh "golang.org/x/crypto/ssh"
)

// Delegated SSH hops are scoped by the server to this tunnel and pinned to verified host keys.
func openTunnelSSH(ctx context.Context, hops []protocol.TunnelSSHHop) (*ssh.Client, error) {
	if len(hops) > 4 {
		return nil, errors.New("SSH chain too deep")
	}
	var client *ssh.Client
	for _, hop := range hops {
		var auth []ssh.AuthMethod
		if hop.AuthType == "private_key" {
			signer, err := ssh.ParsePrivateKey(hop.Secret)
			if err != nil {
				if client != nil {
					client.Close()
				}
				return nil, err
			}
			auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
		} else {
			password := string(hop.Secret)
			auth = []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(func(_, _ string, q []string, _ []bool) ([]string, error) {
				a := make([]string, len(q))
				for i := range a {
					a[i] = password
				}
				return a, nil
			})}
		}
		var conn net.Conn
		var err error
		parent := client
		if parent == nil {
			conn, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", hop.Address)
		} else {
			conn, err = parent.DialContext(ctx, "tcp", hop.Address)
		}
		if err != nil {
			if parent != nil {
				parent.Close()
			}
			return nil, err
		}
		chain := &tunnelSSHConn{Conn: conn, parent: parent}
		stop := context.AfterFunc(ctx, func() { chain.Close() })
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		cfg := &ssh.ClientConfig{User: hop.Username, Auth: auth, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if len(hop.HostKey) == 0 || !bytes.Equal(key.Marshal(), hop.HostKey) {
				return errors.New("SSH host key changed")
			}
			return nil
		}}
		cc, ch, req, err := ssh.NewClientConn(chain, hop.Address, cfg)
		stop()
		if err != nil {
			chain.Close()
			return nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		client = ssh.NewClient(cc, ch, req)
	}
	return client, nil
}

type tunnelSSHConn struct {
	net.Conn
	parent *ssh.Client
}

func (c *tunnelSSHConn) Close() error {
	err := c.Conn.Close()
	if c.parent != nil {
		c.parent.Close()
	}
	return err
}

func (c *Client) handleTunnelPeer(stream io.ReadWriteCloser, reader *bufio.Reader, req protocol.StreamRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := openTunnelSSH(ctx, req.TunnelHops)
	if client != nil {
		defer client.Close()
	}
	var conn net.Conn
	if err == nil {
		if client == nil {
			conn, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", req.Target)
		} else {
			conn, err = client.DialContext(ctx, "tcp", req.Target)
		}
	}
	if err != nil {
		_ = protocol.WriteJSONLine(stream, protocol.StreamResponse{Error: err.Error()})
		return
	}
	defer conn.Close()
	if protocol.WriteJSONLine(stream, protocol.StreamResponse{OK: true, Peer: true}) != nil {
		return
	}
	serveTunnelPeer(conn, stream, reader, false, req.STUNServers)
}
func serveTunnelPeer(conn net.Conn, stream io.ReadWriteCloser, reader *bufio.Reader, entry bool, servers []string) {
	relay := &tunnel.Relay{Reader: reader, Writer: stream, Closer: stream}
	stable := tunnel.NewConn(relay)
	negotiator := tunnel.NewNegotiator(stable, entry, servers)
	defer negotiator.Close()
	defer stable.Close()
	done := make(chan struct{})
	defer close(done)
	go stable.Run()
	go func() {
		select {
		case <-stable.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	go negotiator.Run()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if entry && stable.ReportStats() != nil {
					return
				}
				if negotiator.Report() != nil {
					return
				}
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(stable, conn); _ = stable.CloseWrite() }()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(conn, stable)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = conn.Close()
		}
	}()
	wg.Wait()
}
