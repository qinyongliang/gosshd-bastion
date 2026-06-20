package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestWebAppServesIndexAndStaticAssets(t *testing.T) {
	app := NewApp(Config{})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	indexBody := ""
	for _, path := range []string{"/", "/targets"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status mismatch: %d", path, resp.StatusCode)
		}
		if !strings.Contains(body, "gosshd Bastion") || !strings.Contains(body, `id="root"`) || !strings.Contains(body, `type="module"`) {
			t.Fatalf("%s did not serve React/Vite index: %s", path, body)
		}
		if path == "/" {
			indexBody = body
		}
	}

	for _, path := range viteAssetPaths(t, indexBody) {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status mismatch: %d", path, resp.StatusCode)
		}
		if len(body) < 200 {
			t.Fatalf("%s asset too small", path)
		}
	}

	for _, path := range []string{"/app.js", "/main.js", "/i18n.js", "/components.js", "/views/auth.js", "/missing-module.js", "/unknown-route"} {
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

func viteAssetPaths(t *testing.T, indexBody string) []string {
	t.Helper()
	matches := regexp.MustCompile(`(?:src|href)="/([^"]+\.(?:js|css))"`).FindAllStringSubmatch(indexBody, -1)
	if len(matches) == 0 {
		t.Fatalf("index did not reference Vite assets: %s", indexBody)
	}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, "/"+match[1])
	}
	return paths
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
