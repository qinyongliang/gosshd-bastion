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
	Minutes   int
	ExpiresAt time.Time
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
	NormalExpiresAt    time.Time
	AutoAllowMinutes   int
	AutoAllowExpiresAt time.Time
	AutoAllow          bool
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
	NormalExpiresAt    time.Time
	AutoAllowMinutes   int
	AutoAllowExpiresAt time.Time
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
	req.NormalExpiresAt = now.Add(timeout)
	req.ExpiresAt = req.NormalExpiresAt
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

func (h *manualReviewHub) AutoAllowState(organizationID, sessionID string) (manualReviewAutoAllow, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeAutoAllowLocked(manualReviewPollerKey(organizationID, sessionID), time.Now().UTC())
}

func (h *manualReviewHub) Decide(id string, decision manualReviewDecision) error {
	return h.DecideWithAutoAllow(id, decision, nil)
}

func (h *manualReviewHub) DecideWithAutoAllow(id string, decision manualReviewDecision, autoAllowMinutes *int) error {
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
		h.updateAutoAllowLocked(req.OrganizationID, req.SessionID, *autoAllowMinutes, now)
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

func (h *manualReviewHub) updateAutoAllowLocked(organizationID, sessionID string, minutes int, now time.Time) {
	key := manualReviewPollerKey(organizationID, sessionID)
	state := manualReviewAutoAllow{}
	if minutes > 0 {
		state = manualReviewAutoAllow{Minutes: minutes, ExpiresAt: now.Add(time.Duration(minutes) * time.Minute)}
		h.autoAllow[key] = state
	} else {
		delete(h.autoAllow, key)
	}
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
	req.decision <- manualReviewDecision{Allow: true, Reviewer: "automatic deadline", DecidedAt: now}
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
		NormalExpiresAt:    req.NormalExpiresAt,
		AutoAllowMinutes:   req.AutoAllowMinutes,
		AutoAllowExpiresAt: req.AutoAllowExpiresAt,
	}
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
