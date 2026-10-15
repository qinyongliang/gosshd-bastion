package server

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func bootIdentityTestSession(t *testing.T, answer bool) *yamux.Session {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = false
	cfg.LogOutput = io.Discard
	server, err := yamux.Server(left, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	if answer {
		client, err := yamux.Client(right, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
	} else {
		go io.Copy(io.Discard, right)
	}
	return server
}

func TestAgentBootContinuityUsesBootBeforeProcessID(t *testing.T) {
	for _, test := range []struct {
		name           string
		boot, instance string
		want           bool
	}{
		{"same boot new process", "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", true},
		{"new boot copied process", "44444444-4444-4444-8444-444444444444", "22222222-2222-4222-8222-222222222222", false},
		{"legacy same process", "", "22222222-2222-4222-8222-222222222222", true},
		{"missing continuity", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := &App{registry: NewAgentRegistry()}
			session := bootIdentityTestSession(t, true)
			app.registry.RegisterWithInfo("agent", session, AgentRegistryInfo{BootID: "11111111-1111-4111-8111-111111111111", InstanceID: "22222222-2222-4222-8222-222222222222"})
			stored := store.Agent{ID: "agent", CurrentRuntimeID: "runtime"}
			hello := protocol.AgentHello{ID: "runtime", BootID: test.boot, InstanceID: test.instance}
			if got := app.isSameAgentInstance(stored, hello); got != test.want {
				t.Fatalf("continuity=%v, want %v", got, test.want)
			}
		})
	}
}

func TestAgentBootChangeWithOfflineRegistrationResumes(t *testing.T) {
	app := &App{registry: NewAgentRegistry()}
	old := bootIdentityTestSession(t, true)
	app.registry.RegisterWithInfo("agent", old, AgentRegistryInfo{BootID: "11111111-1111-4111-8111-111111111111"})
	old.Close()
	resumed, err := app.agentConnectionCanResume(context.Background(), store.Agent{ID: "agent", CurrentRuntimeID: "runtime"}, protocol.AgentHello{ID: "runtime", BootID: "22222222-2222-4222-8222-222222222222"})
	if err != nil || !resumed {
		t.Fatalf("normal reboot forked an offline registration: %v %v", resumed, err)
	}
}

func TestAgentBootChangeWithLivePeerIsClone(t *testing.T) {
	app := &App{registry: NewAgentRegistry()}
	old := bootIdentityTestSession(t, true)
	app.registry.RegisterWithInfo("agent", old, AgentRegistryInfo{BootID: "11111111-1111-4111-8111-111111111111"})
	resumed, err := app.agentConnectionCanResume(context.Background(), store.Agent{ID: "agent", CurrentRuntimeID: "runtime"}, protocol.AgentHello{ID: "runtime", BootID: "22222222-2222-4222-8222-222222222222"})
	if err != nil || resumed || old.IsClosed() {
		t.Fatalf("live original must be preserved: %v %v", resumed, err)
	}
}

func TestAgentBootChangeWithStalePeerResumes(t *testing.T) {
	app := &App{registry: NewAgentRegistry()}
	old := bootIdentityTestSession(t, false)
	app.registry.RegisterWithInfo("agent", old, AgentRegistryInfo{BootID: "11111111-1111-4111-8111-111111111111"})
	resumed, err := app.agentConnectionCanResume(context.Background(), store.Agent{ID: "agent", CurrentRuntimeID: "runtime"}, protocol.AgentHello{ID: "runtime", BootID: "22222222-2222-4222-8222-222222222222"})
	if err != nil || !resumed || !old.IsClosed() {
		t.Fatalf("unresponsive old connection created a clone: %v %v", resumed, err)
	}
	if _, err := app.registry.Get("agent"); !errors.Is(err, ErrAgentOffline) {
		t.Fatal("stale registration was not retired")
	}
}

func TestAgentReconnectCanceledProbeDoesNotDisconnectOriginal(t *testing.T) {
	app := &App{registry: NewAgentRegistry()}
	old := bootIdentityTestSession(t, false)
	app.registry.RegisterWithInfo("agent", old, AgentRegistryInfo{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	resumed, err := app.agentConnectionCanResume(ctx, store.Agent{ID: "agent", CurrentRuntimeID: "runtime"}, protocol.AgentHello{ID: "runtime"})
	if resumed || !errors.Is(err, context.DeadlineExceeded) || old.IsClosed() {
		t.Fatalf("canceled reconnect modified original: %v %v", resumed, err)
	}
}
