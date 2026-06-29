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

const (
	terminalCommandIdleFallbackTimeout = 1200 * time.Millisecond
	terminalCommandIdleFallbackGrace   = 200 * time.Millisecond
)

var (
	errTerminalSessionBusy      = errors.New("terminal session is busy")
	errTerminalSessionClosed    = errors.New("terminal session is closed")
	errTerminalSessionInputWait = errors.New("session input not ready")
)

type terminalSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

type terminalSessionRouteSnapshot struct {
	ID             string
	UserID         string
	TargetID       string
	TargetAlias    string
	StartedAt      time.Time
	LastHeartbeat  time.Time
	Closed         bool
	InputReady     bool
	ClientCount    int
	ShellBusy      bool
	HeartbeatStale bool
	Reason         string
}

type terminalSessionRouteLookup struct {
	Session      *terminalSession
	Snapshots    []terminalSessionRouteSnapshot
	SessionCount int
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

	mu                  sync.Mutex
	input               io.Writer
	resize              func(cols, rows int)
	closeInput          func()
	recorder            *terminalRecorder
	clients             map[*terminalWSWriter]bool
	output              strings.Builder
	screen              *terminalScreenBuffer
	lastHeartbeat       time.Time
	closed              bool
	shellBusy           bool
	commandIdleFallback bool
	commandMu           sync.Mutex
	commandWaiters      []*terminalCommandWaiter
	oscBuffer           string
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

func (m *terminalSessionManager) earliestOnlineForUserTarget(userID, targetID string) *terminalSession {
	return m.earliestOnlineForUserTargetWithDiagnostics(userID, targetID).Session
}

func (m *terminalSessionManager) earliestOnlineForUserTargetWithDiagnostics(userID, targetID string) terminalSessionRouteLookup {
	m.mu.Lock()
	defer m.mu.Unlock()
	var selected *terminalSession
	var selectedStartedAt time.Time
	var snapshots []terminalSessionRouteSnapshot
	for _, session := range m.sessions {
		session.mu.Lock()
		staleHeartbeat := time.Since(session.lastHeartbeat) > terminalSessionHeartbeatTimeout
		snapshot := terminalSessionRouteSnapshot{
			ID:             session.id,
			UserID:         session.userID,
			TargetID:       session.target.ID,
			TargetAlias:    session.target.Alias,
			StartedAt:      session.startedAt,
			LastHeartbeat:  session.lastHeartbeat,
			Closed:         session.closed,
			InputReady:     session.input != nil,
			ClientCount:    len(session.clients),
			ShellBusy:      session.shellBusy,
			HeartbeatStale: staleHeartbeat,
		}
		startedAt := session.startedAt
		session.mu.Unlock()
		switch {
		case snapshot.UserID != userID:
			snapshot.Reason = "user-mismatch"
		case snapshot.TargetID != targetID:
			snapshot.Reason = "target-mismatch"
		case snapshot.Closed:
			snapshot.Reason = "closed"
		case !snapshot.InputReady:
			snapshot.Reason = "input-wait"
		case snapshot.ClientCount == 0:
			snapshot.Reason = "no-client"
		case snapshot.HeartbeatStale:
			snapshot.Reason = "stale-heartbeat"
		default:
			snapshot.Reason = "candidate"
		}
		snapshots = append(snapshots, snapshot)
		matches := snapshot.Reason == "candidate"
		if !matches {
			continue
		}
		if selected == nil || startedAt.Before(selectedStartedAt) {
			selected = session
			selectedStartedAt = startedAt
		}
	}
	return terminalSessionRouteLookup{Session: selected, Snapshots: snapshots, SessionCount: len(snapshots)}
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
		return errTerminalSessionInputWait
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

func (s *terminalSession) enableCommandIdleFallback() {
	s.mu.Lock()
	s.commandIdleFallback = true
	s.mu.Unlock()
}

type terminalCommandResult struct {
	Output   string
	ExitCode int
}

type terminalCommandAttempt struct {
	Result   terminalCommandResult
	Acquired bool
	Sent     bool
	Err      error
}

type terminalCommandWaiter struct {
	id     string
	output chan string
	done   chan int
}

type terminalIntegrationEvent struct {
	Kind     string
	ID       string
	ExitCode int
}

func bashShellIntegrationCommand() string {
	return `if command -v bash >/dev/null 2>&1; then
  __gosshd_rc="${TMPDIR:-/tmp}/gosshd-bashrc.$$"
  cat > "$__gosshd_rc" <<'__GOSSHD_BASHRC__'
if [ -r "$HOME/.bashrc" ]; then
  . "$HOME/.bashrc"
fi
if [ -n "$GOSSHD_BASHRC" ]; then
  rm -f "$GOSSHD_BASHRC"
  unset GOSSHD_BASHRC
fi
__gosshd_command_running=0
__gosshd_original_prompt_command=${PROMPT_COMMAND-}
__gosshd_preexec() {
  if [ "$__gosshd_command_running" = 0 ]; then
    __gosshd_command_running=1
    printf '\033]633;C\007'
  fi
}
__gosshd_precmd() {
  local __gosshd_rc=$?
  trap - DEBUG
  if [ "$__gosshd_command_running" = 1 ]; then
    printf '\033]633;D;%s\007' "$__gosshd_rc"
    __gosshd_command_running=0
  fi
  printf '\033]633;A\007'
  if [ -n "$__gosshd_original_prompt_command" ]; then
    eval "$__gosshd_original_prompt_command"
  fi
  trap '__gosshd_preexec' DEBUG
  return "$__gosshd_rc"
}
PROMPT_COMMAND='__gosshd_precmd'
trap '__gosshd_preexec' DEBUG
__GOSSHD_BASHRC__
  GOSSHD_BASHRC="$__gosshd_rc" exec bash --rcfile "$__gosshd_rc" -i
fi
exec "${SHELL:-/bin/sh}" -i
`
}

func (s *terminalSession) sendCommand(ctx context.Context, command string) (terminalCommandResult, error) {
	attempt := s.sendCommandAttempt(ctx, command, false)
	return attempt.Result, attempt.Err
}

func (s *terminalSession) trySendCommand(ctx context.Context, command string) terminalCommandAttempt {
	return s.sendCommandAttempt(ctx, command, true)
}

func (s *terminalSession) sendCommandAttempt(ctx context.Context, command string, nonBlocking bool) terminalCommandAttempt {
	command, err := normalizeTerminalCommand(command)
	if err != nil {
		return terminalCommandAttempt{Err: err}
	}
	unlock, acquired := s.lockCommand(nonBlocking)
	if !acquired {
		return terminalCommandAttempt{}
	}
	defer unlock()
	result, sent, err := s.trySendCommandLocked(ctx, command)
	return terminalCommandAttempt{Result: result, Acquired: true, Sent: sent, Err: err}
}

func normalizeTerminalCommand(command string) (string, error) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return "", errors.New("command is required")
	}
	return command, nil
}

