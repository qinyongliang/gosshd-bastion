package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/relay"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"github.com/gorilla/websocket"
)

func TestAgentWSReturnsVersionAndAgentDownloadURL(t *testing.T) {
	ctx := context.Background()
	app := NewApp(Config{Version: "v1.2.3", PublicHost: "relay.example.com", DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	token := createAgentEnrollmentForTest(t, ctx, app, "relay-agent")
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + protocol.WebSocketPath
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	conn := relay.NewWSConn(ws)

	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:              "11111111-1111-4111-8111-111111111111",
		EnrollmentToken: token,
		Version:         "v1.2.2",
		GOOS:            "linux",
		GOARCH:          "amd64",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ServerVersion != "v1.2.3" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if got, want := resp.AgentDownloadURL, "http://relay.example.com/download/agent/linux/amd64"; got != want {
		t.Fatalf("download URL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAgentWSDefaultsMissingPlatformToServerPlatform(t *testing.T) {
	ctx := context.Background()
	app := NewApp(Config{Version: "v1.2.3", DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	token := createAgentEnrollmentForTest(t, ctx, app, "platform-agent")
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + protocol.WebSocketPath
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	conn := relay.NewWSConn(ws)

	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:              "11111111-1111-4111-8111-111111111111",
		EnrollmentToken: token,
		Version:         "v1.2.2",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := "/download/agent/" + runtime.GOOS + "/" + runtime.GOARCH
	if !strings.HasSuffix(resp.AgentDownloadURL, wantSuffix) {
		t.Fatalf("download URL %q does not end with %q", resp.AgentDownloadURL, wantSuffix)
	}
}

func TestAgentWSEnrollmentCreatesPersistedAgent(t *testing.T) {
	ctx := context.Background()
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	if err := app.ensureServices(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "agent@example.com", DisplayName: "Agent", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := "enroll-token"
	enrollment, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash(token),
		Label:       "laptop",
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn := dialAgentWS(t, srv.URL)
	defer conn.Close()
	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:              "11111111-1111-4111-8111-111111111111",
		EnrollmentToken: token,
		GOOS:            "linux",
		GOARCH:          "amd64",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("agent rejected: %+v", resp)
	}
	agent, err := app.store.Repository().GetAgentByEnrollment(ctx, enrollment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID == "" || agent.OwnerID != personal.ID {
		t.Fatalf("persisted agent mismatch: %+v", agent)
	}
	if _, err := app.Registry().Get(agent.ID); err != nil {
		t.Fatalf("persisted agent not online: %v", err)
	}
	targets, err := app.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].TargetType != store.TargetAgent || targets[0].Alias != "laptop" || targets[0].AgentID != agent.ID {
		t.Fatalf("agent target mismatch: %+v", targets)
	}
}

func TestEnsureAgentTargetReplacesExistingAgentTargetWithSameAlias(t *testing.T) {
	ctx := context.Background()
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	if err := app.ensureServices(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "replace-agent@example.com", DisplayName: "Replace Agent", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstEnrollment, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash("first-token"),
		Label:       "laptop",
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	firstAgent, err := app.store.Repository().UpsertAgent(ctx, store.UpsertAgentParams{
		OwnerType:        firstEnrollment.OwnerType,
		OwnerID:          firstEnrollment.OwnerID,
		EnrollmentID:     firstEnrollment.ID,
		Label:            firstEnrollment.Label,
		CurrentRuntimeID: "11111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ensureAgentTarget(ctx, firstEnrollment, firstAgent); err != nil {
		t.Fatal(err)
	}
	secondEnrollment, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash("second-token"),
		Label:       "laptop",
		DefaultHost: "127.0.0.1",
		DefaultPort: 2222,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondAgent, err := app.store.Repository().UpsertAgent(ctx, store.UpsertAgentParams{
		OwnerType:        secondEnrollment.OwnerType,
		OwnerID:          secondEnrollment.OwnerID,
		EnrollmentID:     secondEnrollment.ID,
		Label:            secondEnrollment.Label,
		CurrentRuntimeID: "22222222-2222-4222-8222-222222222222",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ensureAgentTarget(ctx, secondEnrollment, secondAgent); err != nil {
		t.Fatal(err)
	}
	targets, err := app.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected replacement to reuse target, got %+v", targets)
	}
	if targets[0].AgentID != secondAgent.ID || targets[0].Alias != "laptop" || targets[0].Port != 2222 {
		t.Fatalf("replacement target mismatch: %+v", targets[0])
	}
}

func TestAgentWSRejectsMissingEnrollmentToken(t *testing.T) {
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn := dialAgentWS(t, srv.URL)
	defer conn.Close()
	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID: "11111111-1111-4111-8111-111111111111",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error != "enrollment token is required" {
		t.Fatalf("expected enrollment-token rejection, got %+v", resp)
	}
}

func TestAgentWSRejectsInvalidEnrollmentToken(t *testing.T) {
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn := dialAgentWS(t, srv.URL)
	defer conn.Close()
	if err := protocol.WriteJSONLine(conn, protocol.AgentHello{
		ID:              "11111111-1111-4111-8111-111111111111",
		EnrollmentToken: "missing",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatalf("expected rejection, got %+v", resp)
	}
}

func dialAgentWS(t *testing.T, serverURL string) *relay.WSConn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + protocol.WebSocketPath
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return relay.NewWSConn(ws)
}

func createAgentEnrollmentForTest(t *testing.T, ctx context.Context, app *App, label string) string {
	t.Helper()
	if err := app.ensureServices(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: label + "@example.com", DisplayName: label, PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := label + "-token"
	if _, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     org.ID,
		TokenHash:   codeHash(token),
		Label:       label,
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return token
}
