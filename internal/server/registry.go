package server

import (
	"errors"
	"runtime"
	"sync"

	"github.com/hashicorp/yamux"
)

var ErrAgentOffline = errors.New("agent is offline")

type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*yamux.Session
	infos  map[string]AgentRegistryInfo
}

type AgentRegistryInfo struct {
	Version string
	GOOS    string
	GOARCH  string
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*yamux.Session),
		infos:  make(map[string]AgentRegistryInfo),
	}
}

func (r *AgentRegistry) Register(id string, session *yamux.Session) {
	r.RegisterWithInfo(id, session, AgentRegistryInfo{})
}

func (r *AgentRegistry) RegisterWithInfo(id string, session *yamux.Session, info AgentRegistryInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.agents[id]; old != nil && old != session {
		_ = old.Close()
	}
	if info.GOOS == "" {
		info.GOOS = runtime.GOOS
	}
	if info.GOARCH == "" {
		info.GOARCH = runtime.GOARCH
	}
	r.agents[id] = session
	r.infos[id] = info
}

func (r *AgentRegistry) Unregister(id string, session *yamux.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agents[id] == session {
		delete(r.agents, id)
		delete(r.infos, id)
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

func (r *AgentRegistry) Info(id string) (AgentRegistryInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session := r.agents[id]
	if session == nil || session.IsClosed() {
		return AgentRegistryInfo{}, false
	}
	info := r.infos[id]
	return info, true
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
