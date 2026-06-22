package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrWeakPassword = errors.New("password must be at least 8 characters")

type Service struct {
	repo       *store.Repository
	sessionTTL time.Duration
}

func NewService(repo *store.Repository) *Service {
	return &Service{
		repo:       repo,
		sessionTTL: 30 * 24 * time.Hour,
	}
}

func (s *Service) Register(ctx context.Context, email, displayName, password string) (store.User, string, error) {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return store.User{}, "", ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return store.User{}, "", err
	}
	user, err := s.repo.CreateUser(ctx, store.CreateUserParams{
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hash,
	})
	if err != nil {
		return store.User{}, "", err
	}
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		return store.User{}, "", err
	}
	return user, token, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (store.User, string, error) {
	user, err := s.repo.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.User{}, "", ErrInvalidCredentials
		}
		return store.User{}, "", err
	}
	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)); err != nil {
		return store.User{}, "", ErrInvalidCredentials
	}
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		return store.User{}, "", err
	}
	return user, token, nil
}

func (s *Service) ResetPassword(ctx context.Context, userID, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.repo.UpdateUserPasswordHash(ctx, userID, hash)
}

func (s *Service) ChangePassword(ctx context.Context, user store.User, currentPassword, newPassword string) error {
	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}
	return s.ResetPassword(ctx, user.ID, newPassword)
}

func (s *Service) UserForSession(ctx context.Context, token string) (store.User, error) {
	session, err := s.repo.GetSessionByTokenHash(ctx, tokenHash(token))
	if err != nil {
		return store.User{}, err
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.repo.DeleteSessionByTokenHash(ctx, tokenHash(token))
		return store.User{}, store.ErrNotFound
	}
	return s.repo.GetUser(ctx, session.UserID)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.DeleteSessionByTokenHash(ctx, tokenHash(token))
}

func (s *Service) createSession(ctx context.Context, userID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.repo.CreateSession(ctx, userID, tokenHash(token), time.Now().UTC().Add(s.sessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
