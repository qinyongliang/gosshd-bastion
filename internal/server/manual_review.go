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
	notify        chan struct{}
}

type manualReviewRequest struct {
	ID              string
	OrganizationID  string
	TargetID        string
	TargetName      string
	TargetAlias     string
	UserID          string
	UserEmail       string
	UserDisplayName string
	Command         string
	Reason          string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	decision        chan manualReviewDecision
}

type manualReviewDecision struct {
	Allow      bool
	ReviewerID string
	Reviewer   string
	DecidedAt  time.Time
}

type manualReviewSnapshot struct {
	ID              string
	OrganizationID  string
	TargetID        string
	TargetName      string
	TargetAlias     string
	UserID          string
	UserEmail       string
	UserDisplayName string
	Command         string
	Reason          string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

func newManualReviewHub() *manualReviewHub {
	return &manualReviewHub{
		pending:       make(map[string]*manualReviewRequest),
		activePollers: make(map[string]int),
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
	req.decision = make(chan manualReviewDecision, 1)
	h.pending[req.ID] = &req
	h.signalLocked()
	return snapshotManualReview(&req), req.decision
}

func (h *manualReviewHub) List(ctx context.Context, organizationID string, timeout time.Duration, knownIDs map[string]struct{}) ([]manualReviewSnapshot, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	registered := false
	defer func() {
		if registered {
			h.unregisterPoller(organizationID)
		}
	}()
	for {
		reviews, notify, didRegister := h.listOrNotify(organizationID, knownIDs, timeout > 0 && !registered)
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

func (h *manualReviewHub) HasActivePollers(organizationID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activePollers[organizationID] > 0
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

func (h *manualReviewHub) Decide(id string, decision manualReviewDecision) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireLocked(time.Now().UTC())
	req, ok := h.pending[id]
	if !ok {
		return errManualReviewNotFound
	}
	delete(h.pending, id)
	decision.DecidedAt = time.Now().UTC()
	req.decision <- decision
	close(req.decision)
	h.signalLocked()
	return nil
}

func (h *manualReviewHub) Expire(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if req, ok := h.pending[id]; ok {
		delete(h.pending, id)
		close(req.decision)
		h.signalLocked()
	}
}

func (h *manualReviewHub) listOrNotify(organizationID string, knownIDs map[string]struct{}, registerPoller bool) ([]manualReviewSnapshot, <-chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireLocked(time.Now().UTC())
	reviews := h.listLocked(organizationID, knownIDs)
	registered := false
	if registerPoller && len(reviews) == 0 {
		h.activePollers[organizationID]++
		registered = true
	}
	return reviews, h.notify, registered
}

func (h *manualReviewHub) listLocked(organizationID string, knownIDs map[string]struct{}) []manualReviewSnapshot {
	out := []manualReviewSnapshot{}
	for _, req := range h.pending {
		if req.OrganizationID == organizationID && !knownManualReviewID(req.ID, knownIDs) {
			out = append(out, snapshotManualReview(req))
		}
	}
	return out
}

func (h *manualReviewHub) expireLocked(now time.Time) {
	changed := false
	for id, req := range h.pending {
		if now.After(req.ExpiresAt) {
			delete(h.pending, id)
			close(req.decision)
			changed = true
		}
	}
	if changed {
		h.signalLocked()
	}
}

func (h *manualReviewHub) signalLocked() {
	close(h.notify)
	h.notify = make(chan struct{})
}

func (h *manualReviewHub) unregisterPoller(organizationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activePollers[organizationID] <= 1 {
		delete(h.activePollers, organizationID)
		return
	}
	h.activePollers[organizationID]--
}

func snapshotManualReview(req *manualReviewRequest) manualReviewSnapshot {
	return manualReviewSnapshot{
		ID:              req.ID,
		OrganizationID:  req.OrganizationID,
		TargetID:        req.TargetID,
		TargetName:      req.TargetName,
		TargetAlias:     req.TargetAlias,
		UserID:          req.UserID,
		UserEmail:       req.UserEmail,
		UserDisplayName: req.UserDisplayName,
		Command:         req.Command,
		Reason:          req.Reason,
		CreatedAt:       req.CreatedAt,
		ExpiresAt:       req.ExpiresAt,
	}
}

func knownManualReviewID(id string, knownIDs map[string]struct{}) bool {
	if len(knownIDs) == 0 {
		return false
	}
	_, ok := knownIDs[id]
	return ok
}
