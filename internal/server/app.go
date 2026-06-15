package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

type App struct {
	cfg      Config
	registry *AgentRegistry
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
	log.Printf("start Linux/macOS agent: curl %s/run.sh | sh", base)
	log.Printf("start Windows agent: irm %s/run.ps1 | iex", base)
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
