package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	winPTYVersion = "0.4.3"
	winPTYZipName = "winpty-0.4.3-msvc2015.zip"
	winPTYZipSHA  = "35a48ece2ff4acdcbc8299d4920de53eb86b1fb41e64d2fe5ae7898931bcee89"
	winPTYURL     = "https://github.com/rprichard/winpty/releases/download/0.4.3/winpty-0.4.3-msvc2015.zip"
)

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		return requestOriginAllowed(r)
	},
}

func (a *App) routes(mux *http.ServeMux) {
	a.apiRoutes(mux)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/download/agent/", a.downloadAgent)
	mux.HandleFunc("/download/winpty/", a.downloadWinPTY)
	mux.HandleFunc(protocol.WebSocketPath, a.agentWS)
	mux.Handle("/mcp", a.mcpHandler())
	mux.HandleFunc("/", a.serveWeb)
}

func (a *App) downloadWinPTY(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/download/winpty/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] != "windows" || parts[1] == "" {
		http.Error(w, "expected /download/winpty/windows/{goarch}", http.StatusBadRequest)
		return
	}
	goarch := parts[1]
	checksumOnly := false
	if strings.HasSuffix(goarch, ".sha256") {
		checksumOnly = true
		goarch = strings.TrimSuffix(goarch, ".sha256")
	}
	if goarch != "amd64" && goarch != "386" {
		http.Error(w, "unsupported winpty platform", http.StatusBadRequest)
		return
	}
	if checksumOnly {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, winPTYZipSHA)
		return
	}
	path, err := a.ensureWinPTYZip()
	if err != nil {
		log.Printf("winpty download failed: %v", err)
		http.Error(w, "winpty unavailable", http.StatusBadGateway)
		return
	}
	http.ServeFile(w, r, path)
}

func (a *App) downloadAgent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/download/agent/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "expected /download/agent/{goos}/{goarch}", http.StatusBadRequest)
		return
	}
	goos, goarch := parts[0], parts[1]
	checksumOnly := false
	if strings.HasSuffix(goarch, ".sha256") {
		checksumOnly = true
		goarch = strings.TrimSuffix(goarch, ".sha256")
	}
	if !isSafePlatformPart(goos) || !isSafePlatformPart(goarch) {
		http.Error(w, "invalid platform", http.StatusBadRequest)
		return
	}
	name := "gosshd-agent"
	if goos == "windows" {
		name += ".exe"
	}
	if path, ok := a.localAgentPath(goos, goarch, name); ok {
		if checksumOnly {
			a.serveAgentChecksum(w, path)
			return
		}
		http.ServeFile(w, r, path)
		return
	}
	path, err := a.ensureAgentBinary(goos, goarch, name)
	if err != nil {
		log.Printf("agent download failed for %s/%s: %v", goos, goarch, err)
		http.Error(w, "agent binary unavailable", http.StatusBadGateway)
		return
	}
	if checksumOnly {
		a.serveAgentChecksum(w, path)
		return
	}
	http.ServeFile(w, r, path)
}

var agentDownloadLocks sync.Map

