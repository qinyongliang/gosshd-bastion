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

	for _, path := range []string{"/", "/app.js", "/styles.css", "/targets"} {
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
			continue
		}
		if len(body) < 200 {
			t.Fatalf("%s asset too small", path)
		}
		if path == "/app.js" && (!strings.Contains(body, "systemctl") || !strings.Contains(body, "sc.exe")) {
			t.Fatalf("agent guide did not include startup service instructions")
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
