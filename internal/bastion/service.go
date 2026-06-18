package bastion

import (
	"bytes"
	"context"
	"strings"

	"github.com/qinyongliang/gosshd/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func NormalizeAuthorizedKey(raw string) (string, string, error) {
	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(strings.TrimSpace(raw)))
	if err != nil {
		return "", "", err
	}
	normalized := string(gossh.MarshalAuthorizedKey(key))
	normalized = string(bytes.TrimRight([]byte(normalized), "\n"))
	return normalized + "\n", gossh.FingerprintSHA256(key), nil
}

func (s *Service) NormalizeAuthorizedKey(raw string) (string, string, error) {
	return NormalizeAuthorizedKey(raw)
}

func (s *Service) LookupUserByPublicKey(ctx context.Context, key gossh.PublicKey) (store.User, error) {
	return s.repo.GetUserByPublicKeyFingerprint(ctx, gossh.FingerprintSHA256(key))
}
