package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/auth"
	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type App struct {
	cfg                 Config
	registry            *AgentRegistry
	store               *store.Store
	audit               *store.AuditStore
	auth                *auth.Service
	authLimiter         *authRateLimiter
	bastion             *bastion.Service
	manualReviews       *manualReviewHub
	runningAudits       *runningAuditStore
	terminalSessions    *terminalSessionManager
	auditRecordingsPath string
	brandingCache       brandingSettings
	brandingCacheValid  bool
	brandingCacheMu     sync.RWMutex
	localAgentCancel    context.CancelFunc
	localTargetID       string
	initMu              sync.Mutex
	knownHostsMu        sync.Mutex
	backgroundWG        sync.WaitGroup
	httpSrv             *http.Server
	sshLn               net.Listener
}

func NewApp(cfg Config) *App {
	return &App{
		cfg:              cfg,
		registry:         NewAgentRegistry(),
		authLimiter:      newAuthRateLimiter(),
		manualReviews:    newManualReviewHub(),
		runningAudits:    newRunningAuditStore(),
		terminalSessions: newTerminalSessionManager(),
	}
}

func (a *App) Registry() *AgentRegistry {
	return a.registry
}

func (a *App) Close() error {
	if a.localAgentCancel != nil {
		a.localAgentCancel()
		a.localAgentCancel = nil
	}
	a.backgroundWG.Wait()
	a.initMu.Lock()
	defer a.initMu.Unlock()
	var err error
	if a.audit != nil {
		err = a.audit.Close()
		a.audit = nil
	}
	if a.store != nil {
		if closeErr := a.store.Close(); err == nil {
			err = closeErr
		}
		a.store = nil
	}
	a.auth = nil
	a.bastion = nil
	return err
}

func (a *App) ensureServices(ctx context.Context) error {
	a.initMu.Lock()
	defer a.initMu.Unlock()
	if a.store != nil {
		return nil
	}
	secretKey, err := a.loadSecretKey()
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, a.cfg.DatabasePath, secretKey)
	if err != nil {
		return err
	}
	if err := st.Repository().EncryptLegacySecrets(ctx); err != nil {
		_ = st.Close()
		return fmt.Errorf("encrypt legacy secrets: %w", err)
	}
	auditPath := a.auditDatabasePath()
	audit, err := store.OpenAudit(ctx, auditPath)
	if err != nil {
		_ = st.Close()
		return err
	}
	a.store = st
	a.audit = audit
	a.auditRecordingsPath = a.auditRecordingPath()
	a.auth = auth.NewService(st.Repository())
	a.bastion = bastion.NewService(st.Repository())
	if a.cfg.ClientMode {
		user, err := st.Repository().EnsureClientUser(ctx)
		if err != nil {
			return err
		}
		if err := a.ensureClientLocalAgent(ctx, user); err != nil {
			return err
		}
		log.Printf("client mode account ready: email=%s", user.Email)
		return nil
	}
	password := strings.TrimSpace(a.cfg.BootstrapAdminPassword)
	if password == "" {
		password = strings.TrimSpace(os.Getenv("GOSSHD_BOOTSTRAP_ADMIN_PASSWORD"))
	}
	if admin, createdPassword, err := st.Repository().EnsureBootstrapAdmin(ctx, password); err != nil {
		return err
	} else if createdPassword != "" {
		if password == "" {
			path, err := a.writeBootstrapPassword(createdPassword)
			if err != nil {
				return err
			}
			log.Printf("bootstrap admin account ready: email=%s password_file=%s", admin.Email, path)
		} else {
			log.Printf("bootstrap admin account ready: email=%s password=provided", admin.Email)
		}
	}
	return nil
}

// ResetUserPassword generates a strong temporary password for the user
// identified by email or ID, stores its hash, and returns the plaintext once.
// It is intended for the one-shot --reset-user-password CLI operation.
func (a *App) ResetUserPassword(ctx context.Context, identifier string) (string, error) {
	if err := a.ensureServices(ctx); err != nil {
		return "", err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", errors.New("user email or ID is required")
	}
	user, err := a.store.Repository().GetUserByEmail(ctx, identifier)
	if errors.Is(err, store.ErrNotFound) {
		user, err = a.store.Repository().GetUser(ctx, identifier)
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("user %q not found", identifier)
		}
		return "", err
	}
	password, err := generateTemporaryPassword()
	if err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	if err := a.auth.ResetPassword(ctx, user.ID, password); err != nil {
		return "", fmt.Errorf("reset password for %q: %w", user.Email, err)
	}
	return password, nil
}

func generateTemporaryPassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	const length = 16
	password := make([]byte, length)
	// Rejection sampling avoids modulo bias while keeping the output shell-safe.
	limit := byte(256 - (256 % len(alphabet)))
	for i := range password {
		for {
			var raw [1]byte
			if _, err := rand.Read(raw[:]); err != nil {
				return "", err
			}
			if raw[0] >= limit {
				continue
			}
			password[i] = alphabet[int(raw[0])%len(alphabet)]
			break
		}
	}
	return string(password), nil
}

func (a *App) loadSecretKey() ([]byte, error) {
	if key := strings.TrimSpace(a.cfg.SecretKey); key != "" {
		return []byte(key), nil
	}
	if path := strings.TrimSpace(a.cfg.SecretKeyPath); path != "" {
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read secret key: %w", err)
		}
		if len(strings.TrimSpace(string(key))) == 0 {
			return nil, errors.New("secret key file is empty")
		}
		return []byte(strings.TrimSpace(string(key))), nil
	}
	if key := strings.TrimSpace(os.Getenv("GOSSHD_SECRET_KEY")); key != "" {
		return []byte(key), nil
	}
	base := strings.TrimSpace(a.cfg.DatabasePath)
	if base == "" {
		base = "gosshd.db"
	}
	keyPath := base + ".secret-key"
	if key, err := os.ReadFile(keyPath); err == nil {
		if len(strings.TrimSpace(string(key))) == 0 {
			return nil, errors.New("generated secret key file is empty")
		}
		return []byte(strings.TrimSpace(string(key))), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read generated secret key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("write generated secret key: %w", err)
	}
	return key, nil
}

func (a *App) auditDatabasePath() string {
	if strings.TrimSpace(a.cfg.AuditDatabasePath) != "" {
		return a.cfg.AuditDatabasePath
	}
	base := strings.TrimSpace(a.cfg.DatabasePath)
	if base == "" {
		return "gosshd-audit.db"
	}
	dir := filepath.Dir(base)
	if dir == "." || dir == "" {
		return "gosshd-audit.db"
	}
	return filepath.Join(dir, "gosshd-audit.db")
}

func (a *App) auditRecordingPath() string {
	if strings.TrimSpace(a.cfg.AuditRecordingPath) != "" {
		return a.cfg.AuditRecordingPath
	}
	base := strings.TrimSpace(a.cfg.DatabasePath)
	if base == "" {
		return filepath.Join(".", "audit-recordings")
	}
	dir := filepath.Dir(base)
	if dir == "." || dir == "" {
		return filepath.Join(".", "audit-recordings")
	}
	return filepath.Join(dir, "audit-recordings")
}

func (a *App) knownHostsPath() string {
	if strings.TrimSpace(a.cfg.KnownHostsPath) != "" {
		return a.cfg.KnownHostsPath
	}
	base := strings.TrimSpace(a.cfg.DatabasePath)
	if base == "" {
		return "known_hosts"
	}
	dir := filepath.Dir(base)
	if dir == "." || dir == "" {
		return "known_hosts"
	}
	return filepath.Join(dir, "known_hosts")
}

func (a *App) writeBootstrapPassword(password string) (string, error) {
	base := strings.TrimSpace(a.cfg.DatabasePath)
	dir := "."
	if base != "" {
		dir = filepath.Dir(base)
	}
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "bootstrap-admin-password.txt")
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) sessionCookieName() string {
	if a.cfg.SessionCookieName != "" {
		return a.cfg.SessionCookieName
	}
	return "gosshd_session"
}

