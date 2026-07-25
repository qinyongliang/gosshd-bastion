package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

var errManualReviewNotFound = errors.New("manual review request not found")

type manualReviewHub struct {
	mu            sync.Mutex
	pending       map[string]*manualReviewRequest
	activePollers map[string]int
	autoAllow     map[string]manualReviewAutoAllow
	notify        chan struct{}
}

type manualReviewAutoAllow struct {
	Allow     bool
	Minutes   int
	ExpiresAt time.Time
	TargetIDs []string
}

type manualReviewRequest struct {
	ID                 string
	OrganizationID     string
	SessionID          string
	TargetID           string
	TargetName         string
	TargetAlias        string
	UserID             string
	UserEmail          string
	UserDisplayName    string
	Command            string
	Reason             string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	DefaultAllow       bool
	AutoAllowMinutes   int
	AutoAllowExpiresAt time.Time
	AutoAllowTargetIDs []string
	timer              *time.Timer
	decision           chan manualReviewDecision
}

type manualReviewDecision struct {
	Allow      bool
	ReviewerID string
	Reviewer   string
	DecidedAt  time.Time
}

type manualReviewSnapshot struct {
	ID                 string
	OrganizationID     string
	SessionID          string
	TargetID           string
	TargetName         string
	TargetAlias        string
	UserID             string
	UserEmail          string
	UserDisplayName    string
	Command            string
	Reason             string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	DefaultAllow       bool
	AutoAllowMinutes   int
	AutoAllowExpiresAt time.Time
	AutoAllowTargetIDs []string
}

func newManualReviewHub() *manualReviewHub {
	return &manualReviewHub{
		pending:       make(map[string]*manualReviewRequest),
		activePollers: make(map[string]int),
		autoAllow:     make(map[string]manualReviewAutoAllow),
		notify:        make(chan struct{}),
	}
}

func (h *manualReviewHub) Create(req manualReviewRequest, timeout time.Duration) (manualReviewSnapshot, <-chan manualReviewDecision) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().UTC()
	if timeout <= 0 {
		timeout = time.Duration(store.DefaultManualReviewTimeoutSeconds) * time.Second
	}
	req.ID = uuid.NewString()
	req.CreatedAt = now
	req.ExpiresAt = now.Add(timeout)
	if state, ok := h.activeAutoAllowLocked(manualReviewChoiceKey(req.OrganizationID, req.UserID), now); ok && containsString(state.TargetIDs, req.TargetID) {
		req.DefaultAllow = state.Allow
		req.AutoAllowMinutes = state.Minutes
		req.AutoAllowExpiresAt = state.ExpiresAt
		req.AutoAllowTargetIDs = append([]string(nil), state.TargetIDs...)
	}
	req.decision = make(chan manualReviewDecision, 1)
	h.pending[req.ID] = &req
	h.scheduleLocked(&req, now)
	h.signalLocked()
	return snapshotManualReview(&req), req.decision
}

func (h *manualReviewHub) List(ctx context.Context, organizationID, sessionID string, timeout time.Duration, knownIDs map[string]struct{}) ([]manualReviewSnapshot, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	registered := false
	pollerKey := manualReviewPollerKey(organizationID, sessionID)
	defer func() {
		if registered {
			h.unregisterPoller(pollerKey)
		}
	}()
	for {
		reviews, notify, didRegister := h.listOrNotify(organizationID, sessionID, knownIDs, timeout > 0 && !registered)
		if didRegister {
			registered = true
		}
		if len(reviews) > 0 || timeout <= 0 {
			return reviews, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return []manualReviewSnapshot{}, nil
		case <-notify:
		}
	}
}

func (h *manualReviewHub) HasActivePollers(organizationID, sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activePollers[manualReviewPollerKey(organizationID, sessionID)] > 0
}

func (h *manualReviewHub) Get(id string) (manualReviewSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireLocked(time.Now().UTC())
	req, ok := h.pending[id]
	if !ok {
		return manualReviewSnapshot{}, false
	}
	return snapshotManualReview(req), true
}

func (h *manualReviewHub) AutoAllowState(organizationID, userID string) (manualReviewAutoAllow, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.activeAutoAllowLocked(manualReviewChoiceKey(organizationID, userID), time.Now().UTC())
	state.TargetIDs = append([]string(nil), state.TargetIDs...)
	return state, ok
}

func (h *manualReviewHub) Decide(id string, decision manualReviewDecision) error {
	return h.DecideWithAutoAllow(id, decision, nil, nil)
}

func (h *manualReviewHub) DecideWithAutoAllow(id string, decision manualReviewDecision, autoAllowMinutes *int, targetIDs []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().UTC()
	h.expireLocked(now)
	req, ok := h.pending[id]
	if !ok {
		return errManualReviewNotFound
	}
	delete(h.pending, id)
	if req.timer != nil {
		req.timer.Stop()
	}
	decision.DecidedAt = now
	req.decision <- decision
	close(req.decision)
	if autoAllowMinutes != nil {
		h.updateAutoAllowLocked(req.OrganizationID, req.UserID, *autoAllowMinutes, decision.Allow, targetIDs, now)
	}
	h.signalLocked()
	return nil
}

func (h *manualReviewHub) Expire(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if req, ok := h.pending[id]; ok {
		delete(h.pending, id)
		if req.timer != nil {
			req.timer.Stop()
		}
		close(req.decision)
		h.signalLocked()
	}
}

