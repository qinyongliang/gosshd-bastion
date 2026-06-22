package server

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/agent"
	"github.com/qinyongliang/gosshd-bastion/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestBastionE2E(t *testing.T) {
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp(Config{
		DatabasePath:      filepath.Join(t.TempDir(), "gosshd.db"),
		HostKeyPath:       filepath.Join(t.TempDir(), "host_key"),
		SessionCookieName: "e2e_session",
	})
	enablePublicRegistrationForTest(t, app)
	go func() {
		if err := app.RunListeners(ctx, httpLn, sshLn); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		if app.store != nil {
			_ = app.Close()
		}
	})

	baseURL := "http://" + httpLn.Addr().String()
	client := apiClient(t)
	user := registerForAPI(t, client, baseURL, "alice@example.com")

	var me apiMeResponse
	getJSON(t, client, baseURL+"/api/me", http.StatusOK, &me)
	if len(me.Organizations) != 1 || !me.Organizations[0].IsPersonal {
		t.Fatalf("personal organization missing: %+v", me.Organizations)
	}
	personalOrg := me.Organizations[0]

	var groups apiUserGroupsResponse
	getJSON(t, client, baseURL+"/api/orgs/"+personalOrg.ID+"/groups", http.StatusOK, &groups)
	if len(groups.Groups) != 1 || !groups.Groups[0].IsDefault {
		t.Fatalf("default group missing: %+v", groups)
	}
	var prompts apiLLMPromptsResponse
	getJSON(t, client, baseURL+"/api/llm-prompts?owner_type=organization&owner_id="+personalOrg.ID, http.StatusOK, &prompts)
	if len(prompts.Prompts) != 1 || !prompts.Prompts[0].IsReadonly {
		t.Fatalf("default prompt missing: %+v", prompts)
	}

	userSigner := testSSHSigner(t)
	var key apiPublicKeyResponse
	postJSON(t, client, baseURL+"/api/keys", map[string]string{
		"name":           "e2e",
		"authorized_key": string(gossh.MarshalAuthorizedKey(userSigner.PublicKey())),
	}, http.StatusCreated, &key)
	if key.Key.Fingerprint == "" {
		t.Fatalf("key response missing fingerprint")
	}

	targetAddr, closeTarget := startTestSSHServer(t)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	var target apiTargetResponse
	postJSON(t, client, baseURL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        personalOrg.ID,
		"alias":           "test2",
		"target_type":     "direct",
		"host":            host,
		"port":            port,
		"remote_username": "remote",
		"auth_type":       "password",
	}, http.StatusCreated, &target)
	bindAllowPolicyForAPI(t, client, baseURL, personalOrg.ID, target.Target.ID, groups.Groups[0].ID)

	out, err := runBastionSSHCommand(sshLn.Addr().String(), "test2", userSigner, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "remote" {
		t.Fatalf("unexpected ssh output %q", out)
	}
	assertAuditContains(t, client, baseURL, "whoami", store.DecisionAllow)

	var policy apiPolicyResponse
	postJSON(t, client, baseURL+"/api/policies", map[string]any{
		"owner_type":     "organization",
		"owner_id":       personalOrg.ID,
		"name":           "deny destructive",
		"default_action": "allow",
	}, http.StatusCreated, &policy)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/rules", map[string]string{
		"rule_type":    "blacklist",
		"pattern_type": "contains",
		"pattern":      "rm -rf",
	}, http.StatusCreated, nil)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/targets", map[string]string{
		"target_id": target.Target.ID,
	}, http.StatusOK, nil)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/user-groups", map[string]string{
		"group_id": groups.Groups[0].ID,
	}, http.StatusOK, nil)

	if _, err := runBastionSSHCommand(sshLn.Addr().String(), "test2", userSigner, "rm -rf /tmp/e2e"); err == nil {
		t.Fatalf("expected blacklist denial")
	}
	assertAuditContains(t, client, baseURL, "rm -rf /tmp/e2e", store.DecisionDeny)

	var enrollment apiAgentEnrollmentResponse
	postJSON(t, client, baseURL+"/api/agent-enrollments", map[string]any{
		"owner_type":   "organization",
		"owner_id":     personalOrg.ID,
		"label":        "agentbox-initial",
		"default_host": host,
		"default_port": port,
	}, http.StatusCreated, &enrollment)
	agentClient, err := agent.New(agent.Config{
		Server:          baseURL,
		EnrollmentToken: enrollment.Token,
		IDFile:          filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()
	go func() {
		if err := agentClient.Run(agentCtx); err != nil {
			t.Logf("agent stopped: %v", err)
		}
	}()
	agentTarget := waitForAgentTarget(t, app, personalOrg.ID)
	var renamed apiTargetResponse
	patchJSON(t, client, baseURL+"/api/targets/"+agentTarget.ID, map[string]string{"alias": "agentbox"}, http.StatusOK, &renamed)
	bindAllowPolicyForAPI(t, client, baseURL, personalOrg.ID, agentTarget.ID, groups.Groups[0].ID)
	out, err = runBastionSSHCommand(sshLn.Addr().String(), "agentbox", userSigner, "echo agent-ok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "agent-ok" {
		t.Fatalf("unexpected agent ssh output %q", out)
	}

	assertMCPListsTools(t, baseURL, client)

	resp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "gosshd Bastion") {
		t.Fatalf("frontend response mismatch: %d %s", resp.StatusCode, body)
	}

	_ = user
}

