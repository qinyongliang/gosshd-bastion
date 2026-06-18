package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/relay"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

const minDirectDownloadBytesPerSecond = 100 * 1024

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (a *App) routes(mux *http.ServeMux) {
	a.apiRoutes(mux)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/download/agent/", a.downloadAgent)
	mux.HandleFunc(protocol.WebSocketPath, a.agentWS)
	mux.Handle("/mcp", a.mcpHandler())
	mux.HandleFunc("/", a.serveWeb)
}

func (a *App) downloadAgent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/download/agent/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "expected /download/agent/{goos}/{goarch}", http.StatusBadRequest)
		return
	}
	goos, goarch := parts[0], parts[1]
	if !isSafePlatformPart(goos) || !isSafePlatformPart(goarch) {
		http.Error(w, "invalid platform", http.StatusBadRequest)
		return
	}
	name := "gosshd-agent"
	if goos == "windows" {
		name += ".exe"
	}
	if path, ok := a.localAgentPath(goos, goarch, name); ok {
		http.ServeFile(w, r, path)
		return
	}
	path, err := a.ensureAgentBinary(goos, goarch, name)
	if err != nil {
		log.Printf("agent download failed for %s/%s: %v", goos, goarch, err)
		http.Error(w, "agent binary unavailable", http.StatusBadGateway)
		return
	}
	http.ServeFile(w, r, path)
}

var agentDownloadLocks sync.Map

func (a *App) ensureAgentBinary(goos, goarch, name string) (string, error) {
	if a.cfg.version() == DefaultVersion {
		return "", errors.New("agent binary missing locally and release version is not configured")
	}

	cachePath := a.agentCachePath(goos, goarch, name)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	key := goos + "/" + goarch
	lockAny, _ := agentDownloadLocks.LoadOrStore(key, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}

	directURL := a.agentReleaseURL(goos, goarch, name)
	if err := downloadAgentFile(directURL, cachePath, true); err == nil {
		return cachePath, nil
	} else {
		log.Printf("direct agent download failed or slow from %s: %v", directURL, err)
	}

	proxyURL := a.proxyReleaseURL(directURL)
	if proxyURL == directURL {
		return "", fmt.Errorf("direct download failed and no proxy URL configured")
	}
	if err := downloadAgentFile(proxyURL, cachePath, false); err != nil {
		return "", err
	}
	return cachePath, nil
}

func (a *App) agentCachePath(goos, goarch, name string) string {
	root := a.cfg.AgentCachePath
	if root == "" {
		root = filepath.Join(os.TempDir(), "gosshd-agent-cache")
	}
	return filepath.Join(root, a.cfg.version(), goos, goarch, name)
}

func (a *App) localAgentPath(goos, goarch, name string) (string, bool) {
	if a.cfg.AgentPath == "" {
		return "", false
	}
	versioned := filepath.Join(a.cfg.AgentPath, a.cfg.version(), goos, goarch, name)
	if _, err := os.Stat(versioned); err == nil {
		return versioned, true
	}
	return "", false
}

func (a *App) agentReleaseURL(goos, goarch, name string) string {
	version := a.cfg.version()
	assetName := fmt.Sprintf("gosshd-agent-%s-%s-%s", version, goos, goarch)
	if goos == "windows" {
		assetName += ".exe"
	}
	base := strings.TrimRight(a.cfg.releaseBaseURL(), "/")
	return fmt.Sprintf("%s/%s/%s", base, url.PathEscape(version), url.PathEscape(assetName))
}

func (a *App) proxyReleaseURL(rawURL string) string {
	proxy := a.cfg.releaseProxyURL()
	if proxy == "" {
		return rawURL
	}
	return strings.TrimRight(proxy, "/") + "/" + rawURL
}