func (s *terminalSession) lockCommand(nonBlocking bool) (func(), bool) {
	if nonBlocking {
		if !s.commandMu.TryLock() {
			return nil, false
		}
	} else {
		s.commandMu.Lock()
	}
	return s.commandMu.Unlock, true
}

func (s *terminalSession) sendCommandLocked(ctx context.Context, command string) (terminalCommandResult, error) {
	result, _, err := s.trySendCommandLocked(ctx, command)
	return result, err
}

func (s *terminalSession) trySendCommandLocked(ctx context.Context, command string) (terminalCommandResult, bool, error) {
	waiter := &terminalCommandWaiter{
		output: make(chan string, 64),
		done:   make(chan int, 1),
	}
	if err := s.commandReadinessError(); err != nil {
		return terminalCommandResult{}, false, err
	}
	s.mu.Lock()
	s.commandWaiters = append(s.commandWaiters, waiter)
	s.mu.Unlock()
	defer s.removeCommandWaiter(waiter)
	if err := s.writeInput(command + "\r"); err != nil {
		return terminalCommandResult{}, false, err
	}
	result, err := collectCommandOutput(ctx, s.ctx, waiter, s.commandIdleFallback)
	return result, true, err
}

func (s *terminalSession) commandReadinessError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errTerminalSessionClosed
	}
	if s.input == nil {
		return errTerminalSessionInputWait
	}
	if s.shellBusy {
		return errTerminalSessionBusy
	}
	return nil
}

func (s *terminalSession) removeCommandWaiter(waiter *terminalCommandWaiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.commandWaiters {
		if item == waiter {
			s.commandWaiters = append(s.commandWaiters[:i], s.commandWaiters[i+1:]...)
			close(waiter.output)
			return
		}
	}
}

func collectCommandOutput(ctx, sessionCtx context.Context, waiter *terminalCommandWaiter, idleFallback bool) (terminalCommandResult, error) {
	var out strings.Builder
	var idleTimer *time.Timer
	var idleC <-chan time.Time
	stopIdle := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
	}
	resetIdle := func(d time.Duration) {
		if !idleFallback {
			return
		}
		if idleTimer == nil {
			idleTimer = time.NewTimer(d)
			idleC = idleTimer.C
			return
		}
		stopIdle()
		idleTimer.Reset(d)
	}
	defer stopIdle()
	resetIdle(terminalCommandIdleFallbackTimeout)
	for {
		select {
		case chunk, ok := <-waiter.output:
			if !ok {
				return terminalCommandResult{Output: out.String(), ExitCode: 255}, errors.New("session command output closed before completion")
			}
			out.WriteString(chunk)
			resetIdle(terminalCommandIdleFallbackGrace)
		case code := <-waiter.done:
			for {
				select {
				case chunk, ok := <-waiter.output:
					if ok {
						out.WriteString(chunk)
						continue
					}
				default:
				}
				break
			}
			return terminalCommandResult{Output: out.String(), ExitCode: code}, nil
		case <-idleC:
			return terminalCommandResult{Output: out.String(), ExitCode: 0}, nil
		case <-ctx.Done():
			return terminalCommandResult{Output: out.String(), ExitCode: 255}, ctx.Err()
		case <-sessionCtx.Done():
			return terminalCommandResult{Output: out.String(), ExitCode: 255}, errors.New("session closed before command completed")
		}
	}
}

