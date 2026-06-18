package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/qinyongliang/gosshd/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestAPIRegisterLoginMeAndLogout(t *testing.T) {
	srv, client, _ := newAPITestServer(t)
	defer srv.Close()

	var reg apiUserResponse
	postJSON(t, client, srv.URL+"/api/auth/register", map[string]string{
		"email":        "Alice@Example.com",
		"display_name": "Alice",
		"password":     "secret-pass",
	}, http.StatusCreated, &reg)
	if reg.User.ID == "" || reg.User.Email != "alice@example.com" {
		t.Fatalf("unexpected register response: %+v", reg)
	}

	postJSON(t, client, srv.URL+"/api/auth/logout", nil, http.StatusOK, nil)

	var login apiUserResponse
	postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{
		"email":    "alice@example.com",
		"password": "secret-pass",
	}, http.StatusOK, &login)
	if login.User.ID != reg.User.ID {
		t.Fatalf("login user mismatch: %+v", login)
	}

	var me apiMeResponse
	getJSON(t, client, srv.URL+"/api/me", http.StatusOK, &me)
	if me.User.ID != reg.User.ID {
		t.Fatalf("me user mismatch: %+v", me)
	}
}

func TestAPIOrganizationCreateInviteJoin(t *testing.T) {
	srv, alice, _ := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, alice, srv.URL, "alice@example.com")

	var org apiOrganizationResponse
	postJSON(t, alice, srv.URL+"/api/orgs", map[string]string{
		"name": "Ops",
		"slug": "ops",
	}, http.StatusCreated, &org)
	if org.Organization.ID == "" {
		t.Fatalf("missing org id")
	}

	var invite apiInviteResponse
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/invites", map[string]string{
		"role": "member",
	}, http.StatusCreated, &invite)
	if invite.Code == "" {
		t.Fatalf("missing invite code")
	}

	bob := apiClient(t)
	registerForAPI(t, bob, srv.URL, "bob@example.com")
	var joined apiOrganizationResponse
	postJSON(t, bob, srv.URL+"/api/orgs/join", map[string]string{
		"code": invite.Code,
	}, http.StatusOK, &joined)
	if joined.Organization.ID != org.Organization.ID {
		t.Fatalf("joined org mismatch: %+v", joined)
	}

	var me apiMeResponse
	getJSON(t, bob, srv.URL+"/api/me", http.StatusOK, &me)
	if len(me.Organizations) != 1 || me.Organizations[0].ID != org.Organization.ID {
		t.Fatalf("bob organizations mismatch: %+v", me.Organizations)
	}
}

func TestAPIOrganizationDefaultAndCustomUserGroups(t *testing.T) {
	srv, alice, _ := newAPITestServer(t)
	defer srv.Close()
	aliceUser := registerForAPI(t, alice, srv.URL, "alice@example.com")

	var org apiOrganizationResponse
	postJSON(t, alice, srv.URL+"/api/orgs", map[string]string{
		"name": "Ops",
		"slug": "ops",
	}, http.StatusCreated, &org)

	var groups apiUserGroupsResponse
	getJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/groups", http.StatusOK, &groups)
	if len(groups.Groups) != 1 || !groups.Groups[0].IsDefault {
		t.Fatalf("default group missing: %+v", groups)
	}

	var custom apiUserGroupResponse
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/groups", map[string]string{
		"name": "DBA",
		"slug": "dba",
	}, http.StatusCreated, &custom)
	if custom.Group.ID == "" || custom.Group.IsDefault {
		t.Fatalf("custom group mismatch: %+v", custom)
	}

	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/groups/"+custom.Group.ID+"/members", map[string]string{
		"user_id": aliceUser.User.ID,
	}, http.StatusOK, nil)

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/orgs/"+org.Organization.ID+"/groups/"+custom.Group.ID+"/members/"+aliceUser.User.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := alice.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove group member status mismatch: got %d", resp.StatusCode)
	}
}

func TestAPIPublicKeyCRUD(t *testing.T) {
	srv, client, _ := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, client, srv.URL, "alice@example.com")

	signer := testAPISigner(t)
	rawKey := string(gossh.MarshalAuthorizedKey(signer.PublicKey()))
	var created apiPublicKeyResponse
	postJSON(t, client, srv.URL+"/api/keys", map[string]string{
		"name":           "laptop",
		"authorized_key": rawKey,
	}, http.StatusCreated, &created)
	if created.Key.ID == "" || created.Key.Fingerprint != gossh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("unexpected key response: %+v", created)
	}

	var listed apiPublicKeysResponse
	getJSON(t, client, srv.URL+"/api/keys", http.StatusOK, &listed)
	if len(listed.Keys) != 1 || listed.Keys[0].ID != created.Key.ID {
		t.Fatalf("key list mismatch: %+v", listed)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/keys/"+created.Key.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status mismatch: got %d", resp.StatusCode)
	}

	getJSON(t, client, srv.URL+"/api/keys", http.StatusOK, &listed)
	if len(listed.Keys) != 0 {
		t.Fatalf("expected no keys after delete, got %+v", listed)
	}
}

