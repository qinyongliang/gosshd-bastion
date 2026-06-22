package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type authRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newAuthRateLimiter() *authRateLimiter {
	return &authRateLimiter{attempts: map[string][]time.Time{}}
}

func (l *authRateLimiter) allow(key string, limit int, window time.Duration) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-window)
	l.mu.Lock()
	defer l.mu.Unlock()
	values := l.attempts[key]
	kept := values[:0]
	for _, item := range values {
		if item.After(cutoff) {
			kept = append(kept, item)
		}
	}
	if len(kept) >= limit {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, now)
	return true
}

func (a *App) allowAuthAttempt(r *http.Request, subject string, limit int, window time.Duration) bool {
	ip := remoteIP(r)
	if ip == "" {
		ip = "unknown"
	}
	return a.authLimiter.allow(ip+"|"+subject, limit, window)
}

func remoteIP(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); raw != "" {
		if first, _, ok := strings.Cut(raw, ","); ok {
			raw = first
		}
		return strings.TrimSpace(raw)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