func (a *App) ensureWinPTYZip() (string, error) {
	root := a.cfg.AgentCachePath
	if root == "" {
		root = filepath.Join(os.TempDir(), "gosshd-agent-cache")
	}
	cachePath := filepath.Join(root, "winpty", winPTYVersion, winPTYZipName)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}
	lockAny, _ := agentDownloadLocks.LoadOrStore("winpty/"+winPTYVersion, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}
	if err := downloadAgentFile(winPTYURL, cachePath, true, winPTYZipSHA); err == nil {
		return cachePath, nil
	} else {
		log.Printf("direct winpty download failed or slow from %s: %v", winPTYURL, err)
	}
	proxyURL := a.proxyReleaseURL(winPTYURL)
	if proxyURL == winPTYURL {
		return "", fmt.Errorf("direct winpty download failed and no proxy URL configured")
	}
	if err := downloadAgentFile(proxyURL, cachePath, false, winPTYZipSHA); err != nil {
		return "", err
	}
	return cachePath, nil
}

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

	assetName := a.agentAssetName(goos, goarch)
	checksumURL := a.releaseChecksumsURL()
	expectedSHA256, err := a.fetchReleaseChecksumWithProxy(checksumURL, assetName)
	if err != nil {
		return "", fmt.Errorf("fetch release checksum: %w", err)
	}
	directURL := a.agentReleaseURL(goos, goarch, name)
	if err := downloadAgentFile(directURL, cachePath, true, expectedSHA256); err == nil {
		return cachePath, nil
	} else {
		log.Printf("direct agent download failed or slow from %s: %v", directURL, err)
	}

	proxyURL := a.proxyReleaseURL(directURL)
	if proxyURL == directURL {
		return "", fmt.Errorf("direct download failed and no proxy URL configured")
	}
	if err := downloadAgentFile(proxyURL, cachePath, false, expectedSHA256); err != nil {
		return "", err
	}
	return cachePath, nil
}

func (a *App) fetchReleaseChecksumWithProxy(checksumURL, assetName string) (string, error) {
	proxyChecksumURL := a.proxyReleaseURL(checksumURL)
	if proxyChecksumURL != checksumURL {
		if checksum, err := fetchReleaseChecksum(proxyChecksumURL, assetName); err == nil {
			return checksum, nil
		} else {
			log.Printf("proxy release checksum fetch failed from %s: %v", proxyChecksumURL, err)
		}
	}
	checksum, err := fetchReleaseChecksum(checksumURL, assetName)
	if err == nil {
		return checksum, nil
	}
	return "", err
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
	_ = name
	assetName := a.agentAssetName(goos, goarch)
	base := strings.TrimRight(a.cfg.releaseBaseURL(), "/")
	return fmt.Sprintf("%s/%s/%s", base, url.PathEscape(a.cfg.version()), url.PathEscape(assetName))
}

func (a *App) agentAssetName(goos, goarch string) string {
	version := a.cfg.version()
	assetName := fmt.Sprintf("gosshd-agent-%s-%s-%s", version, goos, goarch)
	if goos == "windows" {
		assetName += ".exe"
	}
	return assetName
}

func (a *App) releaseChecksumsURL() string {
	base := strings.TrimRight(a.cfg.releaseBaseURL(), "/")
	return fmt.Sprintf("%s/%s/checksums.txt", base, url.PathEscape(a.cfg.version()))
}

func (a *App) proxyReleaseURL(rawURL string) string {
	proxy := a.cfg.releaseProxyURL()
	if proxy == "" {
		return rawURL
	}
	return strings.TrimRight(proxy, "/") + "/" + rawURL
}