func (h *manualReviewHub) listOrNotify(organizationID, sessionID string, knownIDs map[string]struct{}, registerPoller bool) ([]manualReviewSnapshot, <-chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireLocked(time.Now().UTC())
	reviews := h.listLocked(organizationID, sessionID, knownIDs)
	registered := false
	if registerPoller && len(reviews) == 0 {
		h.activePollers[manualReviewPollerKey(organizationID, sessionID)]++
		registered = true
	}
	return reviews, h.notify, registered
}

func (h *manualReviewHub) listLocked(organizationID, sessionID string, knownIDs map[string]struct{}) []manualReviewSnapshot {
	out := []manualReviewSnapshot{}
	for _, req := range h.pending {
		if req.OrganizationID == organizationID && req.SessionID == sessionID && !knownManualReviewID(req.ID, knownIDs) {
			out = append(out, snapshotManualReview(req))
		}
	}
	return out
}

func (h *manualReviewHub) expireLocked(now time.Time) {
	changed := false
	for key, state := range h.autoAllow {
		if !now.Before(state.ExpiresAt) {
			delete(h.autoAllow, key)
		}
	}
	for id, req := range h.pending {
		if !now.Before(req.ExpiresAt) {
			h.resolveExpiredLocked(id, req, now)
			changed = true
		}
	}
	if changed {
		h.signalLocked()
	}
}

func (h *manualReviewHub) activeAutoAllowLocked(key string, now time.Time) (manualReviewAutoAllow, bool) {
	state, ok := h.autoAllow[key]
	if ok && !now.Before(state.ExpiresAt) {
		delete(h.autoAllow, key)
		return manualReviewAutoAllow{}, false
	}
	return state, ok
}

func (h *manualReviewHub) SetAutoAllow(organizationID, userID string, minutes int, allow bool, targetIDs []string) manualReviewAutoAllow {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.updateAutoAllowLocked(organizationID, userID, minutes, allow, targetIDs, time.Now().UTC())
	state.TargetIDs = append([]string(nil), state.TargetIDs...)
	return state
}

func (h *manualReviewHub) updateAutoAllowLocked(organizationID, userID string, minutes int, allow bool, targetIDs []string, now time.Time) manualReviewAutoAllow {
	key := manualReviewChoiceKey(organizationID, userID)
	state, active := h.activeAutoAllowLocked(key, now)
	if minutes > 0 {
		if !active || state.Minutes != minutes {
			state.ExpiresAt = now.Add(time.Duration(minutes) * time.Minute)
		}
		state.Allow = allow
		state.Minutes = minutes
		state.TargetIDs = append([]string(nil), targetIDs...)
		h.autoAllow[key] = state
	} else {
		delete(h.autoAllow, key)
		state = manualReviewAutoAllow{}
	}
	return state
}

func (h *manualReviewHub) scheduleLocked(req *manualReviewRequest, now time.Time) {
	if req.timer != nil {
		req.timer.Stop()
	}
	delay := req.ExpiresAt.Sub(now)
	req.timer = time.AfterFunc(delay, func() {
		h.expire(req.ID)
	})
}

func (h *manualReviewHub) expire(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	req, ok := h.pending[id]
	if !ok {
		return
	}
	now := time.Now().UTC()
	if now.Before(req.ExpiresAt) {
		h.scheduleLocked(req, now)
		return
	}
	h.resolveExpiredLocked(id, req, now)
	h.signalLocked()
}

func (h *manualReviewHub) resolveExpiredLocked(id string, req *manualReviewRequest, now time.Time) {
	delete(h.pending, id)
	if req.timer != nil {
		req.timer.Stop()
	}
	reviewer := "automatic deadline"
	if req.AutoAllowMinutes > 0 {
		reviewer = "remembered choice"
	}
	req.decision <- manualReviewDecision{Allow: req.DefaultAllow, Reviewer: reviewer, DecidedAt: now}
	close(req.decision)
}

func (h *manualReviewHub) signalLocked() {
	close(h.notify)
	h.notify = make(chan struct{})
}

func (h *manualReviewHub) unregisterPoller(pollerKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activePollers[pollerKey] <= 1 {
		delete(h.activePollers, pollerKey)
		return
	}
	h.activePollers[pollerKey]--
}

func snapshotManualReview(req *manualReviewRequest) manualReviewSnapshot {
	return manualReviewSnapshot{
		ID:                 req.ID,
		OrganizationID:     req.OrganizationID,
		SessionID:          req.SessionID,
		TargetID:           req.TargetID,
		TargetName:         req.TargetName,
		TargetAlias:        req.TargetAlias,
		UserID:             req.UserID,
		UserEmail:          req.UserEmail,
		UserDisplayName:    req.UserDisplayName,
		Command:            req.Command,
		Reason:             req.Reason,
		CreatedAt:          req.CreatedAt,
		ExpiresAt:          req.ExpiresAt,
		DefaultAllow:       req.DefaultAllow,
		AutoAllowMinutes:   req.AutoAllowMinutes,
		AutoAllowExpiresAt: req.AutoAllowExpiresAt,
		AutoAllowTargetIDs: append([]string(nil), req.AutoAllowTargetIDs...),
	}
}

func manualReviewChoiceKey(organizationID, userID string) string {
	return organizationID + "\x00" + userID
}

func manualReviewPollerKey(organizationID, sessionID string) string {
	if sessionID == "" {
		return organizationID
	}
	return organizationID + "\x00" + sessionID
}

func knownManualReviewID(id string, knownIDs map[string]struct{}) bool {
	if len(knownIDs) == 0 {
		return false
	}
	_, ok := knownIDs[id]
	return ok
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
