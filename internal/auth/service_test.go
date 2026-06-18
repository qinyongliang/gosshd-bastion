package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
