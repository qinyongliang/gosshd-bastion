package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadJSONRejectsOversizedBody(t *testing.T) {
	body := `{"value":"` + strings.Repeat("x", 4<<20) + `"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	var value map[string]string
	if err := readJSON(req, &value); err == nil {
		t.Fatal("expected oversized JSON body to be rejected")
	}
}

func TestAuthLimiterEnforcesSubjectAcrossSourceAddresses(t *testing.T) {
	app := NewApp(Config{})
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		if !app.allowAuthAttempt(req, "login:admin", 20, 5*time.Minute) {
			t.Fatalf("attempt %d should be accepted", i)
		}
	}
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
	req.RemoteAddr = "192.0.2.250:1234"
	if app.allowAuthAttempt(req, "login:admin", 20, 5*time.Minute) {
		t.Fatal("subject limit should reject a new source address")
	}
}
