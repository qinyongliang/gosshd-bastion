package server

import "testing"

func TestRegistryOfflineLookup(t *testing.T) {
	reg := NewAgentRegistry()
	if _, err := reg.Get("missing"); err != ErrAgentOffline {
		t.Fatalf("expected ErrAgentOffline, got %v", err)
	}
}

func TestRegistryOnlineIDsEmptyWithoutAgents(t *testing.T) {
	reg := NewAgentRegistry()
	if ids := reg.OnlineIDs(); len(ids) != 0 {
		t.Fatalf("expected no online ids, got %v", ids)
	}
}
