package server

import (
	"errors"
	"sync"

	"github.com/hashicorp/yamux"
)

var ErrAgentOffline = errors.New("agent is offline")

type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*yamux.Session
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: make(map[string]*yamux.Session)}
}

func (r *AgentRegistry) Register(id string, session *yamux.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.agents[id]; old != nil && old != session {
		_ = old.Close()
	}
	r.agents[id] = session
}

func (r *AgentRegistry) Unregister(id string, session *yamux.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agents[id] == session {
		delete(r.agents, id)
	}
}

func (r *AgentRegistry) Get(id string) (*yamux.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session := r.agents[id]
	if session == nil || session.IsClosed() {
		return nil, ErrAgentOffline
	}
	return session, nil
}

func (r *AgentRegistry) OnlineIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.agents))
	for id, session := range r.agents {
		if session != nil && !session.IsClosed() {
			ids = append(ids, id)
		}
	}
	return ids
}
