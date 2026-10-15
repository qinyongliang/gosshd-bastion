package agent

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
)

var ErrAgentAlreadyRunning = errors.New("an Agent with this identity is already running on this machine")

// Derive a per-Agent boot identity rather than exposing the machine-wide OS UUID.
// It is never stored in agent.json, so a disk image cannot copy the live BootID.
// This assumes disk-image clones start a fresh kernel; full-memory snapshots
// require separate instance evidence.
func agentBootID(id, systemBoot string) string {
	boot, err := uuid.Parse(strings.TrimSpace(systemBoot))
	if err != nil || boot == uuid.Nil {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("gosshd/boot/v1\x00"+id+"\x00"+boot.String())).String()
}

func loadAgentBootID(id string) string {
	boot, err := readSystemBootID()
	if err != nil {
		log.Printf("agent OS boot identity unavailable; retaining process identity fallback: %v", err)
		return ""
	}
	result := agentBootID(id, boot)
	if result == "" {
		log.Printf("agent OS boot identity invalid; retaining process identity fallback")
	}
	return result
}

func instanceGuardKey(cfg Config, id string) string {
	// The token is hashed, never put into a kernel object name or a lock file.
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.Server+"\x00"+cfg.EnrollmentToken+"\x00"+id)))
}
