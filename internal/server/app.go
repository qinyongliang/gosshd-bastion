package server

import (
	"context"
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
	auditRecordingsPath string
	initMu              sync.Mutex
	knownHostsMu        sync.Mutex
	backgroundWG        sync.WaitGroup
	httpSrv             *http.Server
	sshLn               net.Listener
}

func NewApp(cfg Config) *App {
	return &App{
		cfg:         cfg,
		registry:    NewAgentRegistry(),
		authLimiter: newAuthRateLimiter(),
	}
}

func (a *App) Registry() *AgentRegistry {
	return a.registry
}

func (a *App) Close() error {
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
	st, err := store.Open(ctx, a.cfg.DatabasePath)
	if err != nil {
		return err
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
	log.Printf("gosshd-server ready")
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

func (a *App) runtimeInfo(r *http.Request) apiRuntime {
	return apiRuntime{
		SSHHost: publicSSHHost(a.cfg.PublicHost, r.Host),
		SSHPort: publicSSHPort(a.cfg.PublicSSHPort, a.cfg.SSHListen),
	}
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
