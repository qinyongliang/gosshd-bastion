package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/store"

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

func TestAPIBootstrapAdminAndAdminSettings(t *testing.T) {
	srv, adminClient, _ := newAPITestServer(t)
	defer srv.Close()

	var admin apiUserResponse
	postJSON(t, adminClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, &admin)
	if !admin.User.IsSystemAdmin {
		t.Fatalf("admin user should be system admin: %+v", admin)
	}

	var settings map[string]any
	getJSON(t, adminClient, srv.URL+"/api/admin/settings", http.StatusOK, &settings)

	putJSON(t, adminClient, srv.URL+"/api/admin/settings/dingtalk", map[string]any{
		"enabled":        true,
		"client_id":      "app-key",
		"client_secret":  "app-secret",
		"auth_url":       "https://login.dingtalk.example/authorize",
		"token_url":      "https://login.dingtalk.example/token",
		"userinfo_url":   "https://login.dingtalk.example/userinfo",
		"redirect_url":   "https://bastion.example/api/auth/dingtalk/callback",
		"default_role":   "member",
		"default_org_id": "org-1",
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
	getJSON(t, adminClient, srv.URL+"/api/admin/settings", http.StatusOK, &settings)
	if settings["dingtalk"] == nil || settings["ldap"] == nil {
		t.Fatalf("settings response missing providers: %+v", settings)
	}

	regular := apiClient(t)
	registerForAPI(t, regular, srv.URL, "regular@example.com")
	getJSON(t, regular, srv.URL+"/api/admin/settings", http.StatusForbidden, nil)
}

func TestAPIAdminOrganizationsExcludePersonal(t *testing.T) {
	srv, adminClient, _ := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, adminClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, nil)

	regular := apiClient(t)
	registerForAPI(t, regular, srv.URL, "personal-owner@example.com")

	var org apiOrganizationResponse
	postJSON(t, adminClient, srv.URL+"/api/orgs", map[string]string{
		"name": "Shared Ops",
		"slug": "shared-ops",
	}, http.StatusCreated, &org)

	var out struct {
		Organizations []apiOrganization `json:"organizations"`
	}
	getJSON(t, adminClient, srv.URL+"/api/admin/orgs", http.StatusOK, &out)
	if !hasOrganization(out.Organizations, org.Organization.ID) {
		t.Fatalf("admin org list missing shared organization: %+v", out.Organizations)
	}
	if hasPersonalOrganization(out.Organizations) {
		t.Fatalf("admin org list should hide personal organizations: %+v", out.Organizations)
	}
}

func TestAPIAdminResetUserPassword(t *testing.T) {
	srv, adminClient, app := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, adminClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, nil)

	regular := apiClient(t)
	regularUser := registerForAPI(t, regular, srv.URL, "reset-me@example.com")
	putJSON(t, adminClient, srv.URL+"/api/admin/users/"+regularUser.User.ID+"/password", map[string]string{
		"password": "new-secret-pass",
	}, http.StatusOK, nil)

	postJSON(t, regular, srv.URL+"/api/auth/logout", nil, http.StatusOK, nil)
	postJSON(t, regular, srv.URL+"/api/auth/login", map[string]string{
		"email":    "reset-me@example.com",
		"password": "secret-pass",
	}, http.StatusUnauthorized, nil)
	postJSON(t, regular, srv.URL+"/api/auth/login", map[string]string{
		"email":    "reset-me@example.com",
		"password": "new-secret-pass",
	}, http.StatusOK, nil)

	outsider := apiClient(t)
	registerForAPI(t, outsider, srv.URL, "outsider@example.com")
	putJSON(t, outsider, srv.URL+"/api/admin/users/"+regularUser.User.ID+"/password", map[string]string{
		"password": "blocked-pass",
	}, http.StatusForbidden, nil)

	external, err := app.store.Repository().CreateUser(context.Background(), store.CreateUserParams{
		Email:        "external@example.com",
		DisplayName:  "External",
		PasswordHash: []byte("hash"),
		AuthProvider: "dingtalk",
	})
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, adminClient, srv.URL+"/api/admin/users/"+external.ID+"/password", map[string]string{
		"password": "should-not-apply",
	}, http.StatusBadRequest, nil)
}