func TestDingTalkAdminOrganizationE2E(t *testing.T) {
	srv, adminClient, app := newAPITestServer(t)
	defer srv.Close()

	var admin apiUserResponse
	postJSON(t, adminClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, &admin)
	if !admin.User.IsSystemAdmin {
		t.Fatalf("bootstrap admin should be system admin: %+v", admin.User)
	}

	var org apiOrganizationResponse
	postJSON(t, adminClient, srv.URL+"/api/orgs", map[string]string{
		"name": "E2E Ops",
		"slug": "e2e-ops",
	}, http.StatusCreated, &org)

	mock := newServerMockDingTalk(t, "union-e2e-1", "open-e2e-1", "Ding User", "ding-user@example.com")
	defer mock.Close()
	putJSON(t, adminClient, srv.URL+"/api/admin/settings/dingtalk", map[string]any{
		"enabled":        true,
		"client_id":      "app-key",
		"client_secret":  "app-secret",
		"auth_url":       mock.URL + "/authorize",
		"token_url":      mock.URL + "/token",
		"userinfo_url":   mock.URL + "/userinfo",
		"redirect_url":   srv.URL + "/api/auth/dingtalk/callback",
		"default_role":   "member",
		"default_org_id": org.Organization.ID,
	}, http.StatusOK, nil)
	putJSON(t, adminClient, srv.URL+"/api/admin/settings/ldap", map[string]any{
		"enabled":       true,
		"server_url":    "ldap://ldap.example",
		"bind_dn":       "cn=reader,dc=example,dc=com",
		"bind_password": "secret",
		"base_dn":       "dc=example,dc=com",
		"user_filter":   "(uid={username})",
		"email_attr":    "mail",
		"name_attr":     "cn",
	}, http.StatusOK, nil)

	dingtalkClient := apiClientNoRedirect(t)
	resp, err := dingtalkClient.Get(srv.URL + "/api/auth/dingtalk/start?redirect_after=/targets")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("dingtalk start status mismatch: %d", resp.StatusCode)
	}
	oauthState := mustQueryParam(t, resp.Header.Get("Location"), "state")
	resp, err = dingtalkClient.Get(srv.URL + "/api/auth/dingtalk/callback?code=valid-code&state=" + url.QueryEscape(oauthState))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("dingtalk callback status mismatch: %d", resp.StatusCode)
	}

	var dingMe apiMeResponse
	getJSON(t, dingtalkClient, srv.URL+"/api/me", http.StatusOK, &dingMe)
	if dingMe.User.Email != "ding-user@example.com" || dingMe.User.AuthProvider != "dingtalk" {
		t.Fatalf("dingtalk user mismatch: %+v", dingMe.User)
	}
	if !hasOrganization(dingMe.Organizations, org.Organization.ID) || orgRole(dingMe.Organizations, org.Organization.ID) != store.RoleMember {
		t.Fatalf("dingtalk user was not added to default organization as member: %+v", dingMe.Organizations)
	}
	defaultGroup, err := app.store.Repository().GetDefaultOrganizationUserGroup(context.Background(), org.Organization.ID)
	if err != nil {
		t.Fatal(err)
	}
	inDefaultGroup, err := app.store.Repository().UserInGroup(context.Background(), defaultGroup.ID, dingMe.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !inDefaultGroup {
		t.Fatalf("dingtalk user was not added to default all-members group")
	}

	var users struct {
		Users []apiUser `json:"users"`
	}
	getJSON(t, adminClient, srv.URL+"/api/admin/users", http.StatusOK, &users)
	if !hasAPIUser(users.Users, dingMe.User.ID) || !hasAPIUser(users.Users, admin.User.ID) {
		t.Fatalf("admin user list missing expected users: %+v", users.Users)
	}
	var orgs struct {
		Organizations []apiOrganization `json:"organizations"`
	}
	getJSON(t, adminClient, srv.URL+"/api/admin/orgs", http.StatusOK, &orgs)
	if !hasOrganization(orgs.Organizations, org.Organization.ID) {
		t.Fatalf("admin org list missing created org: %+v", orgs.Organizations)
	}

	patchJSON(t, adminClient, srv.URL+"/api/orgs/"+org.Organization.ID+"/members/"+dingMe.User.ID, map[string]string{
		"role": "admin",
	}, http.StatusOK, nil)
	patchJSON(t, adminClient, srv.URL+"/api/orgs/"+org.Organization.ID+"/members/"+dingMe.User.ID, map[string]string{
		"role": "member",
	}, http.StatusOK, nil)
	postJSON(t, adminClient, srv.URL+"/api/orgs/"+org.Organization.ID+"/transfer-owner", map[string]string{
		"user_id": dingMe.User.ID,
	}, http.StatusOK, nil)
	var members apiOrganizationMembersResponse
	getJSON(t, adminClient, srv.URL+"/api/admin/orgs/"+org.Organization.ID+"/members", http.StatusOK, &members)
	if !hasMemberRole(members.Members, dingMe.User.ID, store.RoleOwner) || !hasMemberRole(members.Members, admin.User.ID, store.RoleAdmin) {
		t.Fatalf("owner transfer did not set expected roles: %+v", members.Members)
	}

	memberClient := apiClient(t)
	member := registerForAPI(t, memberClient, srv.URL, "plain-member@example.com")
	postJSON(t, adminClient, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", map[string]string{
		"user_id": member.User.ID,
		"role":    "member",
	}, http.StatusOK, nil)
	patchJSON(t, memberClient, srv.URL+"/api/orgs/"+org.Organization.ID+"/members/"+member.User.ID, map[string]string{
		"role": "admin",
	}, http.StatusForbidden, nil)

	postJSON(t, adminClient, srv.URL+"/api/admin/orgs/"+org.Organization.ID+"/transfer-owner", map[string]string{
		"user_id": admin.User.ID,
	}, http.StatusOK, nil)
	getJSON(t, adminClient, srv.URL+"/api/admin/orgs/"+org.Organization.ID+"/members", http.StatusOK, &members)
	if !hasMemberRole(members.Members, admin.User.ID, store.RoleOwner) || !hasMemberRole(members.Members, dingMe.User.ID, store.RoleAdmin) {
		t.Fatalf("system admin transfer did not repair owner: %+v", members.Members)
	}

	for _, path := range []string{"/", "/targets", "/system-admin"} {
		resp, err := adminClient.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status mismatch: %d", path, resp.StatusCode)
		}
		if !strings.Contains(body, "gosshd Bastion") || !strings.Contains(body, `id="root"`) || !strings.Contains(body, `type="module"`) {
			t.Fatalf("%s did not load React frontend", path)
		}
	}
}

