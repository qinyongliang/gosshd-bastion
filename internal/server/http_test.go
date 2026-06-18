package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnscopedAgentInstallRoutesAreNotRegistered(t *testing.T) {
	app := NewApp(Config{})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/run.sh", "/run.ps1", "/install.sh", "/install.ps1"} {
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