func TestAPIDingTalkMockLoginCreatesAndAssignsUser(t *testing.T) {
	srv, adminClient, _ := newAPITestServer(t)
	defer srv.Close()
	postJSON(t, adminClient, srv.URL+"/api/auth/login", map[string]string{
		"email":    "admin",
		"password": "admin-pass",
	}, http.StatusOK, nil)
	var org apiOrganizationResponse
	postJSON(t, adminClient, srv.URL+"/api/orgs", map[string]string{
		"name": "Ops",
		"slug": "ops-dingtalk",
	}, http.StatusCreated, &org)
	mock := newServerMockDingTalk(t, "union-api-1", "open-api-1", "Dora", "dora@example.com")
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

	dingtalkClient := apiClientNoRedirect(t)
	resp, err := dingtalkClient.Get(srv.URL + "/api/auth/dingtalk/start?redirect_after=/targets")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status mismatch: %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	state := mustQueryParam(t, location, "state")
	resp, err = dingtalkClient.Get(srv.URL + "/api/auth/dingtalk/callback?code=valid-code&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback status mismatch: %d", resp.StatusCode)
	}

	var me apiMeResponse
	getJSON(t, dingtalkClient, srv.URL+"/api/me", http.StatusOK, &me)
	if me.User.Email != "dora@example.com" || me.User.AuthProvider != "dingtalk" {
		t.Fatalf("dingtalk me mismatch: %+v", me)
	}
	if !hasOrganization(me.Organizations, org.Organization.ID) {
		t.Fatalf("dingtalk user missing default org: %+v", me.Organizations)
	}
	var members apiOrganizationMembersResponse
	getJSON(t, adminClient, srv.URL+"/api/admin/orgs/"+org.Organization.ID+"/members", http.StatusOK, &members)
	if !hasMemberRole(members.Members, me.User.ID, store.RoleMember) {
		t.Fatalf("dingtalk user missing member role: %+v", members)
	}
}

func TestAPIOrganizationMemberRoleAndOwnerTransfer(t *testing.T) {
	srv, alice, _ := newAPITestServer(t)
	defer srv.Close()
	aliceUser := registerForAPI(t, alice, srv.URL, "alice@example.com")
	var org apiOrganizationResponse
	postJSON(t, alice, srv.URL+"/api/orgs", map[string]string{"name": "Ops", "slug": "ops-members"}, http.StatusCreated, &org)
	bob := apiClient(t)
	bobUser := registerForAPI(t, bob, srv.URL, "bob@example.com")
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", map[string]string{
		"user_id": bobUser.User.ID,
		"role":    "member",
	}, http.StatusOK, nil)

	var members apiOrganizationMembersResponse
	getJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", http.StatusOK, &members)
	if !hasMemberRole(members.Members, bobUser.User.ID, store.RoleMember) {
		t.Fatalf("bob member missing: %+v", members)
	}
	if !hasMemberCreatedAt(members.Members, bobUser.User.ID) {
		t.Fatalf("bob member missing created_at: %+v", members)
	}
	patchJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members/"+bobUser.User.ID, map[string]string{
		"role": "admin",
	}, http.StatusOK, nil)
	getJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", http.StatusOK, &members)
	if !hasMemberRole(members.Members, bobUser.User.ID, store.RoleAdmin) {
		t.Fatalf("bob admin role missing: %+v", members)
	}
	patchJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members/"+bobUser.User.ID, map[string]string{
		"role": "member",
	}, http.StatusOK, nil)
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/transfer-owner", map[string]string{
		"user_id": bobUser.User.ID,
	}, http.StatusOK, nil)
	getJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", http.StatusOK, &members)
	if !hasMemberRole(members.Members, bobUser.User.ID, store.RoleOwner) || !hasMemberRole(members.Members, aliceUser.User.ID, store.RoleAdmin) {
		t.Fatalf("owner transfer roles mismatch: %+v", members)
	}
}