func downloadAgentFile(rawURL, cachePath string, enforceSpeed bool, expectedSHA256 string) error {
	if strings.TrimSpace(expectedSHA256) == "" {
		return errors.New("expected sha256 is required")
	}
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
	hasher := sha256.New()
	written, copyErr := copyWithSpeedCheck(tmp, io.TeeReader(resp.Body, hasher), enforceSpeed)
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
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, expectedSHA256) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, expectedSHA256)
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func fetchReleaseChecksum(rawURL, assetName string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) == assetName {
			if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != sha256.Size*2 {
				return "", fmt.Errorf("invalid checksum for %s", assetName)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) serveAgentChecksum(w http.ResponseWriter, path string) {
	sum, err := fileSHA256(path)
	if err != nil {
		http.Error(w, "agent checksum unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, sum)
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
	if !a.allowAuthAttempt(r, "agent:"+hello.ID, 60, 5*time.Minute) {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "too many agent enrollment attempts"})
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
	agent, target, err := a.enrollAgentConnection(r.Context(), enrollment, hello)
	if err != nil {
		_ = protocol.WriteJSONLine(conn, protocol.StreamResponse{OK: false, Error: "agent enrollment failed"})
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
	downloadSHA256, err := a.agentDownloadSHA256(goos, goarch)
	if err != nil {
		log.Printf("agent checksum unavailable for %s/%s: %v", goos, goarch, err)
	}
	if err := protocol.WriteJSONLine(conn, protocol.StreamResponse{
		OK:                  true,
		ServerVersion:       a.cfg.version(),
		AgentDownloadURL:    downloadURL,
		AgentDownloadSHA256: downloadSHA256,
		AssignedAgentID:     agent.ID,
		TargetID:            target.ID,
		TargetAlias:         target.Alias,
	}); err != nil {
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

func (a *App) enrollAgentConnection(ctx context.Context, enrollment store.AgentEnrollment, hello protocol.AgentHello) (store.Agent, store.SSHTarget, error) {
	agentID := strings.TrimSpace(hello.AssignedAgentID)
	if agentID != "" {
		agent, err := a.store.Repository().GetAgent(ctx, agentID)
		if err == nil && agent.EnrollmentID == enrollment.ID && agent.OwnerType == enrollment.OwnerType && agent.OwnerID == enrollment.OwnerID {
			if _, err := a.registry.Get(agent.ID); err == nil {
				return a.createAgentConnection(ctx, enrollment, hello.ID, true)
			}
			updated, err := a.store.Repository().UpsertAgent(ctx, store.UpsertAgentParams{
				ID:               agent.ID,
				OwnerType:        enrollment.OwnerType,
				OwnerID:          enrollment.OwnerID,
				EnrollmentID:     enrollment.ID,
				Label:            enrollment.Label,
				CurrentRuntimeID: hello.ID,
			})
			if err != nil {
				return store.Agent{}, store.SSHTarget{}, err
			}
			target, err := a.ensureAgentTarget(ctx, enrollment, updated, false)
			return updated, target, err
		}
	}
	return a.createAgentConnection(ctx, enrollment, hello.ID, false)
}

func (a *App) createAgentConnection(ctx context.Context, enrollment store.AgentEnrollment, runtimeID string, forceNewTarget bool) (store.Agent, store.SSHTarget, error) {
	agent, err := a.store.Repository().UpsertAgent(ctx, store.UpsertAgentParams{
		OwnerType:        enrollment.OwnerType,
		OwnerID:          enrollment.OwnerID,
		EnrollmentID:     enrollment.ID,
		Label:            enrollment.Label,
		CurrentRuntimeID: runtimeID,
	})
	if err != nil {
		return store.Agent{}, store.SSHTarget{}, err
	}
	target, err := a.ensureAgentTarget(ctx, enrollment, agent, forceNewTarget)
	if err != nil {
		return store.Agent{}, store.SSHTarget{}, err
	}
	return agent, target, nil
}

func (a *App) ensureAgentTarget(ctx context.Context, enrollment store.AgentEnrollment, agent store.Agent, forceNew bool) (store.SSHTarget, error) {
	targets, err := a.store.Repository().ListSSHTargets(ctx, enrollment.OwnerType, enrollment.OwnerID)
	if err != nil {
		return store.SSHTarget{}, err
	}
	if !forceNew {
		for _, target := range targets {
			if target.TargetType == store.TargetAgent && target.AgentID == agent.ID {
				return a.store.Repository().UpdateSSHTarget(ctx, target.ID, store.UpdateSSHTargetParams{
					Host:           enrollment.DefaultHost,
					Port:           enrollment.DefaultPort,
					RemoteUsername: target.RemoteUsername,
					AuthType:       target.AuthType,
					AgentID:        agent.ID,
				})
			}
		}
	}
	template := firstAgentTargetForEnrollment(ctx, a, enrollment)
	if !forceNew {
		alias := baseAgentAlias(enrollment, agent)
		for _, target := range targets {
			if target.TargetType == store.TargetAgent && target.Alias == alias {
				return a.store.Repository().UpdateSSHTarget(ctx, target.ID, store.UpdateSSHTargetParams{
					Host:           enrollment.DefaultHost,
					Port:           enrollment.DefaultPort,
					RemoteUsername: target.RemoteUsername,
					AuthType:       target.AuthType,
					AgentID:        agent.ID,
				})
			}
		}
	}
	base := baseAgentAlias(enrollment, agent)
	numberedSequence := forceNew
	if template != nil {
		base = numberedAliasBase(template.Alias, base)
		numberedSequence = numberedSequence || template.Alias != base
	}
	alias := nextAgentAlias(base, targets, numberedSequence)
	params := store.CreateSSHTargetParams{
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
	}
	if template != nil {
		params.RemoteUsername = template.RemoteUsername
		params.AuthType = template.AuthType
		params.EncryptedSecret = template.EncryptedSecret
		params.ProxyTargetID = template.ProxyTargetID
		params.Tags = template.Tags
	}
	target, err := a.store.Repository().CreateSSHTarget(ctx, params)
	if err != nil {
		return store.SSHTarget{}, err
	}
	if template != nil {
		if err := a.store.Repository().CopyPolicyTargetBindings(ctx, template.ID, target.ID); err != nil {
			return store.SSHTarget{}, err
		}
		target, err = a.store.Repository().GetSSHTarget(ctx, target.ID)
	}
	return target, err
}

func firstAgentTargetForEnrollment(ctx context.Context, a *App, enrollment store.AgentEnrollment) *store.SSHTarget {
	agents, err := a.store.Repository().ListAgentsByEnrollment(ctx, enrollment.ID)
	if err != nil || len(agents) == 0 {
		return nil
	}
	agentIDs := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		agentIDs[agent.ID] = struct{}{}
	}
	targets, err := a.store.Repository().ListSSHTargets(ctx, enrollment.OwnerType, enrollment.OwnerID)
	if err != nil {
		return nil
	}
	for _, target := range targets {
		if target.TargetType == store.TargetAgent {
			if _, ok := agentIDs[target.AgentID]; ok {
				copy := target
				return &copy
			}
		}
	}
	return nil
}

func baseAgentAlias(enrollment store.AgentEnrollment, agent store.Agent) string {
	alias := strings.TrimSpace(enrollment.Label)
	if alias == "" && agent.ID != "" {
		alias = "agent-" + agent.ID[:8]
	}
	if alias == "" {
		alias = "agent"
	}
	return alias
}

func nextAgentAlias(base string, targets []store.SSHTarget, forceNumbered bool) string {
	used := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target.TargetType == store.TargetAgent {
			used[target.Alias] = struct{}{}
			used[target.Name] = struct{}{}
		}
	}
	if !forceNumbered {
		if _, ok := used[base]; !ok {
			return base
		}
	}
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s_%d", base, index)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

func numberedAliasBase(alias, fallback string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return fallback
	}
	index := strings.LastIndex(alias, "_")
	if index <= 0 || index == len(alias)-1 {
		return alias
	}
	for _, char := range alias[index+1:] {
		if char < '0' || char > '9' {
			return alias
		}
	}
	return alias[:index]
}

func publicBaseURL(r *http.Request, configuredHost string) string {
	scheme := "http"
	if isHTTPSRequest(r) {
		scheme = "https"
	}
	host := configuredHost
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return ""
	}
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return scheme + "://" + host
}

func (a *App) agentDownloadSHA256(goos, goarch string) (string, error) {
	name := "gosshd-agent"
	if goos == "windows" {
		name += ".exe"
	}
	if path, ok := a.localAgentPath(goos, goarch, name); ok {
		return fileSHA256(path)
	}
	cachePath := a.agentCachePath(goos, goarch, name)
	if _, err := os.Stat(cachePath); err == nil {
		return fileSHA256(cachePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if a.cfg.version() == DefaultVersion {
		return "", errors.New("agent binary is not cached")
	}
	return a.fetchReleaseChecksumWithProxy(a.releaseChecksumsURL(), a.agentAssetName(goos, goarch))
}

func requestOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	expectedHost := r.Host
	if expectedHost == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, expectedHost)
}
