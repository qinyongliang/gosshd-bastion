package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
	gossh "golang.org/x/crypto/ssh"
)

const terminalSessionHeartbeatTimeout = 30 * time.Second

type terminalSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

type terminalSession struct {
	id         string
	userID     string
	target     store.SSHTarget
	sourceIP   string
	cols       int
	rows       int
	startedAt  time.Time
	auditLogID string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan int

	mu             sync.Mutex
	input          io.Writer
	resize         func(cols, rows int)
	closeInput     func()
	recorder       *terminalRecorder
	clients        map[*terminalWSWriter]bool
	output         strings.Builder
	screen         *terminalScreenBuffer
	lastHeartbeat  time.Time
	closed         bool
	commandWaiters []chan string
}

type terminalScreenBuffer struct {
	rows  int
	lines []string
	cur   string
}

func newTerminalSessionManager() *terminalSessionManager {
	return &terminalSessionManager{sessions: map[string]*terminalSession{}}
}

func (m *terminalSessionManager) create(sessionID, userID string, target store.SSHTarget, sourceIP string, cols, rows int, recorder *terminalRecorder) *terminalSession {
	ctx, cancel := context.WithCancel(context.Background())
	if strings.TrimSpace(sessionID) == "" {
		sessionID = newAuditSessionID()
	}
	s := &terminalSession{
		id:            sessionID,
		userID:        userID,
		target:        target,
		sourceIP:      sourceIP,
		cols:          cols,
		rows:          rows,
		startedAt:     time.Now().UTC(),
		ctx:           ctx,
		cancel:        cancel,
		done:          make(chan int, 1),
		recorder:      recorder,
		clients:       map[*terminalWSWriter]bool{},
		screen:        newTerminalScreenBuffer(rows),
		lastHeartbeat: time.Now().UTC(),
	}
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	go m.watchHeartbeat(s)
	return s
}

func (m *terminalSessionManager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *terminalSessionManager) listForUser(userID string) []terminalSessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []terminalSessionInfo
	for _, session := range m.sessions {
		session.mu.Lock()
		if session.userID == userID && !session.closed {
			out = append(out, terminalSessionInfo{
				ID:            session.id,
				TargetID:      session.target.ID,
				TargetName:    session.target.Name,
				TargetAlias:   session.target.Alias,
				Endpoint:      targetEndpointForStore(session.target),
				StartedAt:     session.startedAt,
				LastHeartbeat: session.lastHeartbeat,
			})
		}
		session.mu.Unlock()
	}
	return out
}

func (m *terminalSessionManager) getForUser(userID, sessionID string) (*terminalSession, error) {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(sessionID)]
	m.mu.Unlock()
	if session == nil {
		return nil, errors.New("session not found")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.userID != userID || session.closed {
		return nil, errors.New("session not found")
	}
	return session, nil
}

func (m *terminalSessionManager) watchHeartbeat(session *terminalSession) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			session.mu.Lock()
			expired := !session.closed && time.Since(session.lastHeartbeat) > terminalSessionHeartbeatTimeout
			session.mu.Unlock()
			if expired {
				session.close("heartbeat timeout")
				return
			}
		case <-session.ctx.Done():
			return
		}
	}
}

type terminalSessionInfo struct {
	ID            string
	TargetID      string
	TargetName    string
	TargetAlias   string
	Endpoint      string
	StartedAt     time.Time
	LastHeartbeat time.Time
}

func (s *terminalSession) attach(writer *terminalWSWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[writer] = true
	s.lastHeartbeat = time.Now().UTC()
	if s.output.Len() > 0 {
		_ = writer.write(terminalWSMessage{Type: "output", Data: s.output.String()})
	}
}

func (s *terminalSession) detach(writer *terminalWSWriter) {
	s.mu.Lock()
	delete(s.clients, writer)
	s.mu.Unlock()
}

func (s *terminalSession) heartbeat() {
	s.mu.Lock()
	s.lastHeartbeat = time.Now().UTC()
	s.mu.Unlock()
}

func (s *terminalSession) close(reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if reason != "" {
		s.broadcastLocked(terminalWSMessage{Type: "output", Data: "\r\n\x1b[2;37mSession closed: " + reason + "\x1b[0m\r\n"})
	}
	if s.closeInput != nil {
		s.closeInput()
	}
	s.mu.Unlock()
	s.cancel()
}

func (s *terminalSession) writeInput(data string) error {
	s.mu.Lock()
	writer := s.input
	s.mu.Unlock()
	if writer == nil {
		return errors.New("session input not ready")
	}
	_, err := io.WriteString(writer, data)
	return err
}

func (s *terminalSession) resizeTo(cols, rows int) {
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	resize := s.resize
	s.screen.setRows(rows)
	s.mu.Unlock()
	if resize != nil {
		resize(cols, rows)
	}
}

func (s *terminalSession) interrupt() error {
	return s.writeInput("\x03")
}

func (s *terminalSession) sendCommand(command string) (string, error) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return "", errors.New("command is required")
	}
	ch := make(chan string, 64)
	s.mu.Lock()
	s.commandWaiters = append(s.commandWaiters, ch)
	s.mu.Unlock()
	defer s.removeCommandWaiter(ch)
	if err := s.writeInput(command + "\n"); err != nil {
		return "", err
	}
	return collectCommandOutput(ch, 5*time.Second, 550*time.Millisecond), nil
}

