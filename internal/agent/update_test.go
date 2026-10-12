package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/relay"
)

func TestStableExecutablePath(t *testing.T) {
	for _, tc := range []struct{ name, path, want string }{
		{"regular", "gosshd-agent.exe", "gosshd-agent.exe"},
		{"temporary", "gosshd-agent.exe.new-123", "gosshd-agent.exe"},
		{"repeated", "gosshd-agent.exe.new-123.new-456", "gosshd-agent.exe"},
		{"unix", "gosshd-agent.new-123", "gosshd-agent"},
		{"unrelated", "gosshd-agent.exe.new-not-an-update", "gosshd-agent.exe.new-not-an-update"},
		{"parent directory", filepath.Join("parent.new-123", "gosshd-agent.exe"), filepath.Join("parent.new-123", "gosshd-agent.exe")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stableExecutablePath(tc.path); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDownloadReplacementUsesCanonicalBasename(t *testing.T) {
	payload := []byte("verified replacement")
	hash := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	current := filepath.Join(t.TempDir(), "gosshd-agent.exe.new-123.new-456")
	downloaded, err := downloadReplacement(context.Background(), server.URL, current, hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(filepath.Base(downloaded), ".new-"); count != 1 {
		t.Fatalf("temporary suffix nested %d times: %s", count, downloaded)
	}
}

func TestDownloadReplacementRemovesFailedDownloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		payload string
	}{
		{"status", http.StatusNotFound, "not found"},
		{"checksum", http.StatusOK, "wrong bytes"},
		{"empty", http.StatusOK, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer server.Close()
			if _, err := downloadReplacement(context.Background(), server.URL, filepath.Join(dir, "gosshd-agent.exe"), strings.Repeat("0", 64)); err == nil {
				t.Fatal("expected failed download")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed download leaked files: %v", entries)
			}
		})
	}
}

func TestCleanupUpdateArtifacts(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "gosshd-agent.exe")
	current := canonical + ".new-123.new-456"
	pending := canonical + ".new-789"
	old := []string{canonical + ".new-123", canonical + ".new-11.new-22"}
	keep := []string{canonical, current, pending, canonical + ".new-not-numeric", filepath.Join(dir, "other.exe.new-123"), filepath.Join(dir, "agent.json"), canonical + ".new-123.txt"}
	for _, path := range append(append([]string{}, old...), keep...) {
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory := canonical + ".new-999"
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := cleanupUpdateArtifacts(current, pending); err != nil {
		t.Fatal(err)
	}
	for _, path := range old {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("old update still exists: %s (%v)", path, err)
		}
	}
	for _, path := range append(keep, directory) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected path removed: %s (%v)", path, err)
		}
	}
}

func TestUpdateSkipsAlreadyRunningChecksum(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte("unexpected repeated download"))
	}))
	defer server.Close()
	client := &Client{cfg: Config{Version: "v-stale-label"}}
	response := protocol.StreamResponse{ServerVersion: "v-new-label", AgentDownloadURL: server.URL, AgentDownloadSHA256: hex.EncodeToString(hash[:])}
	if err := client.maybeUpdateAndRestart(context.Background(), response); err != nil {
		t.Errorf("already-running bytes must not be downloaded again: %v", err)
	}
	if requests != 0 {
		t.Fatalf("same binary downloaded %d times", requests)
	}
}

func TestFailedUpdateKeepsAgentSessionOnline(t *testing.T) {
	checksum := sha256.Sum256([]byte(t.Name() + time.Now().String()))
	var downloads atomic.Int32
	sessionReady := make(chan *yamux.Session, 1)
	handlerDone := make(chan struct{})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/replacement" {
			downloads.Add(1)
			_, _ = w.Write([]byte("invalid downloaded bytes"))
			return
		}
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		conn := relay.NewWSConn(ws)
		defer conn.Close()
		if _, err := protocol.ReadJSONLine[protocol.AgentHello](bufio.NewReader(conn)); err != nil {
			t.Error(err)
			return
		}
		response := protocol.StreamResponse{OK: true, ServerVersion: "v-update", AgentDownloadURL: server.URL + "/replacement", AgentDownloadSHA256: hex.EncodeToString(checksum[:])}
		if err := protocol.WriteJSONLine(conn, response); err != nil {
			t.Error(err)
			return
		}
		config := yamux.DefaultConfig()
		config.ConnectionWriteTimeout = time.Second
		session, err := yamux.Server(conn, config)
		if err != nil {
			t.Error(err)
			return
		}
		defer session.Close()
		sessionReady <- session
		<-handlerDone
	}))
	defer server.Close()
	defer close(handlerDone)
	client, err := New(Config{Server: server.URL, Version: "v-old", IDFile: filepath.Join(t.TempDir(), "id.json"), Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(stableExecutablePath(current) + ".update-state.json") })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.runOnce(ctx) }()
	var session *yamux.Session
	select {
	case session = <-sessionReady:
	case <-time.After(5 * time.Second):
		t.Fatal("server session was not initialized")
	}
	deadline := time.Now().Add(5 * time.Second)
	for downloads.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if downloads.Load() != 1 {
		t.Fatal("failure path did not attempt the expected download")
	}
	ping := make(chan error, 1)
	go func() { _, err := session.Ping(); ping <- err }()
	select {
	case err := <-ping:
		if err != nil {
			t.Fatalf("bad update took usable session offline: %v", err)
		}
	case err := <-result:
		t.Fatalf("bad update terminated agent session: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("bad update prevented session readiness")
	}
	cancel()
	select {
	case <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop after cancellation")
	}
}
