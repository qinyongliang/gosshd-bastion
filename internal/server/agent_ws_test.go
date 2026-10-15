package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
	"github.com/qinyongliang/gosshd-bastion/internal/relay"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
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
	if resp.AssignedAgentID != agent.ID || resp.TargetID != targets[0].ID || resp.TargetAlias != targets[0].Alias {
		t.Fatalf("assignment response mismatch: resp=%+v agent=%+v target=%+v", resp, agent, targets[0])
	}
}

func TestAgentWSRepeatedEnrollmentCreatesNumberedTargetAndCopiesConfig(t *testing.T) {
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
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "clone@example.com", DisplayName: "Clone", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := "same-token"
	if _, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash(token),
		Label:       "tmp",
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	firstAgent, firstTarget := enrollAgentForTest(t, ctx, app, token, protocol.AgentHello{
		ID:   "11111111-1111-4111-8111-111111111111",
		GOOS: "linux", GOARCH: "amd64",
	})
	if _, err := app.store.Repository().UpdateSSHTarget(ctx, firstTarget.ID, store.UpdateSSHTargetParams{
		Name:           "tmp_1",
		Alias:          "tmp_1",
		RemoteUsername: "deploy",
		AuthType:       store.AuthPrivateKey,
		ReplaceTags:    true,
		Tags:           []string{"prod", "db"},
	}); err != nil {
		t.Fatal(err)
	}
	policy, err := app.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType:                  store.OwnerOrganization,
		OwnerID:                    personal.ID,
		Name:                       "allow tmp",
		DefaultAction:              store.DecisionAllow,
		AllowSSHInteractive:        true,
		AllowWebTerminal:           true,
		ManualReviewTimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().AttachPolicyToTarget(ctx, policy.ID, firstTarget.ID); err != nil {
		t.Fatal(err)
	}

	secondAgent, secondTarget := enrollAgentForTest(t, ctx, app, token, protocol.AgentHello{
		ID:   "22222222-2222-4222-8222-222222222222",
		GOOS: "linux", GOARCH: "amd64",
	})
	if firstAgent.ID == secondAgent.ID {
		t.Fatalf("expected repeated token to create a new agent, first=%s second=%s", firstAgent.ID, secondAgent.ID)
	}
	if secondTarget.Alias != "tmp_2" || secondTarget.Name != "tmp_2" {
		t.Fatalf("expected numbered target alias/name tmp_2, got %+v", secondTarget)
	}
	if secondTarget.RemoteUsername != "deploy" || secondTarget.AuthType != store.AuthPrivateKey {
		t.Fatalf("target config was not copied: %+v", secondTarget)
	}
	if !hasTags(secondTarget.Tags, "prod", "db") {
		t.Fatalf("target tags were not copied: %+v", secondTarget.Tags)
	}
	policies, err := app.store.Repository().ListPoliciesForTarget(ctx, secondTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].ID != policy.ID {
		t.Fatalf("policy binding was not copied: %+v", policies)
	}
}

