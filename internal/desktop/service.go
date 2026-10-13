package desktop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID, ActorID, TargetID, SessionID, Method, Outcome string
	At                                                time.Time
}
type Adapter struct {
	Authorize func(context.Context, string, string, bool) error
	Dial      func(context.Context, string) (io.ReadWriteCloser, error)
	Audit     func(context.Context, Event) error
}
type SessionInfo struct {
	ID        string    `json:"session_id"`
	TargetID  string    `json:"target_id"`
	ExpiresAt time.Time `json:"expires_at"`
}
type session struct {
	SessionInfo
	actor    string
	rw       io.ReadWriteCloser
	reader   *bufio.Scanner
	gate     chan struct{}
	snapshot string
	window   *Window
	timer    *time.Timer
	once     sync.Once
}

func (s *session) close() {
	s.once.Do(func() {
		if s.timer != nil {
			s.timer.Stop()
		}
		s.rw.Close()
	})
}

type Service struct {
	context     context.Context
	cancel      context.CancelFunc
	workers     sync.WaitGroup
	adapter     Adapter
	mu          sync.Mutex
	sessions    map[string]*session
	pending     map[string]int
	closed      bool
	Timeout     time.Duration
	IdleTimeout time.Duration
	Lifetime    time.Duration
}

func NewService(adapter Adapter) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{context: ctx, cancel: cancel, adapter: adapter, sessions: map[string]*session{}, pending: map[string]int{}, Timeout: 30 * time.Second, IdleTimeout: 5 * time.Minute, Lifetime: 30 * time.Minute}
}
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	all := s.sessions
	s.sessions = map[string]*session{}
	s.mu.Unlock()
	for _, item := range all {
		item.close()
	}
	s.workers.Wait()
}
func (s *Service) Remove(id string) {
	s.mu.Lock()
	item := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if item != nil {
		item.close()
	}
}
func (s *Service) audit(ctx context.Context, e Event) error {
	if s.adapter.Audit == nil {
		return nil
	}
	return s.adapter.Audit(ctx, e)
}
func (s *Service) Open(ctx context.Context, actor, target string) (SessionInfo, Response, error) {
	if actor == "" || target == "" || len(actor) > 128 || len(target) > 128 {
		return SessionInfo{}, Response{}, errors.New("valid actor and target identities are required")
	}
	if s.adapter.Authorize == nil || s.adapter.Dial == nil {
		return SessionInfo{}, Response{}, errors.New("desktop adapter is not configured")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return SessionInfo{}, Response{}, errors.New("desktop service is closed")
	}
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	ctx, operationCancel := context.WithCancel(ctx)
	stopService := context.AfterFunc(s.context, operationCancel)
	defer stopService()
	defer operationCancel()
	event := Event{ID: uuid.NewString(), ActorID: actor, TargetID: target, Method: "session_open", At: time.Now().UTC(), Outcome: "started"}
	if err := s.adapter.Authorize(ctx, actor, target, false); err != nil {
		event.Outcome = "denied"
		_ = s.audit(ctx, event)
		return SessionInfo{}, Response{}, err
	}
	s.mu.Lock()
	count, total := s.pending[actor], len(s.sessions)
	for _, n := range s.pending {
		total += n
	}
	for _, existing := range s.sessions {
		if existing.actor == actor {
			count++
		}
	}
	if s.closed || count >= 4 || total >= 64 {
		s.mu.Unlock()
		return SessionInfo{}, Response{}, errors.New("desktop session limit reached or service closed")
	}
	s.pending[actor]++
	s.mu.Unlock()
	reserved := true
	defer func() {
		if reserved {
			s.mu.Lock()
			s.pending[actor]--
			if s.pending[actor] == 0 {
				delete(s.pending, actor)
			}
			s.mu.Unlock()
		}
	}()
	if err := s.audit(ctx, event); err != nil {
		return SessionInfo{}, Response{}, fmt.Errorf("desktop audit unavailable: %w", err)
	}
	auditFinished := false
	defer func() {
		if !auditFinished {
			event.Outcome = "error"
			auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.audit(auditCtx, event)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	rw, err := s.adapter.Dial(ctx, target)
	if err != nil {
		return SessionInfo{}, Response{}, err
	}
	scanner := bufio.NewScanner(rw)
	scanner.Buffer(make([]byte, 65536), MaxMessage)
	stop := context.AfterFunc(ctx, func() { rw.Close() })
	var init Response
	if !scanner.Scan() {
		stop()
		rw.Close()
		return SessionInfo{}, Response{}, errors.New("desktop Agent did not initialize")
	}
	err = json.Unmarshal(scanner.Bytes(), &init)
	if err != nil || !init.OK || init.ID != "init" {
		stop()
		rw.Close()
		if init.Error != "" {
			return SessionInfo{}, Response{}, errors.New(init.Error)
		}
		return SessionInfo{}, Response{}, errors.New("invalid desktop Agent handshake")
	}
	if !stop() || ctx.Err() != nil {
		rw.Close()
		if ctx.Err() != nil {
			return SessionInfo{}, Response{}, ctx.Err()
		}
		return SessionInfo{}, Response{}, errors.New("desktop session initialization cancelled")
	}
	item := &session{SessionInfo: SessionInfo{ID: uuid.NewString(), TargetID: target, ExpiresAt: time.Now().Add(s.Lifetime)}, actor: actor, rw: rw, reader: scanner, gate: make(chan struct{}, 1)}
	item.gate <- struct{}{}
	s.mu.Lock()
	s.pending[actor]--
	if s.pending[actor] == 0 {
		delete(s.pending, actor)
	}
	reserved = false
	if s.closed {
		s.mu.Unlock()
		rw.Close()
		return SessionInfo{}, Response{}, errors.New("desktop service is closed")
	}
	item.timer = time.AfterFunc(s.IdleTimeout, func() { s.Remove(item.ID) })
	s.sessions[item.ID] = item
	s.mu.Unlock()
	res, err := s.Call(ctx, actor, item.ID, Request{Method: "status"})
	if err != nil {
		s.Remove(item.ID)
		return SessionInfo{}, Response{}, err
	}
	event.SessionID = item.ID
	event.Outcome = "ok"
	auditFinished = true
	if err = s.audit(ctx, event); err != nil {
		s.Remove(item.ID)
		return SessionInfo{}, Response{}, errors.New("desktop open audit completion failed")
	}
	return item.SessionInfo, res, nil
}

func (s *Service) CloseSession(ctx context.Context, actor, id string) error {
	s.mu.Lock()
	item := s.sessions[id]
	if s.closed || item == nil || item.actor != actor {
		s.mu.Unlock()
		return errors.New("desktop session not found")
	}
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	// Cleanup cannot depend on audit/database availability.
	s.Remove(id)
	event := Event{ID: uuid.NewString(), ActorID: actor, TargetID: item.TargetID, SessionID: id, Method: "session_close", Outcome: "ok", At: time.Now().UTC()}
	if err := s.audit(ctx, event); err != nil {
		return errors.New("desktop session closed, but audit completion failed")
	}
	return nil
}

func (s *Service) Call(ctx context.Context, actor, id string, req Request) (Response, error) {
	if err := Validate(req); err != nil {
		return Response{}, err
	}

	s.mu.Lock()
	item := s.sessions[id]
	if s.closed || item == nil || item.actor != actor {
		s.mu.Unlock()
		return Response{}, errors.New("desktop session not found")
	}
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	ctx, operationCancel := context.WithCancel(ctx)
	stopService := context.AfterFunc(s.context, operationCancel)
	defer stopService()
	defer operationCancel()
	select {
	case <-item.gate:
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
	defer func() { item.gate <- struct{}{} }()
	if time.Now().After(item.ExpiresAt) {
		s.Remove(id)
		return Response{}, errors.New("desktop session expired")
	}
	event := Event{ID: uuid.NewString(), ActorID: actor, TargetID: item.TargetID, SessionID: id, Method: req.Method, At: time.Now().UTC(), Outcome: "started"}
	if err := s.adapter.Authorize(ctx, actor, item.TargetID, IsControl(req.Method)); err != nil {
		event.Outcome = "denied"
		_ = s.audit(ctx, event)
		s.Remove(id)
		return Response{}, err
	}
	if NeedsSnapshot(req.Method) {
		if item.snapshot == "" || req.Params.SnapshotID != item.snapshot || item.window == nil || req.Params.Window.ID != item.window.ID || req.Params.Window.App != item.window.App {
			return Response{}, errors.New("stale snapshot; observe the same window again")
		}
	}
	if err := s.audit(ctx, event); err != nil {
		return Response{}, fmt.Errorf("desktop audit unavailable: %w", err)
	}
	finishedAudit := false
	defer func() {
		if !finishedAudit {
			event.Outcome = "unknown"
			auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.audit(auditCtx, event)
		}
	}()
	item.timer.Reset(min(s.IdleTimeout, time.Until(item.ExpiresAt)))
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { s.Remove(id) })
	defer stop()
	req.ID = event.ID
	// Any action consumes the observation, including actions which fail.
	if IsControl(req.Method) || req.Method == "get_window_state" || req.Method == "screenshot" {
		item.snapshot = ""
		item.window = nil
	}
	b, _ := json.Marshal(req)
	if _, err := item.rw.Write(append(b, '\n')); err != nil {
		s.Remove(id)
		return Response{}, errors.New("desktop transport failed; re-open the session")
	}
	if !item.reader.Scan() {
		s.Remove(id)
		return Response{}, errors.New("desktop response interrupted; action outcome may be unknown, do not retry without observation")
	}
	var res Response
	if err := json.Unmarshal(item.reader.Bytes(), &res); err != nil || res.ID != req.ID {
		s.Remove(id)
		return Response{}, errors.New("invalid desktop response")
	}
	if ctx.Err() != nil {
		s.Remove(id)
		return Response{}, ctx.Err()
	}
	if res.OK && (req.Method == "get_window_state" || req.Method == "screenshot") {
		var state State
		if err := json.Unmarshal(res.Result, &state); err != nil || state.SnapshotID == "" || (req.Method == "get_window_state" && (req.Params.Window.ID != state.Window.ID || req.Params.Window.App != state.Window.App)) {
			s.Remove(id)
			return Response{}, errors.New("invalid desktop observation")
		}

		if state.Image != nil {
			dimensions, imageError := png.DecodeConfig(bytes.NewReader(state.Image.Data))
			if len(state.Image.Data) > MaxImage || state.Image.MIMEType != "image/png" || imageError != nil || dimensions.Width != state.Image.Width || dimensions.Height != state.Image.Height || dimensions.Width > 4096 || dimensions.Height > 4096 {
				s.Remove(id)
				return Response{}, errors.New("invalid desktop screenshot")
			}
		}
		if err := ValidateElements(state.Elements); err != nil {
			s.Remove(id)
			return Response{}, err
		}
		if req.Method == "get_window_state" {
			item.snapshot = state.SnapshotID
			w := state.Window
			item.window = &w
		}

	}
	event.Outcome = "ok"
	if !res.OK {
		event.Outcome = "error"
	}
	finishedAudit = true
	if err := s.audit(context.WithoutCancel(ctx), event); err != nil {
		s.Remove(id)
		return Response{}, errors.New("desktop audit completion failed; action may have executed, do not retry")
	}
	if !res.OK {
		return res, errors.New(res.Error)
	}
	return res, nil
}

// Revoke closes all matching actor/target sessions immediately.
func (s *Service) Revoke(actor, target string) {
	s.mu.Lock()
	var ids []string
	for id, item := range s.sessions {
		if item.actor == actor && item.TargetID == target {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Remove(id)
	}
}