func (a *App) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	a.routes(mux)
	a.httpSrv = newHTTPServer(a.cfg.HTTPListen, mux)

	sshLn, err := net.Listen("tcp", a.cfg.SSHListen)
	if err != nil {
		return fmt.Errorf("listen ssh: %w", err)
	}
	a.sshLn = sshLn
	a.logStartupInstructions()

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := a.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := a.serveSSH(sshLn); err != nil && !errors.Is(err, net.ErrClosed) {
			errs <- err
		}
	}()

	select {
	case <-ctx.Done():
		_ = a.httpSrv.Shutdown(context.Background())
		_ = sshLn.Close()
		wg.Wait()
		return nil
	case err := <-errs:
		_ = a.httpSrv.Shutdown(context.Background())
		_ = sshLn.Close()
		wg.Wait()
		return err
	}
}

func (a *App) logStartupInstructions() {
	base := a.startupHTTPBase()
	if a.cfg.ClientMode {
		log.Printf("gosshd-server ready in client mode")
	} else {
		log.Printf("gosshd-server ready")
	}
	log.Printf("http listening on %s", a.cfg.HTTPListen)
	log.Printf("ssh listening on %s", a.cfg.SSHListen)
	log.Printf("health check: curl %s/healthz", base)
	log.Printf("create private-node install tokens in the web console: %s/targets", base)
}

func (a *App) startupHTTPBase() string {
	host := strings.TrimSpace(a.cfg.PublicHost)
	if host == "" {
		host = hostFromListenAddress(a.cfg.HTTPListen)
	}
	if host == "" {
		host = "<server-host>"
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	return "http://" + host
}

func hostFromListenAddress(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "<server-host>"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		if strings.HasPrefix(listen, ":") {
			return "<server-host>" + listen
		}
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return "<server-host>:" + port
	}
	if strings.Contains(host, ":") {
		return net.JoinHostPort(host, port)
	}
	return host + ":" + port
}

func (a *App) runtimeInfo(ctx context.Context, r *http.Request) (apiRuntime, error) {
	branding, err := a.loadBrandingSettings(ctx)
	if err != nil {
		return apiRuntime{}, err
	}
	return apiRuntime{
		SSHHost:               publicSSHHost(a.cfg.PublicHost, r.Host),
		SSHPort:               publicSSHPort(a.cfg.PublicSSHPort, a.cfg.SSHListen),
		ClientMode:            a.cfg.ClientMode,
		LocalTerminalTargetID: a.localTargetID,
		AppName:               branding.AppName,
		AppDescription:        branding.AppDescription,
		AppIcon:               branding.AppIcon,
	}, nil
}

func publicSSHHost(configuredHost, requestHost string) string {
	host := strings.TrimSpace(configuredHost)
	if host == "" {
		host = strings.TrimSpace(requestHost)
	}
	if host == "" {
		return "public-ip"
	}
	if parsed, err := url.Parse(host); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	host = strings.Trim(host, "/")
	if before, _, ok := strings.Cut(host, "/"); ok {
		host = before
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil && parsedHost != "" {
		host = parsedHost
	}
	return strings.Trim(host, "[]")
}

func publicSSHPort(configured int, listen string) int {
	if configured > 0 {
		return configured
	}
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return 22
	}
	if _, port, err := net.SplitHostPort(listen); err == nil {
		if parsed, err := strconv.Atoi(port); err == nil && parsed > 0 {
			return parsed
		}
	}
	if strings.HasPrefix(listen, ":") {
		if parsed, err := strconv.Atoi(strings.TrimPrefix(listen, ":")); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 22
}

func (a *App) RunListeners(ctx context.Context, httpLn net.Listener, sshLn net.Listener) error {
	mux := http.NewServeMux()
	a.routes(mux)
	a.httpSrv = newHTTPServer("", mux)
	a.sshLn = sshLn

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := a.httpSrv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := a.serveSSH(sshLn); err != nil && !errors.Is(err, net.ErrClosed) {
			errs <- err
		}
	}()

	select {
	case <-ctx.Done():
		_ = a.httpSrv.Shutdown(context.Background())
		_ = sshLn.Close()
		wg.Wait()
		return nil
	case err := <-errs:
		_ = a.httpSrv.Shutdown(context.Background())
		_ = sshLn.Close()
		wg.Wait()
		return err
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