func TestAgentWSClonedAssignedAgentGetsNewAssignmentWithoutKickingOriginal(t *testing.T) {
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
	user, err := app.store.Repository().CreateUser(ctx, store.CreateUserParams{Email: "image@example.com", DisplayName: "Image", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := app.store.Repository().GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := "image-token"
	if _, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     personal.ID,
		TokenHash:   codeHash(token),
		Label:       "tmp",
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	firstIDFile := filepath.Join(t.TempDir(), "agent.json")
	firstClient, err := agent.New(agent.Config{Server: srv.URL, EnrollmentToken: token, IDFile: firstIDFile})
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	go func() {
		if err := firstClient.Run(firstCtx); err != nil {
			t.Logf("first agent stopped: %v", err)
		}
	}()
	firstAssignment := waitForAssignedAgentID(t, firstIDFile)
	if _, err := app.Registry().Get(firstAssignment.AssignedAgentID); err != nil {
		t.Fatalf("first agent should be online: %v", err)
	}

	secondIDFile := filepath.Join(t.TempDir(), "agent.json")
	data, err := os.ReadFile(firstIDFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondIDFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cloneID, err := protocol.LoadOrCreateAgentIDFile(secondIDFile)
	if err != nil {
		t.Fatal(err)
	}
	clone := dialAgentWS(t, srv.URL)
	defer clone.Close()
	response := writeAgentHelloForTest(t, clone, token, protocol.AgentHello{
		ID: cloneID.ID, AssignedAgentID: cloneID.AssignedAgentID,
		InstanceID: "22222222-2222-4222-8222-222222222222",
		BootID:     "33333333-3333-4333-8333-333333333333",
	})
	if !response.OK || response.AssignedAgentID == firstAssignment.AssignedAgentID {
		t.Fatalf("distinct live boot should receive a separate assignment: %+v", response)
	}
	cloneSession, err := yamux.Client(clone, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cloneSession.Close()
	secondAssignment := protocol.AgentIDFile{AssignedAgentID: response.AssignedAgentID}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if _, err := app.registry.Get(secondAssignment.AssignedAgentID); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if _, err := app.Registry().Get(firstAssignment.AssignedAgentID); err != nil {
		t.Fatalf("original agent should remain online: %v", err)
	}
	if _, err := app.Registry().Get(secondAssignment.AssignedAgentID); err != nil {
		t.Fatalf("clone agent should be online: %v", err)
	}
	targets, err := app.store.Repository().ListSSHTargets(ctx, store.OwnerOrganization, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected two targets after cloned assignment, got %+v", targets)
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
	if _, err := app.ensureAgentTarget(ctx, firstEnrollment, firstAgent, false); err != nil {
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
	if _, err := app.ensureAgentTarget(ctx, secondEnrollment, secondAgent, false); err != nil {
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

func TestAgentWSExpiredEnrollmentOnlyAllowsExistingAgentReconnect(t *testing.T) {
	ctx := context.Background()
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	token, enrollment := createAgentEnrollmentWithExpiryForTest(t, ctx, app, "expired-agent", time.Now().Add(-time.Hour))
	agent, err := app.store.Repository().UpsertAgent(ctx, store.UpsertAgentParams{
		OwnerType:        enrollment.OwnerType,
		OwnerID:          enrollment.OwnerID,
		EnrollmentID:     enrollment.ID,
		Label:            enrollment.Label,
		CurrentRuntimeID: "11111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ensureAgentTarget(ctx, enrollment, agent, false); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	newAgent := dialAgentWS(t, srv.URL)
	defer newAgent.Close()
	resp := writeAgentHelloForTest(t, newAgent, token, protocol.AgentHello{ID: "22222222-2222-4222-8222-222222222222"})
	if resp.OK || resp.Error != "invalid enrollment token" {
		t.Fatalf("expired enrollment should reject first registration: %+v", resp)
	}
	unknownAssignment := dialAgentWS(t, srv.URL)
	defer unknownAssignment.Close()
	resp = writeAgentHelloForTest(t, unknownAssignment, token, protocol.AgentHello{
		ID:              "22222222-2222-4222-8222-222222222222",
		AssignedAgentID: "99999999-9999-4999-8999-999999999999",
	})
	if resp.OK || resp.Error != "invalid enrollment token" {
		t.Fatalf("expired enrollment should reject an unknown assignment: %+v", resp)
	}

	reconnectingAgent := dialAgentWS(t, srv.URL)
	defer reconnectingAgent.Close()
	resp = writeAgentHelloForTest(t, reconnectingAgent, token, protocol.AgentHello{
		ID:              "33333333-3333-4333-8333-333333333333",
		AssignedAgentID: agent.ID,
	})
	if !resp.OK || resp.AssignedAgentID != agent.ID {
		t.Fatalf("expired enrollment should allow its existing agent to reconnect: %+v", resp)
	}

	liveMux, err := yamux.Client(reconnectingAgent, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer liveMux.Close()

	onlineClone := dialAgentWS(t, srv.URL)
	defer onlineClone.Close()
	resp = writeAgentHelloForTest(t, onlineClone, token, protocol.AgentHello{
		ID:              "44444444-4444-4444-8444-444444444444",
		AssignedAgentID: agent.ID,
	})
	if resp.OK || resp.Error != "invalid enrollment token" {
		t.Fatalf("expired enrollment should not clone an online assignment: %+v", resp)
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

func enrollAgentForTest(t *testing.T, ctx context.Context, app *App, token string, hello protocol.AgentHello) (store.Agent, store.SSHTarget) {
	t.Helper()
	hello.EnrollmentToken = token
	enrollment, err := app.store.Repository().GetAgentEnrollmentByTokenHash(ctx, codeHash(token))
	if err != nil {
		t.Fatal(err)
	}
	agent, target, err := app.enrollAgentConnection(ctx, enrollment, hello)
	if err != nil {
		t.Fatal(err)
	}
	return agent, target
}

func writeAgentHelloForTest(t *testing.T, conn *relay.WSConn, token string, hello protocol.AgentHello) protocol.StreamResponse {
	t.Helper()
	hello.EnrollmentToken = token
	if hello.GOOS == "" {
		hello.GOOS = "linux"
	}
	if hello.GOARCH == "" {
		hello.GOARCH = "amd64"
	}
	if err := protocol.WriteJSONLine(conn, hello); err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ReadJSONLine[protocol.StreamResponse](bufio.NewReader(conn))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func hasTags(tags []string, want ...string) bool {
	got := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		got[tag] = struct{}{}
	}
	for _, tag := range want {
		if _, ok := got[tag]; !ok {
			return false
		}
	}
	return len(got) == len(want)
}

func waitForAssignedAgentID(t *testing.T, path string) protocol.AgentIDFile {
	t.Helper()
	return waitForAssignedAgentIDChange(t, path, "")
}

func waitForAssignedAgentIDChange(t *testing.T, path, previous string) protocol.AgentIDFile {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var stored protocol.AgentIDFile
			if json.Unmarshal(data, &stored) == nil && stored.AssignedAgentID != "" && stored.AssignedAgentID != previous {
				return stored
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("assigned agent id was not written to %s", path)
	return protocol.AgentIDFile{}
}

func createAgentEnrollmentForTest(t *testing.T, ctx context.Context, app *App, label string) string {
	t.Helper()
	token, _ := createAgentEnrollmentWithExpiryForTest(t, ctx, app, label, time.Now().Add(time.Hour))
	return token
}

func createAgentEnrollmentWithExpiryForTest(t *testing.T, ctx context.Context, app *App, label string, expiresAt time.Time) (string, store.AgentEnrollment) {
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
	enrollment, err := app.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
		OwnerType:   store.OwnerOrganization,
		OwnerID:     org.ID,
		TokenHash:   codeHash(token),
		Label:       label,
		DefaultHost: "127.0.0.1",
		DefaultPort: 22,
		CreatedBy:   user.ID,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token, enrollment
}

func TestAgentWSExpiredEnrollmentAllowsSameInstanceOverlapOnly(t *testing.T) {
	ctx := context.Background()
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	token, enrollment := createAgentEnrollmentWithExpiryForTest(t, ctx, app, "expired-overlap", time.Now().Add(-time.Hour))
	agent, err := app.store.Repository().UpsertAgent(ctx, store.UpsertAgentParams{OwnerType: enrollment.OwnerType, OwnerID: enrollment.OwnerID, EnrollmentID: enrollment.ID, Label: enrollment.Label, CurrentRuntimeID: "11111111-1111-4111-8111-111111111111"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ensureAgentTarget(ctx, enrollment, agent, false); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hello := protocol.AgentHello{ID: agent.CurrentRuntimeID, AssignedAgentID: agent.ID, InstanceID: "22222222-2222-4222-8222-222222222222", BootID: "66666666-6666-4666-8666-666666666666"}
	first := dialAgentWS(t, srv.URL)
	defer first.Close()
	firstResponse := writeAgentHelloForTest(t, first, token, hello)
	firstMux, err := yamux.Client(first, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer firstMux.Close()
	if !firstResponse.OK {
		t.Fatalf("offline existing agent: %+v", firstResponse)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if _, err := app.registry.Get(agent.ID); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	resumed := dialAgentWS(t, srv.URL)
	defer resumed.Close()
	response := writeAgentHelloForTest(t, resumed, token, hello)
	if !response.OK || response.AssignedAgentID != agent.ID || response.TargetID != firstResponse.TargetID {
		t.Fatalf("expired-token overlapping reconnect changed assignment: %+v", response)
	}
	resumedMux, err := yamux.Client(resumed, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resumedMux.Close()
	for _, changed := range []protocol.AgentHello{
		{ID: hello.ID, AssignedAgentID: agent.ID, InstanceID: "33333333-3333-4333-8333-333333333333"},
		{ID: "44444444-4444-4444-8444-444444444444", AssignedAgentID: agent.ID, InstanceID: hello.InstanceID},
		{ID: hello.ID, AssignedAgentID: agent.ID},
		{ID: hello.ID, AssignedAgentID: agent.ID, InstanceID: hello.InstanceID, BootID: "77777777-7777-4777-8777-777777777777"},
	} {
		clone := dialAgentWS(t, srv.URL)
		rejected := writeAgentHelloForTest(t, clone, token, changed)
		clone.Close()
		if rejected.OK || rejected.Error != "invalid enrollment token" {
			t.Fatalf("expired enrollment admitted a different/missing instance: %+v", rejected)
		}
	}
	hello.InstanceID = "88888888-8888-4888-8888-888888888888"
	restarted := dialAgentWS(t, srv.URL)
	defer restarted.Close()
	afterRestart := writeAgentHelloForTest(t, restarted, token, hello)
	if !afterRestart.OK || afterRestart.AssignedAgentID != agent.ID || afterRestart.TargetID != firstResponse.TargetID {
		t.Fatalf("same-boot process restart with expired enrollment lost identity: %+v", afterRestart)
	}

}

func testAgentReconnectKeepsAssignment(t *testing.T, boot, nextBoot, nextInstance string, disconnect bool) {
	t.Helper()
	ctx := context.Background()
	app := NewApp(Config{DatabasePath: filepath.Join(t.TempDir(), "gosshd.db")})
	token := createAgentEnrollmentForTest(t, ctx, app, "overlap-agent")
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hello := protocol.AgentHello{ID: "11111111-1111-4111-8111-111111111111", InstanceID: "22222222-2222-4222-8222-222222222222", BootID: boot}
	first := dialAgentWS(t, srv.URL)
	defer first.Close()
	response := writeAgentHelloForTest(t, first, token, hello)
	if !response.OK {
		t.Fatalf("first hello: %+v", response)
	}
	var previous *yamux.Session
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		previous, _ = app.registry.Get(response.AssignedAgentID)
		if previous != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if previous == nil {
		t.Fatal("first connection was not registered")
	}
	if disconnect {
		first.Close()
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			if _, err := app.registry.Get(response.AssignedAgentID); errors.Is(err, ErrAgentOffline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if _, err := app.registry.Get(response.AssignedAgentID); !errors.Is(err, ErrAgentOffline) {
			t.Fatal("old connection did not disconnect")
		}
	}
	hello.AssignedAgentID = response.AssignedAgentID
	hello.BootID = nextBoot
	hello.InstanceID = nextInstance
	second := dialAgentWS(t, srv.URL)
	defer second.Close()
	resumed := writeAgentHelloForTest(t, second, token, hello)
	if !resumed.OK || resumed.AssignedAgentID != response.AssignedAgentID || resumed.TargetID != response.TargetID || resumed.TargetAlias != response.TargetAlias {
		t.Fatalf("normal reconnect/reboot forked its assignment: first=%+v resumed=%+v", response, resumed)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		current, _ := app.registry.Get(response.AssignedAgentID)
		if current != nil && current != previous {
			break
		}
		time.Sleep(time.Millisecond)
	}
	current, err := app.registry.Get(response.AssignedAgentID)
	if err != nil || current == previous || !previous.IsClosed() {
		t.Fatalf("obsolete connection was not replaced: %v", err)
	}
	if app.registry.Unregister(response.AssignedAgentID, previous) {
		t.Fatal("late old disconnect must not report the active Agent as offline")
	}
	if active, err := app.registry.Get(response.AssignedAgentID); err != nil || active != current {
		t.Fatalf("late disconnect removed the resumed connection: %v", err)
	}
	enrollment, err := app.store.Repository().GetAgentEnrollmentByTokenHash(ctx, codeHash(token))
	if err != nil {
		t.Fatal(err)
	}
	targets, err := app.store.Repository().ListSSHTargets(ctx, enrollment.OwnerType, enrollment.OwnerID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("reconnect created a duplicate target: %+v %v", targets, err)
	}
}

func TestAgentWSOverlappingReconnectKeepsAssignment(t *testing.T) {
	testAgentReconnectKeepsAssignment(t, "", "", "22222222-2222-4222-8222-222222222222", false)
}
func TestAgentWSSameBootProcessRestartKeepsAssignment(t *testing.T) {
	testAgentReconnectKeepsAssignment(t, "33333333-3333-4333-8333-333333333333", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444", false)
}
func TestAgentWSRebootWithStaleRegistrationKeepsAssignment(t *testing.T) {
	testAgentReconnectKeepsAssignment(t, "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555", "44444444-4444-4444-8444-444444444444", false)
}
func TestAgentWSRebootAfterDisconnectKeepsAssignment(t *testing.T) {
	testAgentReconnectKeepsAssignment(t, "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555", "44444444-4444-4444-8444-444444444444", true)
}
