package server

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type runningAudit struct {
	mu sync.Mutex

	id             string
	userID         string
	targetID       string
	targetName     string
	targetAlias    string
	targetHost     string
	targetPort     int
	targetUsername string
	organizationID string
	publicKeyName  string
	command        string
	requestType    string
	policyDecision string
	policyReason   string
	startedAt      time.Time
	output         strings.Builder
	endedAt        time.Time
	exitCode       *int
}

type runningAuditStore struct {
	mu    sync.RWMutex
	items map[string]*runningAudit
}

func newRunningAuditStore() *runningAuditStore {
	return &runningAuditStore{items: map[string]*runningAudit{}}
}

func (s *runningAuditStore) start(params store.CreateCommandAuditLogParams) *runningAudit {
	normalizeAuditIdentity(&params)
	item := &runningAudit{
		id:             uuid.NewString(),
		userID:         strings.TrimSpace(params.UserID),
		targetID:       strings.TrimSpace(params.TargetID),
		targetName:     strings.TrimSpace(params.TargetName),
		targetAlias:    strings.TrimSpace(params.TargetAlias),
		targetHost:     strings.TrimSpace(params.TargetHost),
		targetPort:     params.TargetPort,
		targetUsername: strings.TrimSpace(params.TargetUsername),
		organizationID: strings.TrimSpace(params.OrganizationID),
		publicKeyName:  params.PublicKeyName,
		command:        strings.TrimSpace(params.Command),
		requestType:    strings.TrimSpace(params.RequestType),
		policyDecision: strings.TrimSpace(params.PolicyDecision),
		policyReason:   strings.TrimSpace(params.PolicyReason),
		startedAt:      params.StartedAt.UTC(),
	}
	if item.startedAt.IsZero() {
		item.startedAt = time.Now().UTC()
	}
	s.mu.Lock()
	s.items[item.id] = item
	s.mu.Unlock()
	return item
}

func (s *runningAuditStore) remove(item *runningAudit) {
	if item == nil {
		return
	}
	s.mu.Lock()
	delete(s.items, item.id)
	s.mu.Unlock()
}

func (s *runningAuditStore) list(userID, organizationID string, isSystemAdmin bool) []*runningAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*runningAudit, 0, len(s.items))
	for _, item := range s.items {
		item.mu.Lock()
		if organizationID != "" && item.organizationID != organizationID {
			item.mu.Unlock()
			continue
		}
		matches := isSystemAdmin
		if !matches && organizationID != "" {
			matches = item.organizationID == organizationID && (userID == "" || item.userID == userID)
		} else if !matches {
			matches = item.userID == userID
		}
		item.mu.Unlock()
		if matches {
			out = append(out, item)
		}
	}
	return out
}

func (s *runningAuditStore) get(id string) *runningAudit {
	s.mu.RLock()
	item := s.items[strings.TrimSpace(id)]
	s.mu.RUnlock()
	return item
}

func (r *runningAudit) append(data string) {
	if r == nil || data == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output.WriteString(data)
}

func (r *runningAudit) finish(exitCode int, endedAt time.Time) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.exitCode = &exitCode
	r.endedAt = endedAt.UTC()
	r.mu.Unlock()
}

func (r *runningAudit) snapshot() runningAuditSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return runningAuditSnapshot{
		ID: r.id, UserID: r.userID, TargetID: r.targetID, TargetName: r.targetName, TargetAlias: r.targetAlias,
		TargetHost: r.targetHost, TargetPort: r.targetPort, TargetUsername: r.targetUsername, OrganizationID: r.organizationID,
		Command: r.command, RequestType: r.requestType, PolicyDecision: r.policyDecision, PolicyReason: r.policyReason,
		StartedAt: r.startedAt, Output: r.output.String(), EndedAt: r.endedAt, ExitCode: r.exitCode,
		PublicKeyName: r.publicKeyName,
	}
}

type runningAuditSnapshot struct {
	PublicKeyName  string
	ID             string
	UserID         string
	TargetID       string
	TargetName     string
	TargetAlias    string
	TargetHost     string
	TargetPort     int
	TargetUsername string
	OrganizationID string
	Command        string
	RequestType    string
	PolicyDecision string
	PolicyReason   string
	StartedAt      time.Time
	EndedAt        time.Time
	ExitCode       *int
	Output         string
}