func (s *terminalSession) writeOutput(typ string, data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	cleanText, events := s.consumeTerminalIntegration(string(data))
	if cleanText == "" && len(events) == 0 {
		s.mu.Unlock()
		return
	}
	cleanData := []byte(cleanText)
	if cleanText != "" && s.recorder != nil {
		s.recorder.WriteOutput(cleanData)
	}
	if cleanText != "" && s.output.Len() < 256*1024 {
		storeText := cleanText
		remaining := 256*1024 - s.output.Len()
		if len(storeText) > remaining {
			storeText = storeText[:remaining]
		}
		s.output.WriteString(storeText)
	}
	s.screen.write(cleanData)
	for _, event := range events {
		switch event.Kind {
		case "C":
			s.shellBusy = true
		case "A", "D":
			s.shellBusy = false
		}
	}
	for _, waiter := range s.commandWaiters {
		if cleanText != "" {
			select {
			case waiter.output <- cleanText:
			default:
			}
		}
		for _, event := range events {
			if event.Kind == "D" && (waiter.id == "" || event.ID == "" || event.ID == waiter.id) {
				select {
				case waiter.done <- event.ExitCode:
				default:
				}
			}
		}
	}
	if cleanText != "" {
		s.broadcastLocked(terminalWSMessage{Type: typ, Data: cleanText})
	}
	s.mu.Unlock()
}

func (s *terminalSession) consumeTerminalIntegration(text string) (string, []terminalIntegrationEvent) {
	input := s.oscBuffer + text
	s.oscBuffer = ""
	var clean strings.Builder
	var events []terminalIntegrationEvent
	for len(input) > 0 {
		start := strings.Index(input, "\x1b]633;")
		if start < 0 {
			clean.WriteString(input)
			break
		}
		clean.WriteString(input[:start])
		remaining := input[start:]
		end, termLen := terminalOSCSequenceEnd(remaining)
		if end < 0 {
			s.oscBuffer = remaining
			if len(s.oscBuffer) > 4096 {
				clean.WriteString(s.oscBuffer)
				s.oscBuffer = ""
			}
			break
		}
		payload := remaining[len("\x1b]633;"):end]
		if event, ok := parseTerminalIntegrationEvent(payload); ok {
			events = append(events, event)
		}
		input = remaining[end+termLen:]
	}
	return clean.String(), events
}

func terminalOSCSequenceEnd(input string) (int, int) {
	bel := strings.IndexByte(input, '\a')
	st := strings.Index(input, "\x1b\\")
	switch {
	case bel < 0 && st < 0:
		return -1, 0
	case bel >= 0 && (st < 0 || bel < st):
		return bel, 1
	default:
		return st, 2
	}
}

func parseTerminalIntegrationEvent(payload string) (terminalIntegrationEvent, bool) {
	parts := strings.Split(payload, ";")
	if len(parts) == 0 || parts[0] == "" {
		return terminalIntegrationEvent{}, false
	}
	event := terminalIntegrationEvent{Kind: parts[0]}
	switch event.Kind {
	case "A", "C", "E":
		if len(parts) >= 2 {
			event.ID = parts[1]
		}
		return event, true
	case "D":
		if len(parts) >= 3 {
			event.ID = parts[1]
			code, err := strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil {
				code = 255
			}
			event.ExitCode = code
			return event, true
		}
		if len(parts) >= 2 {
			code, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				code = 255
			}
			event.ExitCode = code
			return event, true
		}
	case "P":
		return event, true
	}
	return terminalIntegrationEvent{}, false
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
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	if cols > 0 && rows > 0 {
		_ = session.WindowChange(rows, cols)
	}
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
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	if cols > 0 && rows > 0 {
		var data [8]byte
		binary.BigEndian.PutUint32(data[0:4], uint32(cols))
		binary.BigEndian.PutUint32(data[4:8], uint32(rows))
		_ = protocol.WriteFrame(stream, protocol.Frame{Type: protocol.FrameResize, Data: data[:]})
	}
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
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\r':
			if i+1 < len(runes) && runes[i+1] == '\n' {
				continue
			}
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
