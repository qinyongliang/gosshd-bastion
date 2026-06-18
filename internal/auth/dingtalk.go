package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"golang.org/x/crypto/bcrypt"
)

const dingTalkProvider = "dingtalk"

type DingTalkConfig struct {
	Enabled             bool
	ClientID            string
	ClientSecret        string
	AuthURL             string
	TokenURL            string
	UserInfoURL         string
	RedirectURL         string
	DefaultOrganization string
	DefaultRole         string
}

type DingTalkUserInfo struct {
	UnionID string `json:"unionid"`
	OpenID  string `json:"openid"`
	UserID  string `json:"userid"`
	Name    string `json:"name"`
	Nick    string `json:"nick"`
	Email   string `json:"email"`
}

type dingTalkTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (s *Service) BuildDingTalkAuthURL(ctx context.Context, cfg DingTalkConfig, redirectAfter string) (string, error) {
	if err := cfg.validateForStart(); err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.repo.CreateOAuthState(ctx, dingTalkProvider, stateHash(state), redirectAfter, time.Now().UTC().Add(10*time.Minute)); err != nil {
		return "", err
	}
	u, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", strings.TrimSpace(cfg.ClientID))
	q.Set("redirect_uri", strings.TrimSpace(cfg.RedirectURL))
	q.Set("response_type", "code")
	q.Set("scope", "openid")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *Service) CompleteDingTalkLogin(ctx context.Context, cfg DingTalkConfig, code, state string) (store.User, string, error) {
	if err := cfg.validateForCallback(); err != nil {
		return store.User{}, "", err
	}
	if _, err := s.repo.ConsumeOAuthState(ctx, dingTalkProvider, stateHash(state)); err != nil {
		return store.User{}, "", err
	}
	token, err := exchangeDingTalkCode(ctx, cfg, code)
	if err != nil {
		return store.User{}, "", err
	}
	info, rawProfile, err := fetchDingTalkUserInfo(ctx, cfg, token.AccessToken)
	if err != nil {
		return store.User{}, "", err
	}
	subject := info.Subject()
	if subject == "" {
		return store.User{}, "", errors.New("dingtalk userinfo missing stable subject")
	}
	user, err := s.userForDingTalkIdentity(ctx, cfg, info, rawProfile, subject)
	if err != nil {
		return store.User{}, "", err
	}
	tokenValue, err := s.createSession(ctx, user.ID)
	if err != nil {
		return store.User{}, "", err
	}
	return user, tokenValue, nil
}

func (s *Service) userForDingTalkIdentity(ctx context.Context, cfg DingTalkConfig, info DingTalkUserInfo, rawProfile string, subject string) (store.User, error) {
	if identity, err := s.repo.GetExternalIdentity(ctx, dingTalkProvider, subject); err == nil {
		return s.repo.GetUser(ctx, identity.UserID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}

	email := strings.ToLower(strings.TrimSpace(info.Email))
	if email == "" {
		email = subject + "@dingtalk.local"
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Nick)
	}
	if displayName == "" {
		displayName = email
	}

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return store.User{}, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(randomExternalPassword(subject)), bcrypt.DefaultCost)
		if err != nil {
			return store.User{}, err
		}
		user, err = s.repo.CreateUser(ctx, store.CreateUserParams{
			Email:        email,
			DisplayName:  displayName,
			PasswordHash: hash,
			AuthProvider: dingTalkProvider,
		})
		if err != nil {
			return store.User{}, err
		}
	}

	if _, err := s.repo.CreateExternalIdentity(ctx, store.CreateExternalIdentityParams{
		UserID:         user.ID,
		Provider:       dingTalkProvider,
		Subject:        subject,
		Email:          email,
		DisplayName:    displayName,
		RawProfileJSON: rawProfile,
	}); err != nil {
		return store.User{}, err
	}
	if strings.TrimSpace(cfg.DefaultOrganization) != "" {
		role := strings.TrimSpace(cfg.DefaultRole)
		if role == "" {
			role = store.RoleMember
		}
		if err := s.repo.AddOrganizationMember(ctx, cfg.DefaultOrganization, user.ID, role); err != nil {
			return store.User{}, err
		}
	}
	return user, nil
}

func exchangeDingTalkCode(ctx context.Context, cfg DingTalkConfig, code string) (dingTalkTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("client_id", strings.TrimSpace(cfg.ClientID))
	form.Set("client_secret", strings.TrimSpace(cfg.ClientSecret))
	form.Set("redirect_uri", strings.TrimSpace(cfg.RedirectURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return dingTalkTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return dingTalkTokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return dingTalkTokenResponse{}, fmt.Errorf("dingtalk token exchange failed: %s %s", resp.Status, string(body))
	}
	var out dingTalkTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return dingTalkTokenResponse{}, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return dingTalkTokenResponse{}, errors.New("dingtalk token response missing access_token")
	}
	return out, nil
}

func fetchDingTalkUserInfo(ctx context.Context, cfg DingTalkConfig, accessToken string) (DingTalkUserInfo, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.UserInfoURL, nil)
	if err != nil {
		return DingTalkUserInfo{}, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return DingTalkUserInfo{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return DingTalkUserInfo{}, "", fmt.Errorf("dingtalk userinfo failed: %s %s", resp.Status, string(body))
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(resp.Body, 1<<20)); err != nil {
		return DingTalkUserInfo{}, "", err
	}
	var info DingTalkUserInfo
	if err := json.Unmarshal(buf.Bytes(), &info); err != nil {
		return DingTalkUserInfo{}, "", err
	}
	return info, buf.String(), nil
}

func (cfg DingTalkConfig) validateForStart() error {
	if !cfg.Enabled {
		return errors.New("dingtalk login is disabled")
	}
	for name, value := range map[string]string{
		"client_id":    cfg.ClientID,
		"auth_url":     cfg.AuthURL,
		"redirect_url": cfg.RedirectURL,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("dingtalk %s is required", name)
		}
	}
	return nil
}

func (cfg DingTalkConfig) validateForCallback() error {
	if err := cfg.validateForStart(); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"client_secret": cfg.ClientSecret,
		"token_url":     cfg.TokenURL,
		"userinfo_url":  cfg.UserInfoURL,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("dingtalk %s is required", name)
		}
	}
	return nil
}

func (info DingTalkUserInfo) Subject() string {
	for _, value := range []string{info.UnionID, info.OpenID, info.UserID} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func randomExternalPassword(subject string) string {
	sum := sha256.Sum256([]byte("dingtalk:" + subject + ":" + time.Now().UTC().String()))
	return fmt.Sprintf("%x", sum[:])
}

func stateHash(state string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(state)))
	return sum[:]
}
