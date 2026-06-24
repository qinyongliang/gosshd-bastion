package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/server"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx          context.Context
	cancel       context.CancelFunc
	backendURL   string
	backend      *server.App
	childWindow  bool
	initialPath  string
	windowTitle  string
	windowName   string
	windowWidth  int
	windowHeight int
}

func NewApp() *App {
	app := &App{
		initialPath:  "/local-terminal",
		windowTitle:  "GOSSHD",
		windowWidth:  1280,
		windowHeight: 820,
	}
	app.parseArgs(os.Args[1:])
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	wailsRuntime.WindowSetSystemDefaultTheme(ctx)
	if a.windowTitle != "" {
		wailsRuntime.WindowSetTitle(ctx, a.windowTitle)
	}
	if a.childWindow {
		return
	}

	appCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Errorf("listen local HTTP: %w", err))
	}
	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = httpLn.Close()
		panic(fmt.Errorf("listen local SSH: %w", err))
	}

	httpAddr := httpLn.Addr().String()
	_, sshPort, err := net.SplitHostPort(sshLn.Addr().String())
	if err != nil {
		_ = httpLn.Close()
		_ = sshLn.Close()
		panic(fmt.Errorf("parse local SSH port: %w", err))
	}
	publicSSHPort, err := strconv.Atoi(sshPort)
	if err != nil {
		_ = httpLn.Close()
		_ = sshLn.Close()
		panic(fmt.Errorf("parse local SSH port: %w", err))
	}

	dataDir := clientDataDir()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		_ = httpLn.Close()
		_ = sshLn.Close()
		panic(fmt.Errorf("create data directory: %w", err))
	}

	a.backendURL = "http://" + httpAddr
	a.backend = server.NewApp(server.Config{
		ClientMode:         true,
		HTTPListen:         httpAddr,
		SSHListen:          sshLn.Addr().String(),
		DatabasePath:       filepath.Join(dataDir, "gosshd.db"),
		AuditDatabasePath:  filepath.Join(dataDir, "gosshd-audit.db"),
		AuditRecordingPath: filepath.Join(dataDir, "audit-recordings"),
		HostKeyPath:        filepath.Join(dataDir, "gosshd_host_key"),
		KnownHostsPath:     filepath.Join(dataDir, "known_hosts"),
		PublicHost:         httpAddr,
		PublicSSHPort:      publicSSHPort,
		SessionCookieName:  "gosshd_client_session",
	})

	go func() {
		if err := a.backend.RunListeners(appCtx, httpLn, sshLn); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			_ = os.WriteFile(filepath.Join(dataDir, "gosshd-client-error.log"), []byte(time.Now().Format(time.RFC3339)+" "+err.Error()+"\n"), 0o600)
		}
	}()
}

func (a *App) shutdown(_ context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.backend != nil {
		_ = a.backend.Close()
	}
}

func (a *App) BackendURL() string {
	return a.backendURL
}

func (a *App) InitialPath() string {
	return a.initialPath
}

func (a *App) IsChildWindow() bool {
	return a.childWindow
}

func (a *App) OpenClientWindow(path string, title string, name string, width int, height int) error {
	path = cleanClientPath(path)
	if strings.TrimSpace(title) == "" {
		title = "GOSSHD"
	}
	if width <= 0 {
		width = 1100
	}
	if height <= 0 {
		height = 760
	}
	if strings.TrimSpace(a.backendURL) == "" {
		return fmt.Errorf("backend URL is empty")
	}
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{
		"--client-child-window",
		"--backend-url", a.backendURL,
		"--initial-path", path,
		"--window-title", title,
		"--window-width", strconv.Itoa(width),
		"--window-height", strconv.Itoa(height),
	}
	if strings.TrimSpace(name) != "" {
		args = append(args, "--window-name", name)
	}
	cmd := exec.Command(exePath, args...)
	configureClientWindowCommand(cmd)
	return cmd.Start()
}

func (a *App) WaitForBackend(timeoutMS int) error {
	if strings.TrimSpace(a.backendURL) == "" {
		return fmt.Errorf("backend URL is empty")
	}
	if timeoutMS <= 0 {
		timeoutMS = 15000
	}

	deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(a.backendURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("healthz HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("backend did not become ready: %w", lastErr)
	}
	return fmt.Errorf("backend did not become ready")
}

func clientDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = "."
	}
	return filepath.Join(base, "GOSSHD", "Client")
}

func (a *App) parseArgs(args []string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--client-child-window":
			a.childWindow = true
		case "--backend-url":
			i++
			if i < len(args) {
				a.backendURL = strings.TrimRight(args[i], "/")
			}
		case "--initial-path":
			i++
			if i < len(args) {
				a.initialPath = cleanClientPath(args[i])
			}
		case "--window-title":
			i++
			if i < len(args) {
				a.windowTitle = strings.TrimSpace(args[i])
			}
		case "--window-width":
			i++
			if i < len(args) {
				if width, err := strconv.Atoi(args[i]); err == nil && width > 0 {
					a.windowWidth = width
				}
			}
		case "--window-height":
			i++
			if i < len(args) {
				if height, err := strconv.Atoi(args[i]); err == nil && height > 0 {
					a.windowHeight = height
				}
			}
		case "--window-name":
			i++
			if i < len(args) {
				a.windowName = sanitizeWindowName(args[i])
			}
		}
	}
}

func cleanClientPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/local-terminal"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(path, "//") || strings.Contains(path, "://") {
		return "/local-terminal"
	}
	return path
}

func (a *App) webviewDataPath() string {
	name := "main"
	if a.childWindow {
		name = a.windowName
		if name == "" {
			name = "window-" + strconv.Itoa(os.Getpid())
		}
	}
	return filepath.Join(clientDataDir(), "webview2", name)
}

func sanitizeWindowName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var builder strings.Builder
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}
