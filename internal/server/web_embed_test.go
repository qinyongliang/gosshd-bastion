package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebAppServesIndexAndStaticAssets(t *testing.T) {
	app := NewApp(Config{})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{
		"/",
		"/main.js",
		"/i18n.js",
		"/theme.js",
		"/state.js",
		"/router.js",
		"/tag-colors.js",
		"/components/layout.js",
		"/components/management.js",
		"/views/auth.js",
		"/views/agents.js",
		"/views/system-admin.js",
		"/styles.css",
		"/targets",
	} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status mismatch: %d", path, resp.StatusCode)
		}
		if path == "/" || path == "/targets" {
			if !strings.Contains(body, "gosshd Bastion") {
				t.Fatalf("%s did not serve index: %s", path, body)
			}
			if path == "/" && !strings.Contains(body, "main.js") {
				t.Fatalf("%s did not load modular frontend: %s", path, body)
			}
			continue
		}
		if len(body) < 200 {
			t.Fatalf("%s asset too small", path)
		}
		if path == "/i18n.js" && (!strings.Contains(body, "gosshd_locale") || !strings.Contains(body, "zh-CN") || !strings.Contains(body, "systemctl") || !strings.Contains(body, "sc.exe")) {
			t.Fatalf("i18n module did not include locale persistence")
		}
		if path == "/theme.js" && (!strings.Contains(body, "gosshd_theme") || !strings.Contains(body, "light") || !strings.Contains(body, "dark")) {
			t.Fatalf("theme module did not include theme persistence")
		}
		if path == "/tag-colors.js" && (!strings.Contains(body, "TAG_COLORS") || !strings.Contains(body, "purple") || !strings.Contains(body, "tagColorClass")) {
			t.Fatalf("tag color module did not include fixed palette")
		}
		if path == "/views/auth.js" && !strings.Contains(body, "auth.dingTalk") {
			t.Fatalf("auth view did not include DingTalk login action")
		}
		if path == "/views/system-admin.js" && (!strings.Contains(body, "admin.identityTitle") || !strings.Contains(body, "admin.accountTitle") || !strings.Contains(body, "admin.orgTitle")) {
			t.Fatalf("system admin view did not include settings/users/org management")
		}
		if path == "/views/agents.js" && (!strings.Contains(body, "agents.linuxService") || !strings.Contains(body, "agents.windowsService")) {
			t.Fatalf("agent guide did not include startup service instructions")
		}
	}

	for _, path := range []string{"/app.js", "/components.js", "/missing-module.js", "/unknown-route"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s should be a strict 404, got %d", path, resp.StatusCode)
		}
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
