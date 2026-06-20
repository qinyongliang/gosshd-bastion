package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUIE2EWithBrowser(t *testing.T) {
	nodePath := os.Getenv("GOSSHD_UI_E2E_NODE")
	playwrightPath := os.Getenv("GOSSHD_UI_E2E_PLAYWRIGHT")
	browserPath := os.Getenv("GOSSHD_UI_E2E_BROWSER")
	if nodePath == "" || playwrightPath == "" || browserPath == "" {
		t.Fatalf("GOSSHD_UI_E2E_NODE, GOSSHD_UI_E2E_PLAYWRIGHT, and GOSSHD_UI_E2E_BROWSER are required")
	}
	app := NewApp(Config{
		DatabasePath:           filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName:      "ui_e2e_session",
		BootstrapAdminPassword: "admin-pass",
		PublicHost:             "127.0.0.1",
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})

	cmd := exec.CommandContext(context.Background(), nodePath, filepath.Join("web", "e2e", "ui_e2e.mjs"))
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"GOSSHD_UI_E2E_BASE_URL="+srv.URL,
		"PLAYWRIGHT_REQUIRE_PATH="+playwrightPath,
		"PLAYWRIGHT_CHROMIUM_EXECUTABLE="+browserPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ui e2e failed: %v\n%s", err, out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
