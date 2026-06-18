package bastion

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"

	"github.com/qinyongliang/gosshd/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestPublicKeyFingerprintRoundTrip(t *testing.T) {
	signer := testSigner(t)
	raw := string(gossh.MarshalAuthorizedKey(signer.PublicKey()))

	normalized, fingerprint, err := NormalizeAuthorizedKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	if normalized != raw {
		t.Fatalf("normalized key mismatch")
	}
	if fingerprint != gossh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("fingerprint mismatch: got %s", fingerprint)
	}
}

func TestLookupUserByPublicKeyFingerprint(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()

	user, err := repo.CreateUser(ctx, store.CreateUserParams{
		Email:        "carol@example.com",
		DisplayName:  "Carol",
		PasswordHash: []byte("hash"),
	})
	if err != nil {
		t.Fatal(err)
	}
	signer := testSigner(t)
	raw := string(gossh.MarshalAuthorizedKey(signer.PublicKey()))
	normalized, fingerprint, err := NormalizeAuthorizedKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreatePublicKey(ctx, store.CreatePublicKeyParams{
		UserID:        user.ID,
		Name:          "workstation",
		AuthorizedKey: normalized,
		Fingerprint:   fingerprint,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repo)
	got, err := svc.LookupUserByPublicKey(ctx, signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != user.ID {
		t.Fatalf("user mismatch: got %s want %s", got.ID, user.ID)
	}
}

func testSigner(t *testing.T) gossh.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
