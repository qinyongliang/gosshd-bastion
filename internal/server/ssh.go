package server

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/qinyongliang/gosshd/internal/protocol"

	gossh "golang.org/x/crypto/ssh"
)

type tcpipForwardPayload struct {
	Address string
	Port    uint32
}

type directTCPIPPayload struct {
	HostToConnect     string
	PortToConnect     uint32
	OriginatorAddress string
	OriginatorPort    uint32
}

type forwardedTCPIPPayload struct {
	ConnectedAddress  string
	ConnectedPort     uint32
	OriginatorAddress string
	OriginatorPort    uint32
}

func (a *App) serveSSH(ln net.Listener) error {
	cfg, err := sshServerConfig()
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go a.handleSSHConn(conn, cfg)
	}
}

func sshServerConfig() (*gossh.ServerConfig, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("make host signer: %w", err)
	}
	cfg := &gossh.ServerConfig{
		NoClientAuth:  true,
		ServerVersion: "SSH-2.0-gosshd",
	}
	cfg.AddHostKey(signer)
	return cfg, nil
}

func (a *App) handleSSHConn(raw net.Conn, cfg *gossh.ServerConfig) {
	defer raw.Close()
	conn, chans, reqs, err := gossh.NewServerConn(raw, cfg)
	if err != nil {
		return
	}
	defer conn.Close()

	id := conn.User()
	if !protocol.IsValidID(id) {
		return
	}
	if _, err := a.registry.Get(id); err != nil {
		return
	}

	forwards := newForwardManager(conn)
	go forwards.handleGlobalRequests(reqs)
	for ch := range chans {
		switch ch.ChannelType() {
		case "session":
			go a.handleSessionChannel(id, ch)
		case "direct-tcpip":
			go a.handleDirectTCPIP(id, ch)
		default:
			_ = ch.Reject(gossh.UnknownChannelType, "unsupported channel type")
		}
	}
	forwards.closeAll()
}

func (a *App) openAgentStream(id string, req protocol.StreamRequest) (*bufio.Reader, io.ReadWriteCloser, error) {
	session, err := a.registry.Get(id)
	if err != nil {
		return nil, nil, err
	}
	stream, err := session.Open()
	if err != nil {
		return nil, nil, err
	}
	if err := protocol.WriteJSONLine(stream, req); err != nil {
		_ = stream.Close()
		return nil, nil, err
	}
	reader := bufio.NewReader(stream)
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil {
		_ = stream.Close()
		return nil, nil, err
	}
	if !resp.OK {
		_ = stream.Close()
		return nil, nil, errors.New(resp.Error)
	}
	return reader, stream, nil
}

func (a *App) handleSessionChannel(id string, newCh gossh.NewChannel) {
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	go a.handleSessionRequests(id, ch, reqs)
}

func (a *App) handleSessionRequests(id string, ch gossh.Channel, reqs <-chan *gossh.Request) {
	defer ch.Close()
	ptyWidth, ptyHeight := 80, 24
	started := false
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			ptyWidth, ptyHeight = parsePtySize(req.Payload)
			req.Reply(true, nil)
		case "window-change":
			ptyWidth, ptyHeight = parseWindowChange(req.Payload)
		case "shell":
			if started {
				req.Reply(false, nil)
				continue
			}
			started = true
			req.Reply(true, nil)
			a.bridgeFramedSession(id, ch, protocol.StreamRequest{Type: protocol.StreamShell, Width: ptyWidth, Height: ptyHeight})
			return
		case "exec":
			if started {
				req.Reply(false, nil)
				continue
			}
			var payload struct{ Command string }
			if err := gossh.Unmarshal(req.Payload, &payload); err != nil {
				req.Reply(false, nil)
				continue
			}
			started = true
			req.Reply(true, nil)
			a.bridgeFramedSession(id, ch, protocol.StreamRequest{Type: protocol.StreamExec, Command: payload.Command, Width: ptyWidth, Height: ptyHeight})
			return
		case "subsystem":
			if started {
				req.Reply(false, nil)
				continue
			}
			var payload struct{ Name string }
			if err := gossh.Unmarshal(req.Payload, &payload); err != nil || payload.Name != "sftp" {
				req.Reply(false, nil)
				continue
			}
			started = true
			req.Reply(true, nil)
			a.bridgeRaw(id, ch, protocol.StreamRequest{Type: protocol.StreamSFTP})
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (a *App) bridgeFramedSession(id string, ch gossh.Channel, req protocol.StreamRequest) {
	reader, stream, err := a.openAgentStream(id, req)
	if err != nil {
		sendExit(ch, 255)
		return
	}
	defer stream.Close()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ch.Read(buf)
			if n > 0 {
				if writeErr := protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameStdin, Data: append([]byte(nil), buf[:n]...)}); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	exitCode := 0
	for {
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			exitCode = 255
			break
		}
		switch frame.Type {
		case protocol.FrameStdout:
			_, _ = ch.Write(frame.Data)
		case protocol.FrameStderr:
			_, _ = ch.Stderr().Write(frame.Data)
		case protocol.FrameExit:
			exitCode = protocol.ExitCode(frame)
			sendExit(ch, exitCode)
			return
		}
	}
	sendExit(ch, exitCode)
}

