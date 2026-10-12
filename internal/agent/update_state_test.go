package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReserveUpdateAttemptPersistsRetryBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gosshd-agent.exe.new-123")
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	checksum := strings.Repeat("a", 64)
	for _, tc := range []struct {
		offset time.Duration
		want   bool
	}{
		{0, true},
		{time.Second, false},
		{14 * time.Minute, false},
		{15 * time.Minute, true},
		{30 * time.Minute, true},
		{time.Hour, false},
		{23 * time.Hour, false},
		{24 * time.Hour, true},
	} {
		// A fresh read each time also models process restarts.
		got, err := reserveUpdateAttempt(path, checksum, start.Add(tc.offset))
		if err != nil || got != tc.want {
			t.Fatalf("at %s: allowed=%v, err=%v, want %v", tc.offset, got, err, tc.want)
		}
	}
	if allowed, err := reserveUpdateAttempt(path, strings.Repeat("b", 64), start.Add(24*time.Hour+time.Second)); err != nil || allowed {
		t.Fatalf("changing checksum bypassed download cooldown: %v, %v", allowed, err)
	}
	if allowed, err := reserveUpdateAttempt(path, strings.Repeat("b", 64), start.Add(24*time.Hour+15*time.Minute)); err != nil || !allowed {
		t.Fatalf("new checksum blocked: %v, %v", allowed, err)
	}
	if _, err := os.Stat(stableExecutablePath(path) + ".update-state.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".update-state.json"); !os.IsNotExist(err) {
		t.Fatal("state path inherited temporary suffix")
	}
}

func TestReserveUpdateAttemptFailsClosedOnInvalidState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gosshd-agent.exe")
	if err := os.WriteFile(path+".update-state.json", []byte("partial write"), 0600); err != nil {
		t.Fatal(err)
	}
	if allowed, err := reserveUpdateAttempt(path, strings.Repeat("a", 64), time.Now()); err == nil || allowed {
		t.Fatal("invalid retry state allowed unbounded retries")
	}
}

type infiniteUpdateReader struct{ read int64 }

func (r *infiniteUpdateReader) Read(p []byte) (int, error) {
	clear(p)
	r.read += int64(len(p))
	return len(p), nil
}

func TestCopyUpdateDownloadCapsInfiniteStream(t *testing.T) {
	source := &infiniteUpdateReader{}
	written, err := copyUpdateDownload(io.Discard, source, 64)
	if err == nil || written != 65 || source.read != 65 {
		t.Fatalf("unbounded stream: written=%d read=%d err=%v", written, source.read, err)
	}
}

func TestCopyUpdateDownloadAcceptsExactLimit(t *testing.T) {
	var destination bytes.Buffer
	written, err := copyUpdateDownload(&destination, strings.NewReader("1234"), 4)
	if err != nil || written != 4 || destination.String() != "1234" {
		t.Fatalf("exact limit failed: %d, %v", written, err)
	}
}

func TestDownloadReplacementRejectsOversizedContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "134217729")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	if _, err := downloadReplacement(context.Background(), server.URL, filepath.Join(dir, "gosshd-agent.exe"), strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("oversized download accepted: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("oversized download leaked temp file: %v %v", entries, err)
	}
}

func TestReserveUpdateAttemptCannotResetByAlternatingChecksums(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gosshd-agent.exe")
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	checksums := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	for attempt := 0; attempt < 6; attempt++ {
		allowed, err := reserveUpdateAttempt(path, checksums[attempt%2], now.Add(time.Duration(attempt)*updateRetryInterval))
		if err != nil || !allowed {
			t.Fatalf("attempt %d: %v %v", attempt, allowed, err)
		}
	}
	if allowed, err := reserveUpdateAttempt(path, checksums[0], now.Add(6*updateRetryInterval)); err != nil || allowed {
		t.Fatalf("alternating checksums reset attempt budget: %v %v", allowed, err)
	}
}

func TestReserveUpdateAttemptCapsTotalWindowDownloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gosshd-agent.exe")
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for i := 0; i < maxTotalUpdateAttempts; i++ {
		checksum := strings.Repeat(string(rune('a'+i)), 64)
		if allowed, err := reserveUpdateAttempt(path, checksum, now.Add(time.Duration(i)*updateRetryInterval)); err != nil || !allowed {
			t.Fatalf("attempt %d unexpectedly blocked: %v %v", i, allowed, err)
		}
	}
	if allowed, err := reserveUpdateAttempt(path, strings.Repeat("z", 64), now.Add(maxTotalUpdateAttempts*updateRetryInterval)); err != nil || allowed {
		t.Fatalf("unbounded changing update checksums: %v %v", allowed, err)
	}
}