func downloadAgentFile(rawURL, cachePath string, enforceSpeed bool) error {
	tmpPath := cachePath + ".tmp"
	_ = os.Remove(tmpPath)

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	written, copyErr := copyWithSpeedCheck(tmp, resp.Body, enforceSpeed)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if written == 0 {
		_ = os.Remove(tmpPath)
		return errors.New("empty download")
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func copyWithSpeedCheck(dst io.Writer, src io.Reader, enforceSpeed bool) (int64, error) {
	started := time.Now()
	buf := make([]byte, 32*1024)
	var written int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			nw, writeErr := dst.Write(buf[:n])
			written += int64(nw)
			if writeErr != nil {
				return written, writeErr
			}
			if nw != n {
				return written, io.ErrShortWrite
			}
			if enforceSpeed {
				elapsed := time.Since(started)
				if elapsed >= 5*time.Second {
					speed := float64(written) / elapsed.Seconds()
					if speed < minDirectDownloadBytesPerSecond {
						return written, fmt.Errorf("download speed %.0f B/s below %d B/s", speed, minDirectDownloadBytesPerSecond)
					}
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func isSafePlatformPart(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}

func (a *App) agentWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("agent websocket upgrade failed: %v", err)
		return
	}
	conn := relay.NewWSConn(ws)
	reader := bufio.NewReader(conn)
	hello, err := protocol.ReadJSONLine[protocol.AgentHello](reader)
	if err != nil {
		_ = conn.Close()
		log.Printf("agent hello failed: %v", err)
		return
	}
	if !protocol.IsValidID(hello.ID) {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "invalid agent id"})
		_ = conn.Close()
		return
	}
	if strings.TrimSpace(hello.EnrollmentToken) == "" {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "enrollment token is required"})
		_ = conn.Close()
		return
	}
	registryID := hello.ID
	if err := a.ensureServices(r.Context()); err != nil {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "server storage unavailable"})
		_ = conn.Close()
		return
	}
	enrollment, err := a.store.Repository().GetAgentEnrollmentByTokenHash(r.Context(), codeHash(hello.EnrollmentToken))
	if err != nil || time.Now().UTC().After(enrollment.ExpiresAt) {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "invalid enrollment token"})
		_ = conn.Close()
		return
	}
	agent, err := a.store.Repository().UpsertAgent(r.Context(), store.UpsertAgentParams{
		OwnerType:        enrollment.OwnerType,
		OwnerID:          enrollment.OwnerID,
		EnrollmentID:     enrollment.ID,
		Label:            enrollment.Label,
		CurrentRuntimeID: hello.ID,
	})
	if err != nil {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "agent enrollment failed"})
		_ = conn.Close()
		return
	}
	if err := a.ensureAgentTarget(r.Context(), enrollment, agent); err != nil {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "agent target creation failed"})
		_ = conn.Close()
		return
	}
	registryID = agent.ID
	goos, goarch := hello.GOOS, hello.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	downloadURL := publicBaseURL(r, a.cfg.publicHost()) + "/download/agent/" + goos + "/" + goarch
	if err := protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: true, ServerVersion: a.cfg.version(), AgentDownloadURL: downloadURL}); err != nil {
		_ = conn.Close()
		return
	}
	session, err := yamux.Server(conn, nil)
	if err != nil {
		_ = conn.Close()
		log.Printf("yamux server failed: %v", err)
		return
	}
	a.registry.Register(registryID, session)
	log.Printf("agent online: %s", registryID)
	go func() {
		<-session.CloseChan()
		a.registry.Unregister(registryID, session)
		log.Printf("agent offline: %s", registryID)
	}()
}

func (a *App) ensureAgentTarget(ctx context.Context, enrollment store.AgentEnrollment, agent store.Agent) error {
	targets, err := a.store.Repository().ListSSHTargets(ctx, enrollment.OwnerType, enrollment.OwnerID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.TargetType == store.TargetAgent && target.AgentID == agent.ID {
			_, err := a.store.Repository().UpdateSSHTarget(ctx, target.ID, store.UpdateSSHTargetParams{
				Host:           enrollment.DefaultHost,
				Port:           enrollment.DefaultPort,
				RemoteUsername: target.RemoteUsername,
				AuthType:       target.AuthType,
				AgentID:        agent.ID,
			})
			return err
		}
	}
	alias := enrollment.Label
	if strings.TrimSpace(alias) == "" {
		alias = "agent-" + agent.ID[:8]
	}
	_, err = a.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType:      enrollment.OwnerType,
		OwnerID:        enrollment.OwnerID,
		Name:           alias,
		Alias:          alias,
		TargetType:     store.TargetAgent,
		Host:           enrollment.DefaultHost,
		Port:           enrollment.DefaultPort,
		RemoteUsername: "root",
		AuthType:       store.AuthPassword,
		AgentID:        agent.ID,
		CreatedBy:      enrollment.CreatedBy,
	})
	return err
}

func publicBaseURL(r *http.Request, configuredHost string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := configuredHost
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		host = strings.TrimSpace(host)
	}
	return scheme + "://" + host
}