func (a *App) bridgeRaw(id string, ch gossh.Channel, req protocol.StreamRequest) {
	reader, stream, err := a.openAgentStream(id, req)
	if err != nil {
		return
	}
	defer stream.Close()
	bridge(ch, struct {
		io.Reader
		io.Writer
		io.Closer
	}{Reader: reader, Writer: stream, Closer: stream})
}

func (a *App) handleDirectTCPIP(id string, newCh gossh.NewChannel) {
	var payload directTCPIPPayload
	if err := gossh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
		_ = newCh.Reject(gossh.ConnectionFailed, "invalid direct-tcpip payload")
		return
	}
	target := protocol.JoinHostPort(payload.HostToConnect, payload.PortToConnect)
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	go gossh.DiscardRequests(reqs)
	bridgeAgentTCP(a.registry, id, target, ch)
}

func sendExit(ch gossh.Channel, code int) {
	var payload [4]byte
	binary.BigEndian.PutUint32(payload[:], uint32(code))
	_, _ = ch.SendRequest("exit-status", false, payload[:])
}

func parsePtySize(payload []byte) (int, int) {
	var p struct {
		Term   string
		Width  uint32
		Height uint32
		PxW    uint32
		PxH    uint32
		Modes  string
	}
	if err := gossh.Unmarshal(payload, &p); err != nil {
		return 80, 24
	}
	return int(p.Width), int(p.Height)
}

func parseWindowChange(payload []byte) (int, int) {
	var p struct {
		Width  uint32
		Height uint32
		PxW    uint32
		PxH    uint32
	}
	if err := gossh.Unmarshal(payload, &p); err != nil {
		return 80, 24
	}
	return int(p.Width), int(p.Height)
}

func bridge(a io.ReadWriter, b io.ReadWriteCloser) {
	var wg sync.WaitGroup
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			if c, ok := a.(io.Closer); ok {
				_ = c.Close()
			}
			_ = b.Close()
		})
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		closeBoth()
	}()
	wg.Wait()
}

func closeWriter(v any) error {
	if c, ok := v.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return nil
}

func bridgeAgentTCP(reg *AgentRegistry, id, target string, client io.ReadWriteCloser) {
	defer client.Close()
	session, err := reg.Get(id)
	if err != nil {
		return
	}
	stream, err := session.Open()
	if err != nil {
		return
	}
	defer stream.Close()
	if err := protocol.WriteJSONLine(stream, protocol.StreamRequest{Type: protocol.StreamTCP, Target: target}); err != nil {
		return
	}
	reader := bufio.NewReader(stream)
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](reader)
	if err != nil || !resp.OK {
		return
	}
	bridge(client, struct {
		io.Reader
		io.Writer
		io.Closer
	}{Reader: reader, Writer: stream, Closer: stream})
}

type forwardManager struct {
	conn      *gossh.ServerConn
	mu        sync.Mutex
	listeners []net.Listener
}

func newForwardManager(conn *gossh.ServerConn) *forwardManager {
	return &forwardManager{conn: conn}
}

func (m *forwardManager) handleGlobalRequests(reqs <-chan *gossh.Request) {
	for req := range reqs {
		switch req.Type {
		case "tcpip-forward":
			m.handleTCPIPForward(req)
		case "cancel-tcpip-forward":
			req.Reply(true, nil)
		default:
			req.Reply(false, nil)
		}
	}
}

func (m *forwardManager) handleTCPIPForward(req *gossh.Request) {
	var payload tcpipForwardPayload
	if err := gossh.Unmarshal(req.Payload, &payload); err != nil {
		req.Reply(false, nil)
		return
	}
	bindHost := payload.Address
	if bindHost == "" || bindHost == "localhost" {
		bindHost = "127.0.0.1"
	}
	if bindHost != "127.0.0.1" {
		req.Reply(false, nil)
		return
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(bindHost, strconv.Itoa(int(payload.Port))))
	if err != nil {
		req.Reply(false, nil)
		return
	}
	actual := uint32(ln.Addr().(*net.TCPAddr).Port)
	if payload.Port == 0 {
		var resp [4]byte
		binary.BigEndian.PutUint32(resp[:], actual)
		req.Reply(true, resp[:])
	} else {
		req.Reply(true, nil)
	}
	m.mu.Lock()
	m.listeners = append(m.listeners, ln)
	m.mu.Unlock()
	connectedHost := payload.Address
	if connectedHost == "" {
		connectedHost = "localhost"
	}
	go m.acceptForwarded(ln, connectedHost, actual)
}

func (m *forwardManager) acceptForwarded(ln net.Listener, bindHost string, bindPort uint32) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("remote forward accept error: %v", err)
			}
			return
		}
		go m.openForwardedChannel(conn, bindHost, bindPort)
	}
}

func (m *forwardManager) openForwardedChannel(conn net.Conn, bindHost string, bindPort uint32) {
	defer conn.Close()
	host, portText, _ := net.SplitHostPort(conn.RemoteAddr().String())
	port, _ := strconv.ParseUint(portText, 10, 32)
	payload := gossh.Marshal(forwardedTCPIPPayload{
		ConnectedAddress:  bindHost,
		ConnectedPort:     bindPort,
		OriginatorAddress: host,
		OriginatorPort:    uint32(port),
	})
	ch, reqs, err := m.conn.OpenChannel("forwarded-tcpip", payload)
	if err != nil {
		return
	}
	go gossh.DiscardRequests(reqs)
	bridge(conn, ch)
}

func (m *forwardManager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ln := range m.listeners {
		_ = ln.Close()
	}
	m.listeners = nil
}