func assertAuditContains(t *testing.T, client *http.Client, baseURL, command, decision string) {
	t.Helper()
	var logs apiAuditLogsResponse
	getJSON(t, client, baseURL+"/api/audit", http.StatusOK, &logs)
	for _, log := range logs.Logs {
		if log.Command == command && log.PolicyDecision == decision {
			return
		}
	}
	t.Fatalf("audit missing %q/%q in %+v", command, decision, logs.Logs)
}

func bindAllowPolicyForAPI(t *testing.T, client *http.Client, baseURL, orgID, targetID, groupID string) {
	t.Helper()
	var policy apiPolicyResponse
	postJSON(t, client, baseURL+"/api/policies", map[string]any{
		"owner_type":     "organization",
		"owner_id":       orgID,
		"name":           "allow test commands",
		"default_action": "allow",
	}, http.StatusCreated, &policy)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/targets", map[string]string{
		"target_id": targetID,
	}, http.StatusOK, nil)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/user-groups", map[string]string{
		"group_id": groupID,
	}, http.StatusOK, nil)
}

func orgRole(orgs []apiOrganization, id string) string {
	for _, org := range orgs {
		if org.ID == id {
			return org.Role
		}
	}
	return ""
}

func hasAPIUser(users []apiUser, id string) bool {
	for _, user := range users {
		if user.ID == id {
			return true
		}
	}
	return false
}

func waitForAgentTarget(t *testing.T, app *App, orgID string) store.SSHTarget {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		targets, err := app.store.Repository().ListSSHTargets(context.Background(), store.OwnerOrganization, orgID)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			if target.TargetType == store.TargetAgent {
				return target
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("agent target not created")
	return store.SSHTarget{}
}

func assertMCPListsTools(t *testing.T, baseURL string, httpClient *http.Client) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "gosshd-e2e"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             baseURL + "/mcp",
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mcpHasTool(tools, "target_create") {
		t.Fatalf("MCP target_create missing")
	}
}
