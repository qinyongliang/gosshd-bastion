package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

func TestBootIdentityStableAcrossClientsAndNotPersisted(t *testing.T) {
	if _, err := readSystemBootID(); err != nil {
		t.Skipf("OS boot ID not available: %v", err)
	}
	cfg := Config{Server: "https://agent.example.test", IDFile: filepath.Join(t.TempDir(), "agent.json"), Root: t.TempDir()}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.IsValidID(first.bootID) || first.bootID != second.bootID {
		t.Fatal("same OS boot must have the same Agent-scoped boot identity")
	}
	if first.instanceID == second.instanceID {
		t.Fatal("process instance IDs should remain distinct")
	}
	raw, err := os.ReadFile(cfg.IDFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("boot_id")) || bytes.Contains(raw, []byte(first.bootID)) {
		t.Fatal("boot identity must not be persisted in the cloned ID file")
	}
}

func TestAgentBootIdentityIsScopedAndChangesAfterReboot(t *testing.T) {
	boot := "11111111-1111-4111-8111-111111111111"
	first := agentBootID("agent-one", boot)
	if !protocol.IsValidID(first) || first != agentBootID("agent-one", boot) {
		t.Fatal("derived identity is not stable")
	}
	if first == agentBootID("agent-two", boot) || first == agentBootID("agent-one", "22222222-2222-4222-8222-222222222222") {
		t.Fatal("different Agent or boot identities collided")
	}
	for _, bad := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if agentBootID("agent-one", bad) != "" {
			t.Fatalf("invalid boot %q must not create a shared constant identity", bad)
		}
	}
}

func TestAgentInstanceGuardPreventsDuplicateAndReleases(t *testing.T) {
	key := instanceGuardKey(Config{Server: "https://agent.example.test", EnrollmentToken: "synthetic-token"}, filepath.Base(t.TempDir()))
	release, err := acquireAgentInstanceGuard(key, "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if duplicate, err := acquireAgentInstanceGuard(key, ""); !errors.Is(err, ErrAgentAlreadyRunning) {
		if duplicate != nil {
			duplicate()
		}
		t.Fatalf("duplicate startup allowed: %v", err)
	}
	release()
	again, err := acquireAgentInstanceGuard(key, "")
	if err != nil {
		t.Fatalf("guard not released for process restart: %v", err)
	}
	again()
}

func TestAgentInstanceGuardScopesSeparateEnrollments(t *testing.T) {
	cfg := Config{Server: "https://agent.example.test", EnrollmentToken: "owner-one"}
	first := instanceGuardKey(cfg, "same-runtime")
	cfg.EnrollmentToken = "owner-two"
	if first == instanceGuardKey(cfg, "same-runtime") {
		t.Fatal("separate enrollment guards collided")
	}
	cfg.EnrollmentToken = "owner-one"
	cfg.Server = "https://other.example.test"
	if first == instanceGuardKey(cfg, "same-runtime") {
		t.Fatal("separate server guards collided")
	}
}

func TestClientRunRejectsDuplicateBeforeConnecting(t *testing.T) {
	cfg := Config{Server: "https://agent.example.test", EnrollmentToken: "synthetic-token", IDFile: filepath.Join(t.TempDir(), "agent.json"), Root: t.TempDir()}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireAgentInstanceGuard(instanceGuardKey(client.cfg, client.ID()), cfg.IDFile)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Run(ctx); !errors.Is(err, ErrAgentAlreadyRunning) {
		t.Fatalf("duplicate Run entered reconnect loop: %v", err)
	}
}