func TestAPIOrganizationMemberManagementForbiddenForMember(t *testing.T) {
	srv, alice, _ := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, alice, srv.URL, "alice@example.com")
	var org apiOrganizationResponse
	postJSON(t, alice, srv.URL+"/api/orgs", map[string]string{"name": "Ops", "slug": "ops-forbidden"}, http.StatusCreated, &org)
	bob := apiClient(t)
	bobUser := registerForAPI(t, bob, srv.URL, "bob@example.com")
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", map[string]string{
		"user_id": bobUser.User.ID,
		"role":    "member",
	}, http.StatusOK, nil)
	patchJSON(t, bob, srv.URL+"/api/orgs/"+org.Organization.ID+"/members/"+bobUser.User.ID, map[string]string{
		"role": "admin",
	}, http.StatusForbidden, nil)
	postJSON(t, bob, srv.URL+"/api/orgs/"+org.Organization.ID+"/transfer-owner", map[string]string{
		"user_id": bobUser.User.ID,
	}, http.StatusForbidden, nil)
}

func TestAPIOrganizationCreateInviteJoin(t *testing.T) {
	srv, alice, _ := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, alice, srv.URL, "alice@example.com")

	var aliceMe apiMeResponse
	getJSON(t, alice, srv.URL+"/api/me", http.StatusOK, &aliceMe)
	if len(aliceMe.Organizations) != 1 || !aliceMe.Organizations[0].IsPersonal {
		t.Fatalf("alice personal organization missing: %+v", aliceMe.Organizations)
	}
	postJSON(t, alice, srv.URL+"/api/orgs/"+aliceMe.Organizations[0].ID+"/invites", map[string]string{
		"role": "member",
	}, http.StatusBadRequest, nil)

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
	if len(me.Organizations) != 2 || !hasOrganization(me.Organizations, org.Organization.ID) || !hasPersonalOrganization(me.Organizations) {
		t.Fatalf("bob organizations mismatch: %+v", me.Organizations)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/orgs/"+org.Organization.ID+"/leave", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := bob.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("leave status mismatch: got %d", resp.StatusCode)
	}
	getJSON(t, bob, srv.URL+"/api/me", http.StatusOK, &me)
	if len(me.Organizations) != 1 || !hasPersonalOrganization(me.Organizations) {
		t.Fatalf("bob organizations after leave mismatch: %+v", me.Organizations)
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

	var llm apiLLMConfigResponse
	postJSON(t, client, srv.URL+"/api/llm-configs", map[string]any{
		"owner_type":       "organization",
		"owner_id":         org.Organization.ID,
		"name":             "reviewer",
		"base_url":         "https://llm.example.com/",
		"api_key":          "secret-key",
		"model":            "ops-model",
		"prompt":           "review commands",
		"timeout_seconds":  3,
		"unexpected_field": "ignored",
	}, http.StatusCreated, &llm)
	if llm.Config.ID == "" || llm.Config.BaseURL != "https://llm.example.com" || llm.Config.Model != "ops-model" {
		t.Fatalf("llm config response mismatch: %+v", llm)
	}
	var llms apiLLMConfigsResponse
	getJSON(t, client, srv.URL+"/api/llm-configs?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &llms)
	if len(llms.Configs) != 1 || llms.Configs[0].ID != llm.Config.ID {
		t.Fatalf("llm configs list mismatch: %+v", llms)
	}
	var prompts apiLLMPromptsResponse
	getJSON(t, client, srv.URL+"/api/llm-prompts?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &prompts)
	if len(prompts.Prompts) != 1 || !prompts.Prompts[0].IsReadonly {
		t.Fatalf("default llm prompt missing: %+v", prompts)
	}
	var prompt apiLLMPromptResponse
	postJSON(t, client, srv.URL+"/api/llm-prompts", map[string]string{
		"owner_type": "organization",
		"owner_id":   org.Organization.ID,
		"title":      "High risk review",
		"content":    "deny destructive commands",
	}, http.StatusCreated, &prompt)
	if prompt.Prompt.ID == "" || prompt.Prompt.IsReadonly {
		t.Fatalf("prompt response mismatch: %+v", prompt)
	}

	var target apiTargetResponse
	postJSON(t, client, srv.URL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"name":            "Test service",
		"alias":           "test2",
		"target_type":     "direct",
		"host":            "127.0.0.1",
		"port":            22,
		"remote_username": "root",
		"auth_type":       "password",
		"secret":          "secret",
		"tags":            []string{"测试环境", "db"},
	}, http.StatusCreated, &target)
	if target.Target.ID == "" || target.Target.Alias != "test2" || target.Target.Name != "Test service" || len(target.Target.Tags) != 2 {
		t.Fatalf("target response mismatch: %+v", target)
	}
	var proxied apiTargetResponse
	postJSON(t, client, srv.URL+"/api/targets", map[string]any{
		"owner_type":       "organization",
		"owner_id":         org.Organization.ID,
		"name":             "Private subnet service",
		"alias":            "private-subnet",
		"target_type":      "direct",
		"host":             "10.0.0.8",
		"port":             22,
		"remote_username":  "root",
		"auth_type":        "password",
		"secret":           "secret",
		"proxy_target_id":  target.Target.ID,
		"tags":             []string{"private"},
		"unexpected_field": "ignored",
	}, http.StatusCreated, &proxied)
	if proxied.Target.ProxyTargetID != target.Target.ID {
		t.Fatalf("proxied target response mismatch: %+v", proxied)
	}
	var filtered apiTargetsResponse
	getJSON(t, client, srv.URL+"/api/targets?owner_type=organization&owner_id="+org.Organization.ID+"&tags=%E6%B5%8B%E8%AF%95%E7%8E%AF%E5%A2%83", http.StatusOK, &filtered)
	if len(filtered.Targets) != 1 || filtered.Targets[0].ID != target.Target.ID {
		t.Fatalf("target tag filter mismatch: %+v", filtered)
	}
	var renamed apiTargetResponse
	patchJSON(t, client, srv.URL+"/api/targets/"+target.Target.ID, map[string]any{
		"alias": "renamed",
		"name":  "Renamed service",
		"tags":  []string{"prod"},
	}, http.StatusOK, &renamed)
	if renamed.Target.Alias != "renamed" || renamed.Target.Name != "Renamed service" || len(renamed.Target.Tags) != 1 || renamed.Target.Tags[0] != "prod" {
		t.Fatalf("target rename mismatch: %+v", renamed)
	}
	target.Target = renamed.Target

	var policy apiPolicyResponse
	postJSON(t, client, srv.URL+"/api/policies", map[string]any{
		"owner_type":     "organization",
		"owner_id":       org.Organization.ID,
		"name":           "strict",
		"default_action": "deny",
		"llm_config_id":  llm.Config.ID,
		"llm_prompt_id":  prompt.Prompt.ID,
	}, http.StatusCreated, &policy)
	if policy.Policy.LLMConfigID != llm.Config.ID || policy.Policy.LLMPromptID != prompt.Prompt.ID {
		t.Fatalf("policy llm config mismatch: %+v", policy)
	}
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/rules", map[string]string{
		"rule_type":    "whitelist",
		"pattern_type": "exact",
		"pattern":      "whoami",
	}, http.StatusCreated, nil)
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/targets", map[string]string{
		"target_id": target.Target.ID,
	}, http.StatusOK, nil)
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/target-tags", map[string]string{
		"owner_type": "organization",
		"owner_id":   org.Organization.ID,
		"tag":        "prod",
	}, http.StatusOK, nil)
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/user-groups", map[string]string{
		"group_id": groups.Groups[0].ID,
	}, http.StatusOK, nil)
	var listedPolicies struct {
		Policies []apiPolicy `json:"policies"`
	}
	getJSON(t, client, srv.URL+"/api/policies?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &listedPolicies)
	if len(listedPolicies.Policies) != 1 || len(listedPolicies.Policies[0].TargetTags) != 1 || listedPolicies.Policies[0].TargetTags[0] != "prod" {
		t.Fatalf("policy target tags mismatch: %+v", listedPolicies)
	}

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
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	user := registerForAPI(t, client, srv.URL, "alice@example.com")
	var me apiMeResponse
	getJSON(t, client, srv.URL+"/api/me", http.StatusOK, &me)
	if len(me.Organizations) == 0 {
		t.Fatalf("registered user missing organization")
	}

	var enrollment apiAgentEnrollmentResponse
	postJSON(t, client, srv.URL+"/api/agent-enrollments", map[string]any{
		"owner_type":   "organization",
		"owner_id":     me.Organizations[0].ID,
		"label":        "laptop",
		"default_host": "127.0.0.1",
		"default_port": 22,
	}, http.StatusCreated, &enrollment)
	if enrollment.Token == "" || enrollment.InstallSH == "" || enrollment.InstallPS1 == "" || enrollment.ServiceSH == "" || enrollment.ServicePS1 == "" {
		t.Fatalf("enrollment response missing install data: %+v", enrollment)
	}
	if !strings.Contains(enrollment.ServiceSH, "sudo sh -s -- install") {
		t.Fatalf("shell service command missing install mode: %s", enrollment.ServiceSH)
	}
	if !strings.Contains(enrollment.ServicePS1, "-Install") {
		t.Fatalf("powershell service command missing install flag: %s", enrollment.ServicePS1)
	}

	resp, err := client.Get(srv.URL + "/install/" + enrollment.Token + ".sh")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install script status mismatch: %d", resp.StatusCode)
	}
	shBody := readBody(t, resp)
	resp.Body.Close()
	if !strings.Contains(shBody, "systemctl enable --now gosshd-agent") || !strings.Contains(shBody, "--enrollment-token") {
		t.Fatalf("shell install script missing service install flow:\n%s", shBody)
	}

	resp, err = client.Get(srv.URL + "/install/" + enrollment.Token + ".ps1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install ps1 status mismatch: %d", resp.StatusCode)
	}
	psBody := readBody(t, resp)
	resp.Body.Close()
	if !strings.Contains(psBody, "sc.exe create gosshd-agent") || !strings.Contains(psBody, "sc.exe start gosshd-agent") {
		t.Fatalf("powershell install script missing service install flow:\n%s", psBody)
	}
	if strings.Contains(shBody, "installl") || strings.Contains(psBody, "installl") {
		t.Fatalf("install scripts should not accept misspelled install mode")
	}

	postJSON(t, client, srv.URL+"/api/agent-enrollments", map[string]any{
		"owner_type": "organization",
		"owner_id":   me.Organizations[0].ID,
		"label":      "minimal-agent",
	}, http.StatusCreated, &apiAgentEnrollmentResponse{})
	enrollments, err := app.store.Repository().ListAgentEnrollments(context.Background(), store.OwnerOrganization, me.Organizations[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var foundDefault bool
	for _, item := range enrollments {
		if item.Label == "minimal-agent" {
			foundDefault = item.DefaultHost == "127.0.0.1" && item.DefaultPort == 22
			break
		}
	}
	if !foundDefault {
		t.Fatalf("minimal agent enrollment did not receive defaults: %+v", enrollments)
	}

	postJSON(t, client, srv.URL+"/api/agent-enrollments", map[string]any{
		"label": "missing-owner",
	}, http.StatusBadRequest, nil)
	_ = user
}

func newAPITestServer(t *testing.T) (*httptest.Server, *http.Client, *App) {
	t.Helper()
	app := NewApp(Config{
		DatabasePath:           filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName:      "gosshd_test_session",
		BootstrapAdminPassword: "admin-pass",
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

func apiClientNoRedirect(t *testing.T) *http.Client {
	t.Helper()
	client := apiClient(t)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
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

func putJSON(t *testing.T, client *http.Client, url string, body any, wantStatus int, out any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(payload))
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
		t.Fatalf("PUT %s status mismatch: got %d want %d", url, resp.StatusCode, wantStatus)
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

func hasOrganization(orgs []apiOrganization, id string) bool {
	for _, org := range orgs {
		if org.ID == id {
			return true
		}
	}
	return false
}

func hasPersonalOrganization(orgs []apiOrganization) bool {
	for _, org := range orgs {
		if org.IsPersonal {
			return true
		}
	}
	return false
}

func hasMemberRole(members []apiOrganizationMember, userID, role string) bool {
	for _, member := range members {
		if member.UserID == userID && member.Role == role {
			return true
		}
	}
	return false
}

func hasMemberCreatedAt(members []apiOrganizationMember, userID string) bool {
	for _, member := range members {
		if member.UserID == userID && member.CreatedAt != "" {
			return true
		}
	}
	return false
}

func newServerMockDingTalk(t *testing.T, unionID, openID, name, email string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("code") != "valid-code" {
			http.Error(w, "bad code", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-access-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unionid": unionID,
			"openid":  openID,
			"name":    name,
			"email":   email,
		})
	})
	return httptest.NewServer(mux)
}

func mustQueryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	value := u.Query().Get(key)
	if value == "" {
		t.Fatalf("missing query param %q in %s", key, rawURL)
	}
	return value
}
