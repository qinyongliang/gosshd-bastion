package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/relay"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

type Client struct {
	forwardMu       sync.Mutex
	edge            *edgeForwarder
	cfg             Config
	id              string
	assignedAgentID string
}

var errServerRequired = errors.New("server is required")

func New(cfg Config) (*Client, error) {
	var err error
	cfg, err = configureDesktop(cfg)
	if err != nil {
		return nil, err
	}
	cfg.Server = protocol.NormalizeServerURL(cfg.Server)
	if cfg.Server == "" {
		return nil, errServerRequired
	}
	idFile, err := protocol.LoadOrCreateAgentIDFile(cfg.IDFile)
	if err != nil {
		return nil, err
	}
	if cfg.Root == "" {
		cfg.Root, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if cfg.Shell == "" {
		cfg.Shell = protocol.DefaultShell()
	}
	if cfg.SSHHost == "" {
		cfg.SSHHost = os.Getenv("GOSSHD_SSH_HOST")
	}
	if cfg.SSHPort == "" {
		cfg.SSHPort = os.Getenv("GOSSHD_SSH_PORT")
	}
	return &Client{cfg: cfg, id: idFile.ID, assignedAgentID: idFile.AssignedAgentID}, nil
}

// Close releases Agent-owned forwarding workers; session reconnects do not.
func (c *Client) Close() error { c.closeForwarder(); return nil }

func (c *Client) ID() string {
	return c.id
}

func (c *Client) SSHAddress() string {
	host := strings.TrimSpace(c.cfg.SSHHost)
	port := strings.TrimSpace(c.cfg.SSHPort)
	if host != "" {
		if port != "" && port != "22" {
			return fmt.Sprintf("ssh %s@%s -p %s", c.id, host, port)
		}
		return fmt.Sprintf("ssh %s@%s", c.id, host)
	}

	u, err := url.Parse(c.cfg.Server)
	if err != nil || u.Host == "" {
		return fmt.Sprintf("ssh %s@%s", c.id, c.cfg.Server)
	}
	return fmt.Sprintf("ssh %s@%s", c.id, u.Hostname())
}

func (c *Client) Run(ctx context.Context) error {
	defer c.closeForwarder()
	if restarting, err := finalizeLegacyUpdate(); err != nil {
		return err
	} else if restarting {
		return nil
	}
	if _, err := c.forwarder(); err != nil {
		log.Printf("agent forward cache restore failed; awaiting authoritative sync: %v", err)
	}
	backoff := time.Second
	for {
		if err := c.runOnce(ctx); err != nil {
			if errors.Is(err, errAgentRestarting) {
				return nil
			}
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

func NewEmbedded(cfg Config, id string) (*Client, error) {
	var err error
	cfg, err = configureDesktop(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Root == "" {
		cfg.Root, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if cfg.Shell == "" {
		cfg.Shell = protocol.DefaultShell()
	}
	return &Client{cfg: cfg, id: id}, nil
}

func (c *Client) ServeSession(ctx context.Context, session *yamux.Session) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-done:
		}
	}()
	for {
		stream, err := session.Accept()
		if err != nil {
			return err
		}
		go c.handleStreamWithSession(stream, session)
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
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:              c.id,
		AssignedAgentID: c.assignedAgentID,
		EnrollmentToken: c.cfg.EnrollmentToken,
		Version:         c.cfg.Version,
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
	}); err != nil {
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
	if resp.AssignedAgentID != "" {
		c.assignedAgentID = resp.AssignedAgentID
		if err := protocol.SaveAgentAssignment(c.cfg.IDFile, protocol.AgentIDFile{
			AssignedAgentID: resp.AssignedAgentID,
			TargetID:        resp.TargetID,
			TargetAlias:     resp.TargetAlias,
		}); err != nil {
			log.Printf("agent assignment save failed: %v", err)
		}
	}
	if err := c.maybeUpdateAndRestart(ctx, resp); err != nil {
		if errors.Is(err, errAgentRestarting) {
			return err
		}
		// A bad/oversized update must not take the agent offline or trigger a
		// reconnect-and-download loop. Keep serving with the current version.
		log.Printf("agent auto-update skipped: %v", err)
	}

	session, err := yamux.Client(conn, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	log.Printf("agent online: %s", c.SSHAddress())
	return c.ServeSession(ctx, session)
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
