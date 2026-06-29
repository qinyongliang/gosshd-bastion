package server

import (
	"net"
	"testing"

	"github.com/hashicorp/yamux"
)

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

func TestAgentRegistryStoresRuntimeInfo(t *testing.T) {
	reg := NewAgentRegistry()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	clientSession, err := yamux.Client(clientConn, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	serverSession, err := yamux.Server(serverConn, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	reg.RegisterWithInfo("agent-1", serverSession, AgentRegistryInfo{
		Version: "v1",
		GOOS:    "windows",
		GOARCH:  "amd64",
	})
	info, ok := reg.Info("agent-1")
	if !ok {
		t.Fatal("expected runtime info for online agent")
	}
	if info.Version != "v1" || info.GOOS != "windows" || info.GOARCH != "amd64" {
		t.Fatalf("runtime info mismatch: %+v", info)
	}
	reg.Unregister("agent-1", serverSession)
	if _, ok := reg.Info("agent-1"); ok {
		t.Fatal("runtime info should be removed when agent unregisters")
	}
}
