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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

var errAgentRestarting = errors.New("agent restarting after update")

func (c *Client) maybeUpdateAndRestart(ctx context.Context, resp protocol.StreamResponse) error {
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
	tmpPath, err := downloadReplacement(ctx, downloadURL, currentExe, expectedSHA256)
	if err != nil {
		return fmt.Errorf("%s failed: %w", logPrefix, err)
	}
	restartPath, err := installReplacement(currentExe, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s replace failed: %w", logPrefix, err)
	}
	if err := restartCurrentProcess(restartPath); err != nil {
		return fmt.Errorf("%s restart failed: %w", logPrefix, err)
	}
	return errAgentRestarting
}

func downloadReplacement(ctx context.Context, rawURL, currentExe, expectedSHA256 string) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(currentExe), filepath.Base(currentExe)+".new-*")
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
	hasher := sha256.New()
	written, err := io.Copy(tmp, io.TeeReader(resp.Body, hasher))
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

func restartCurrentProcess(exePath string) error {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if runtime.GOOS == "windows" {
		return cmd.Start()
	}
	return syscallExec(exePath, append([]string{exePath}, os.Args[1:]...), os.Environ())
}
