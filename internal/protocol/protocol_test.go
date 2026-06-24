package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadOrCreateIDStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	first, err := LoadOrCreateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if !IsValidID(first) {
		t.Fatalf("generated invalid id %q", first)
	}
	second, err := LoadOrCreateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("id should be stable: first=%s second=%s", first, second)
	}
}

func TestLoadOrCreateIDRegeneratesInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"id":"nope"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if !IsValidID(id) {
		t.Fatalf("generated invalid id %q", id)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored AgentIDFile
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != id || stored.CreatedAt.IsZero() || time.Since(stored.CreatedAt) > time.Minute {
		t.Fatalf("stored file not refreshed correctly: %+v", stored)
	}
}

func TestSaveAgentAssignmentPreservesRuntimeID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	id, err := LoadOrCreateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveAgentAssignment(path, AgentIDFile{
		AssignedAgentID: "11111111-1111-4111-8111-111111111111",
		TargetID:        "target-1",
		TargetAlias:     "tmp_1",
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := LoadOrCreateAgentIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != id || stored.AssignedAgentID != "11111111-1111-4111-8111-111111111111" || stored.TargetID != "target-1" || stored.TargetAlias != "tmp_1" {
		t.Fatalf("assignment mismatch: %+v", stored)
	}
	if stored.UpdatedAt.IsZero() {
		t.Fatalf("updated_at should be set: %+v", stored)
	}
}
