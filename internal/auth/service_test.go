package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestAuthServiceRegistersAndAuthenticatesUser(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := NewService(st.Repository())
	registered, token, err := svc.Register(ctx, "Alice@Example.com", "Alice", "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if registered.ID == "" || token == "" {
		t.Fatalf("missing registered user id or token")
	}
	if registered.Email != "alice@example.com" {
		t.Fatalf("email not normalized: %q", registered.Email)
	}

	loggedIn, loginToken, err := svc.Login(ctx, "alice@example.com", "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if loggedIn.ID != registered.ID || loginToken == "" || loginToken == token {
		t.Fatalf("login returned unexpected user/token")
	}

	fromSession, err := svc.UserForSession(ctx, loginToken)
	if err != nil {
		t.Fatal(err)
	}
	if fromSession.ID != registered.ID {
		t.Fatalf("session user mismatch: got %s want %s", fromSession.ID, registered.ID)
	}
}

func TestAuthServiceRejectsBadPassword(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := NewService(st.Repository())
	if _, _, err := svc.Register(ctx, "bob@example.com", "Bob", "correct-pass"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(ctx, "bob@example.com", "wrong-pass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestDingTalkLoginCreatesUserAndIdentity(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()

	admin, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "admin@example.com", DisplayName: "Admin", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Ops", Slug: "ops", OwnerUserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	mock := newMockDingTalkServer(t, "union-1", "open-1", "Dora", "dora@example.com")
	svc := NewService(repo)
	cfg := DingTalkConfig{
		Enabled:             true,
		ClientID:            "app-key",
		ClientSecret:        "app-secret",
		AuthURL:             mock.URL + "/authorize",
		TokenURL:            mock.URL + "/token",
		UserInfoURL:         mock.URL + "/userinfo",
		RedirectURL:         "http://bastion.example.com/api/auth/dingtalk/callback",
		DefaultOrganization: org.ID,
		DefaultRole:         store.RoleMember,
	}
	startURL, err := svc.BuildDingTalkAuthURL(ctx, cfg, "/targets")
	if err != nil {
		t.Fatal(err)
	}
	state := queryParam(t, startURL, "state")
	user, token, err := svc.CompleteDingTalkLogin(ctx, cfg, "valid-code", state)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || user.Email != "dora@example.com" || user.DisplayName != "Dora" || user.AuthProvider != "dingtalk" {
		t.Fatalf("dingtalk-created user mismatch: user=%#v token=%q", user, token)
	}
	identity, err := repo.GetExternalIdentity(ctx, "dingtalk", "union-1")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != user.ID {
		t.Fatalf("identity user mismatch: %#v", identity)
	}
	member, err := repo.GetOrganizationMember(ctx, org.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != store.RoleMember {
		t.Fatalf("dingtalk user role mismatch: %#v", member)
	}
	defaultGroup, err := repo.GetDefaultOrganizationUserGroup(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	inGroup, err := repo.UserInGroup(ctx, defaultGroup.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !inGroup {
		t.Fatalf("dingtalk user missing from default group")
	}
}

func TestDingTalkLoginBindsExistingEmail(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	existing, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "dora@example.com", DisplayName: "Local Dora", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	mock := newMockDingTalkServer(t, "union-2", "open-2", "Dora Ding", "dora@example.com")
	svc := NewService(repo)
	cfg := DingTalkConfig{
		Enabled:      true,
		ClientID:     "app-key",
		ClientSecret: "app-secret",
		AuthURL:      mock.URL + "/authorize",
		TokenURL:     mock.URL + "/token",
		UserInfoURL:  mock.URL + "/userinfo",
		RedirectURL:  "http://bastion.example.com/api/auth/dingtalk/callback",
	}
	startURL, err := svc.BuildDingTalkAuthURL(ctx, cfg, "/")
	if err != nil {
		t.Fatal(err)
	}
	user, token, err := svc.CompleteDingTalkLogin(ctx, cfg, "valid-code", queryParam(t, startURL, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || user.ID != existing.ID || user.DisplayName != "Local Dora" {
		t.Fatalf("expected existing email user binding, got user=%#v token=%q", user, token)
	}
	identity, err := repo.GetExternalIdentity(ctx, "dingtalk", "union-2")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != existing.ID {
		t.Fatalf("identity should bind existing user: %#v", identity)
	}
}

func TestDingTalkLoginRejectsInvalidState(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mock := newMockDingTalkServer(t, "union-3", "open-3", "Nope", "nope@example.com")
	svc := NewService(st.Repository())
	cfg := DingTalkConfig{
		Enabled:      true,
		ClientID:     "app-key",
		ClientSecret: "app-secret",
		AuthURL:      mock.URL + "/authorize",
		TokenURL:     mock.URL + "/token",
		UserInfoURL:  mock.URL + "/userinfo",
		RedirectURL:  "http://bastion.example.com/api/auth/dingtalk/callback",
	}
	if _, _, err := svc.CompleteDingTalkLogin(ctx, cfg, "valid-code", "missing-state"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected invalid state to be rejected, got %v", err)
	}
}

func newMockDingTalkServer(t *testing.T, unionID, openID, name, email string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("code") != "valid-code" {
			http.Error(w, "bad code", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"expires_in":   int(time.Hour.Seconds()),
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-access-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unionid": unionID,
			"openid":  openID,
			"name":    name,
			"email":   email,
		})
	})
	return httptest.NewServer(mux)
}

func queryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	value := req.URL.Query().Get(key)
	if value == "" {
		t.Fatalf("missing query param %q in %s", key, rawURL)
	}
	return value
}