func TestAPITargetPolicyUserGroupAndAuditFlow(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	user := registerForAPI(t, client, srv.URL, "alice@example.com")

	var org apiOrganizationResponse
	postJSON(t, client, srv.URL+"/api/orgs", map[string]string{"name": "Ops", "slug": "ops"}, http.StatusCreated, &org)
	var groups apiUserGroupsResponse
	getJSON(t, client, srv.URL+"/api/orgs/"+org.Organization.ID+"/groups", http.StatusOK, &groups)
	if len(groups.Groups) != 1 {
		t.Fatalf("missing default group")
	}

	var target apiTargetResponse
	postJSON(t, client, srv.URL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"alias":           "test2",
		"target_type":     "direct",
		"host":            "127.0.0.1",
		"port":            22,
		"remote_username": "root",
		"auth_type":       "password",
		"secret":          "secret",
	}, http.StatusCreated, &target)
	if target.Target.ID == "" || target.Target.Alias != "test2" {
		t.Fatalf("target response mismatch: %+v", target)
	}
	var renamed apiTargetResponse
	patchJSON(t, client, srv.URL+"/api/targets/"+target.Target.ID, map[string]any{
		"alias": "renamed",
	}, http.StatusOK, &renamed)
	if renamed.Target.Alias != "renamed" {
		t.Fatalf("target rename mismatch: %+v", renamed)
	}
	target.Target = renamed.Target

	var policy apiPolicyResponse
	postJSON(t, client, srv.URL+"/api/policies", map[string]any{
		"owner_type":     "organization",
		"owner_id":       org.Organization.ID,
		"name":           "strict",
		"default_action": "deny",
	}, http.StatusCreated, &policy)
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/rules", map[string]string{
		"rule_type":    "whitelist",
		"pattern_type": "exact",
		"pattern":      "whoami",
	}, http.StatusCreated, nil)
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/targets", map[string]string{
		"target_id": target.Target.ID,
	}, http.StatusOK, nil)
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/user-groups", map[string]string{
		"group_id": groups.Groups[0].ID,
	}, http.StatusOK, nil)

	audit, err := app.store.Repository().CreateCommandAuditLog(contextBackground(), store.CreateCommandAuditLogParams{
		UserID:         user.User.ID,
		TargetID:       target.Target.ID,
		OrganizationID: org.Organization.ID,
		SessionID:      "session-1",
		Command:        "whoami",
		RequestType:    store.RequestExec,
		PolicyDecision: store.DecisionAllow,
		PolicyReason:   "whitelist",
		ExitCode:       intPtr(0),
		RemoteAddress:  "127.0.0.1:12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs apiAuditLogsResponse
	getJSON(t, client, srv.URL+"/api/audit", http.StatusOK, &logs)
	if len(logs.Logs) != 1 || logs.Logs[0].ID != audit.ID || logs.Logs[0].Command != "whoami" {
		t.Fatalf("audit logs mismatch: %+v", logs)
	}
}

func TestAPIAgentEnrollmentReturnsInstallScripts(t *testing.T) {
	srv, client, _ := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, client, srv.URL, "alice@example.com")

	var enrollment apiAgentEnrollmentResponse
	postJSON(t, client, srv.URL+"/api/agent-enrollments", map[string]any{
		"owner_type":   "user",
		"owner_id":     "me",
		"label":        "laptop",
		"default_host": "127.0.0.1",
		"default_port": 22,
	}, http.StatusCreated, &enrollment)
	if enrollment.Token == "" || enrollment.InstallSH == "" || enrollment.InstallPS1 == "" {
		t.Fatalf("enrollment response missing install data: %+v", enrollment)
	}

	resp, err := client.Get(srv.URL + "/install/" + enrollment.Token + ".sh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install script status mismatch: %d", resp.StatusCode)
	}
}

func newAPITestServer(t *testing.T) (*httptest.Server, *http.Client, *App) {
	t.Helper()
	app := NewApp(Config{
		DatabasePath:      filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName: "gosshd_test_session",
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		if app.store != nil {
			app.store.Close()
		}
	})
	return srv, apiClient(t), app
}

func apiClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func registerForAPI(t *testing.T, client *http.Client, baseURL, email string) apiUserResponse {
	t.Helper()
	var out apiUserResponse
	postJSON(t, client, baseURL+"/api/auth/register", map[string]string{
		"email":        email,
		"display_name": email,
		"password":     "secret-pass",
	}, http.StatusCreated, &out)
	return out
}

func postJSON(t *testing.T, client *http.Client, url string, body any, wantStatus int, out any) {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status mismatch: got %d want %d", url, resp.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func getJSON(t *testing.T, client *http.Client, url string, wantStatus int, out any) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s status mismatch: got %d want %d", url, resp.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func patchJSON(t *testing.T, client *http.Client, url string, body any, wantStatus int, out any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("PATCH %s status mismatch: got %d want %d", url, resp.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func testAPISigner(t *testing.T) gossh.Signer {
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

func intPtr(v int) *int {
	return &v
}
