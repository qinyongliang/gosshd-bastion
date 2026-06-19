package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/qinyongliang/gosshd-bastion/internal/auth"
	"github.com/qinyongliang/gosshd-bastion/internal/bastion"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type App struct {
	cfg      Config
	registry *AgentRegistry
	store    *store.Store
	auth     *auth.Service
	bastion  *bastion.Service
	initMu   sync.Mutex
	httpSrv  *http.Server
	sshLn    net.Listener
}

func NewApp(cfg Config) *App {
	return &App{
		cfg:      cfg,
		registry: NewAgentRegistry(),
	}
}

func (a *App) Registry() *AgentRegistry {
	return a.registry
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
	a.store = st
	a.auth = auth.NewService(st.Repository())
	a.bastion = bastion.NewService(st.Repository())
	password := strings.TrimSpace(a.cfg.BootstrapAdminPassword)
	if password == "" {
		password = strings.TrimSpace(os.Getenv("GOSSHD_BOOTSTRAP_ADMIN_PASSWORD"))
	}
	if admin, createdPassword, err := st.Repository().EnsureBootstrapAdmin(ctx, password); err != nil {
		return err
	} else if createdPassword != "" {
		log.Printf("bootstrap admin account ready: email=%s password=%s", admin.Email, createdPassword)
	}
	return nil
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
	a.httpSrv = &http.Server{Addr: a.cfg.HTTPListen, Handler: mux}

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

func (a *App) RunListeners(ctx context.Context, httpLn net.Listener, sshLn net.Listener) error {
	mux := http.NewServeMux()
	a.routes(mux)
	a.httpSrv = &http.Server{Handler: mux}
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
