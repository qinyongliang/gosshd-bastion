package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

var errAgentRestarting = errors.New("agent restarting after update")

var updateTempSuffix = regexp.MustCompile(`(?:\.new-[0-9]+)+$`)

func stableExecutablePath(currentExe string) string {
	base := filepath.Base(currentExe)
	return filepath.Join(filepath.Dir(currentExe), updateTempSuffix.ReplaceAllString(base, ""))
}

// Only remove recognized update files next to this executable. Never remove
// the running image, the pending replacement, directories, or unrelated files.
func cleanupUpdateArtifacts(currentExe, pendingReplacement string) error {
	canonical := stableExecutablePath(currentExe)
	dir := filepath.Dir(canonical)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var cleanupErrors []error
	for _, entry := range entries {
		if !entry.Type().IsRegular() || entry.Name() == filepath.Base(canonical) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if stableExecutablePath(path) != canonical || path == currentExe || path == pendingReplacement {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove update artifact %s: %w", path, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

// The old Windows updater starts the new version directly from a .new-* file.
// Finalize that first upgrade before connecting, even if the version now matches
// the server, so users do not need another release to reclaim old update files.
func finalizeLegacyUpdate() (bool, error) {
	currentExe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if currentExe == stableExecutablePath(currentExe) {
		return false, nil
	}
	if err := cleanupUpdateArtifacts(currentExe, ""); err != nil {
		log.Printf("agent legacy update cleanup: %v", err)
	}
	if err := replaceAndRestart(currentExe, currentExe); err != nil {
		return false, fmt.Errorf("finalize legacy update: %w", err)
	}
	return true, nil
}

func (c *Client) maybeUpdateAndRestart(ctx context.Context, resp protocol.StreamResponse) error {
	if c.cfg.DisableAutoUpdate {
		return nil
	}
	serverVersion := strings.TrimSpace(resp.ServerVersion)
	if serverVersion == "" || serverVersion == c.cfg.Version {
		return nil
	}
	if c.cfg.Version == "" || c.cfg.Version == "dev" {
		return nil
	}
	downloadURL := strings.TrimSpace(resp.AgentDownloadURL)
	if downloadURL == "" {
		return nil
	}
	expectedSHA256 := strings.TrimSpace(resp.AgentDownloadSHA256)
	if expectedSHA256 == "" {
		log.Printf("agent update %s -> %s skipped: missing download checksum", c.cfg.Version, serverVersion)
		return nil
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil || len(expectedSHA256) != sha256.Size*2 {
		return fmt.Errorf("invalid agent download checksum")
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	logPrefix := fmt.Sprintf("agent update %s -> %s", c.cfg.Version, serverVersion)
	// Reclaim obsolete binaries before downloading, even on nearly full disks.
	if err := cleanupUpdateArtifacts(currentExe, ""); err != nil {
		log.Printf("%s cleanup: %v", logPrefix, err)
	}
	currentSHA256, err := executableSHA256(currentExe)
	if err != nil {
		return fmt.Errorf("%s inspect current binary: %w", logPrefix, err)
	}
	if strings.EqualFold(currentSHA256, expectedSHA256) {
		log.Printf("%s skipped: offered binary is already running (version labels differ)", logPrefix)
		return nil
	}
	allowed, err := reserveUpdateAttempt(currentExe, expectedSHA256, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("%s retry guard: %w", logPrefix, err)
	}
	if !allowed {
		log.Printf("%s deferred: retry budget is %d/checksum and %d total per 24h, 15m apart", logPrefix, maxUpdateAttempts, maxTotalUpdateAttempts)
		return nil
	}
	tmpPath, err := downloadReplacement(ctx, downloadURL, currentExe, expectedSHA256)
	if err != nil {
		return fmt.Errorf("%s failed: %w", logPrefix, err)
	}
	if err := replaceAndRestart(currentExe, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s failed: %w", logPrefix, err)
	}
	return errAgentRestarting
}

func downloadReplacement(ctx context.Context, rawURL, currentExe, expectedSHA256 string) (string, error) {
	canonical := stableExecutablePath(currentExe)
	tmp, err := os.CreateTemp(filepath.Dir(canonical), filepath.Base(canonical)+".new-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	if resp.ContentLength > maxUpdateDownloadSize {
		return "", fmt.Errorf("agent update Content-Length %d exceeds %d bytes", resp.ContentLength, maxUpdateDownloadSize)
	}
	hasher := sha256.New()
	written, err := copyUpdateDownload(tmp, io.TeeReader(resp.Body, hasher), maxUpdateDownloadSize)
	if err != nil {
		return "", err
	}
	if written == 0 {
		return "", errors.New("empty download")
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, expectedSHA256) {
		return "", fmt.Errorf("sha256 mismatch: got %s want %s", got, expectedSHA256)
	}
	if err := tmp.Chmod(0o755); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	cleanup = false
	return tmpPath, nil
}