func (s *terminalSession) removeCommandWaiter(ch chan string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, waiter := range s.commandWaiters {
		if waiter == ch {
			s.commandWaiters = append(s.commandWaiters[:i], s.commandWaiters[i+1:]...)
			close(ch)
			return
		}
	}
}

func collectCommandOutput(ch <-chan string, maxWait, quietWait time.Duration) string {
	var out strings.Builder
	timer := time.NewTimer(maxWait)
	quiet := time.NewTimer(quietWait)
	defer timer.Stop()
	defer quiet.Stop()
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return out.String()
			}
			out.WriteString(chunk)
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(quietWait)
		case <-quiet.C:
			if out.Len() > 0 {
				return out.String()
			}
			quiet.Reset(quietWait)
		case <-timer.C:
			return out.String()
		}
	}
}

func (s *terminalSession) writeOutput(typ string, data []byte) {
	if len(data) == 0 {
		return
	}
	if s.recorder != nil {
		s.recorder.WriteOutput(data)
	}
	text := string(data)
	s.mu.Lock()
	if s.output.Len() < 256*1024 {
		remaining := 256*1024 - s.output.Len()
		if len(text) > remaining {
			text = text[:remaining]
		}
		s.output.WriteString(text)
	}
	s.screen.write(data)
	for _, waiter := range s.commandWaiters {
		select {
		case waiter <- string(data):
		default:
		}
	}
	s.broadcastLocked(terminalWSMessage{Type: typ, Data: string(data)})
	s.mu.Unlock()
}

func (s *terminalSession) broadcastLocked(msg terminalWSMessage) {
	for writer := range s.clients {
		_ = writer.write(msg)
	}
}

func (s *terminalSession) currentScreen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen.String()
}

func (s *terminalSession) setDirectInput(stdin io.WriteCloser, session *gossh.Session) {
	s.mu.Lock()
	s.input = stdin
	s.closeInput = func() {
		_ = closeWriter(stdin)
		_ = session.Close()
	}
	s.resize = func(cols, rows int) {
		_ = session.WindowChange(rows, cols)
	}
	s.mu.Unlock()
}

func (s *terminalSession) setAgentInput(stream io.WriteCloser) {
	s.mu.Lock()
	s.input = agentSessionInput{stream: stream}
	s.closeInput = func() { _ = stream.Close() }
	s.resize = func(cols, rows int) {
		var data [8]byte
		binary.BigEndian.PutUint32(data[0:4], uint32(cols))
		binary.BigEndian.PutUint32(data[4:8], uint32(rows))
		_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameResize, Data: data[:]})
	}
	s.mu.Unlock()
}

type agentSessionInput struct {
	stream io.Writer
}

func (w agentSessionInput) Write(data []byte) (int, error) {
	if err := protocol.WriteFrame(w.stream, protocol.Frame{Type: protocol.FrameStdin, Data: append([]byte(nil), data...)}); err != nil {
		return 0, err
	}
	return len(data), nil
}

func newTerminalScreenBuffer(rows int) *terminalScreenBuffer {
	if rows <= 0 {
		rows = 32
	}
	return &terminalScreenBuffer{rows: rows}
}

func (b *terminalScreenBuffer) setRows(rows int) {
	if rows <= 0 {
		return
	}
	b.rows = rows
	if len(b.lines) > b.rows {
		b.lines = b.lines[len(b.lines)-b.rows:]
	}
}

func (b *terminalScreenBuffer) write(data []byte) {
	text := stripANSI(string(bytes.ToValidUTF8(data, []byte{})))
	for _, r := range text {
		switch r {
		case '\r':
			b.cur = ""
		case '\n':
			b.pushLine(b.cur)
			b.cur = ""
		case '\b':
			if len(b.cur) > 0 {
				_, size := utf8.DecodeLastRuneInString(b.cur)
				b.cur = b.cur[:len(b.cur)-size]
			}
		default:
			if r >= 32 || r == '\t' {
				b.cur += string(r)
			}
		}
	}
}

func (b *terminalScreenBuffer) pushLine(line string) {
	b.lines = append(b.lines, line)
	if len(b.lines) > b.rows {
		b.lines = b.lines[len(b.lines)-b.rows:]
	}
}

func (b *terminalScreenBuffer) String() string {
	lines := append([]string(nil), b.lines...)
	if b.cur != "" {
		lines = append(lines, b.cur)
	}
	if len(lines) > b.rows {
		lines = lines[len(lines)-b.rows:]
	}
	return strings.Join(lines, "\n")
}

func stripANSI(input string) string {
	var out strings.Builder
	for i := 0; i < len(input); i++ {
		if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '[' {
			i += 2
			for i < len(input) {
				c := input[i]
				if c >= 0x40 && c <= 0x7e {
					break
				}
				i++
			}
			continue
		}
		out.WriteByte(input[i])
	}
	return out.String()
}

func targetEndpointForStore(target store.SSHTarget) string {
	if target.Host == "" {
		return target.RemoteUsername + "@agent:" + target.AgentID
	}
	port := target.Port
	if port <= 0 {
		port = 22
	}
	return target.RemoteUsername + "@" + target.Host + ":" + strconv.Itoa(port)
}
