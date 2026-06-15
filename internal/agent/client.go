package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/qinyongliang/gosshd/internal/protocol"
	"github.com/qinyongliang/gosshd/internal/relay"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

type Client struct {
	cfg Config
	id  string
}

func New(cfg Config) (*Client, error) {
	id, err := protocol.LoadOrCreateID(cfg.IDFile)
	if err != nil {
		return nil, err
	}
	cfg.Server = protocol.NormalizeServerURL(cfg.Server)
	if cfg.Root == "" {
		cfg.Root = "/"
	}
	if cfg.Shell == "" {
		cfg.Shell = protocol.DefaultShell()
	}
	return &Client{cfg: cfg, id: id}, nil
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) SSHAddress() string {
	u, err := url.Parse(c.cfg.Server)
	if err != nil || u.Host == "" {
		return fmt.Sprintf("ssh %s@%s", c.id, c.cfg.Server)
	}
	return fmt.Sprintf("ssh %s@%s", c.id, u.Hostname())
}

func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if err := c.runOnce(ctx); err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			log.Printf("agent reconnecting after error: %v", err)
			continue
		}
		backoff = time.Second
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	wsURL, err := c.wsURL()
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	ws, _, err := dialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		return err
	}
	conn := relay.NewWSConn(ws)
	defer conn.Close()
	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{ID: c.id, Token: c.cfg.Token}); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("server rejected agent: %s", resp.Error)
	}

	session, err := yamux.Client(conn, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	log.Printf("agent online: %s", c.SSHAddress())
	for {
		stream, err := session.Accept()
		if err != nil {
			return err
		}
		go c.handleStream(stream)
	}
}

func (c *Client) wsURL() (string, error) {
	u, err := url.Parse(c.cfg.Server)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported server scheme %q", u.Scheme)
	}
	u.Path = protocol.WebSocketPath
	u.RawQuery = ""
	return u.String(), nil
}
