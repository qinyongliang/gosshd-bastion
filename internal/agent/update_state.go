package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	maxUpdateDownloadSize  int64 = 128 << 20
	updateRetryInterval          = 15 * time.Minute
	updateAttemptWindow          = 24 * time.Hour
	maxUpdateAttempts            = 3
	maxTotalUpdateAttempts       = 12
)

type updateAttemptState struct {
	WindowStarted time.Time      `json:"window_started"`
	LastAttempt   time.Time      `json:"last_attempt"`
	Attempts      map[string]int `json:"attempts"`
}

func executableSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Persist the retry budget before downloading so restarting the process cannot
// reset a bad-update loop. This tiny metadata file is not an executable backup.
func reserveUpdateAttempt(currentExe, checksum string, now time.Time) (bool, error) {
	path := stableExecutablePath(currentExe) + ".update-state.json"
	var state updateAttemptState
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return false, fmt.Errorf("read update retry state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if state.WindowStarted.IsZero() || now.Sub(state.WindowStarted) >= updateAttemptWindow {
		state = updateAttemptState{WindowStarted: now, LastAttempt: state.LastAttempt}
	}
	if state.Attempts == nil {
		state.Attempts = make(map[string]int)
	}
	checksum = strings.ToLower(checksum)
	totalAttempts := 0
	for _, count := range state.Attempts {
		totalAttempts += count
	}
	if state.Attempts[checksum] >= maxUpdateAttempts || totalAttempts >= maxTotalUpdateAttempts || (!state.LastAttempt.IsZero() && now.Sub(state.LastAttempt) < updateRetryInterval) {
		return false, nil
	}
	state.Attempts[checksum]++
	state.LastAttempt = now
	data, err = json.Marshal(state)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return false, fmt.Errorf("save update retry state: %w", err)
	}
	return true, nil
}

func copyUpdateDownload(destination io.Writer, source io.Reader, maxBytes int64) (int64, error) {
	written, err := io.Copy(destination, io.LimitReader(source, maxBytes+1))
	if err != nil {
		return written, err
	}
	if written > maxBytes {
		return written, fmt.Errorf("agent update download exceeds %d bytes", maxBytes)
	}
	return written, nil
}
