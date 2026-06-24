package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestClientModeMeUsesBuiltInUserWithoutCookie(t *testing.T) {
	app := NewApp(Config{
		ClientMode:        true,
		DatabasePath:      filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName: "gosshd_client_test_session",
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

	var me apiMeResponse
	getJSON(t, apiClient(t), srv.URL+"/api/me", http.StatusOK, &me)
	if me.User.Email != "user" || me.User.DisplayName != "user" {
		t.Fatalf("client mode user mismatch: %+v", me.User)
	}
	if me.User.IsSystemAdmin {
		t.Fatalf("client mode user should not be system admin: %+v", me.User)
	}
	if !me.Runtime.ClientMode {
		t.Fatalf("runtime should report client mode: %+v", me.Runtime)
	}
	if len(me.Organizations) != 1 || !me.Organizations[0].IsPersonal {
		t.Fatalf("client mode should expose one internal owner scope: %+v", me.Organizations)
	}
}

func TestNonClientModeMeStillRequiresCookie(t *testing.T) {
	app := NewApp(Config{
		DatabasePath:           filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName:      "gosshd_non_client_test_session",
		BootstrapAdminPassword: "admin-pass",
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

	getJSON(t, apiClient(t), srv.URL+"/api/me", http.StatusUnauthorized, nil)
}

func TestClientModeAutoLoginRequiresLoopbackRequest(t *testing.T) {
	app := NewApp(Config{
		ClientMode:        true,
		DatabasePath:      filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName: "gosshd_client_remote_test_session",
	})
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.RemoteAddr = net.JoinHostPort("203.0.113.10", "41234")
	rr := httptest.NewRecorder()

	app.requireUser(app.handleMe)(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("remote client mode request status mismatch: got %d want %d", rr.Code, http.StatusUnauthorized)
	}
}
