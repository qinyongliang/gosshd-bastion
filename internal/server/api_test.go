package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if me.Runtime.SSHHost == "" || me.Runtime.SSHPort != 22 {
		t.Fatalf("me runtime mismatch: %+v", me.Runtime)
	}
	if me.Runtime.AppName != "gosshd" || me.Runtime.AppDescription == "" {
		t.Fatalf("me runtime branding mismatch: %+v", me.Runtime)
	}
}

func TestAPIRegisterDisabledByDefault(t *testing.T) {
	app := NewApp(Config{
		DatabasePath:           filepath.Join(t.TempDir(), "gosshd.db"),
		SessionCookieName:      "gosshd_test_session",
		BootstrapAdminPassword: "admin-pass",
	})
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Cleanup(func() {
		if app.store != nil {
			_ = app.Close()
		}
	})

	var providers ProvidersForTest
	getJSON(t, apiClient(t), srv.URL+"/api/auth/providers", http.StatusOK, &providers)
	if providers.RegistrationEnabled {
		t.Fatalf("registration should be disabled by default: %+v", providers)
	}

	postJSON(t, apiClient(t), srv.URL+"/api/auth/register", map[string]string{
		"email":        "blocked@example.com",
		"display_name": "Blocked",
		"password":     "secret-pass",
	}, http.StatusForbidden, nil)
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

	putJSON(t, adminClient, srv.URL+"/api/admin/settings/auth", map[string]any{
		"public_registration": true,
	}, http.StatusOK, nil)
	putJSON(t, adminClient, srv.URL+"/api/admin/settings/branding", map[string]any{
		"app_name":        "吉时雨堡垒机",
		"app_description": "内部 SSH 安全控制台",
	}, http.StatusOK, nil)
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
	if settings["auth"] == nil || settings["branding"] == nil || settings["dingtalk"] == nil || settings["ldap"] == nil {
		t.Fatalf("settings response missing providers: %+v", settings)
	}
	branding, _ := settings["branding"].(map[string]any)
	if branding["app_name"] != "吉时雨堡垒机" || branding["app_description"] != "内部 SSH 安全控制台" {
		t.Fatalf("settings response missing branding: %+v", settings)
	}
	var providers ProvidersForTest
	getJSON(t, adminClient, srv.URL+"/api/auth/providers", http.StatusOK, &providers)
	if !providers.RegistrationEnabled {
		t.Fatalf("provider response should expose public registration setting: %+v", providers)
	}
	if providers.Branding.AppName != "吉时雨堡垒机" || providers.Branding.AppDescription != "内部 SSH 安全控制台" {
		t.Fatalf("provider response should expose branding: %+v", providers)
	}
	var me apiMeResponse
	getJSON(t, adminClient, srv.URL+"/api/me", http.StatusOK, &me)
	if me.Runtime.AppName != "吉时雨堡垒机" || me.Runtime.AppDescription != "内部 SSH 安全控制台" {
		t.Fatalf("me response should expose branding: %+v", me.Runtime)
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

func TestAPIChangeOwnPassword(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, client, srv.URL, "self-password@example.com")

	putJSON(t, client, srv.URL+"/api/me/password", map[string]string{
		"current_password": "wrong-pass",
		"new_password":     "new-secret-pass",
		"confirm_password": "new-secret-pass",
	}, http.StatusUnauthorized, nil)
	putJSON(t, client, srv.URL+"/api/me/password", map[string]string{
		"current_password": "secret-pass",
		"new_password":     "new-secret-pass",
		"confirm_password": "different-pass",
	}, http.StatusBadRequest, nil)
	putJSON(t, client, srv.URL+"/api/me/password", map[string]string{
		"current_password": "secret-pass",
		"new_password":     "new-secret-pass",
		"confirm_password": "new-secret-pass",
	}, http.StatusOK, nil)

	postJSON(t, client, srv.URL+"/api/auth/logout", nil, http.StatusOK, nil)
	postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{
		"email":    "self-password@example.com",
		"password": "secret-pass",
	}, http.StatusUnauthorized, nil)
	postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{
		"email":    "self-password@example.com",
		"password": "new-secret-pass",
	}, http.StatusOK, nil)

	external, err := app.store.Repository().CreateUser(context.Background(), store.CreateUserParams{
		Email:        "external-self@example.com",
		DisplayName:  "External Self",
		PasswordHash: []byte("hash"),
		AuthProvider: "ldap",
	})
	if err != nil {
		t.Fatal(err)
	}
	externalClient := apiClient(t)
	attachTestSession(t, externalClient, srv.URL, app, external.ID)
	putJSON(t, externalClient, srv.URL+"/api/me/password", map[string]string{
		"current_password": "whatever",
		"new_password":     "new-secret-pass",
		"confirm_password": "new-secret-pass",
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
	postJSON(t, bob, srv.URL+"/api/orgs/"+org.Organization.ID+"/invites", map[string]string{
		"role": "member",
	}, http.StatusForbidden, nil)
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/invites", map[string]string{
		"role": "owner",
	}, http.StatusBadRequest, nil)

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
	var otherOrg apiOrganizationResponse
	postJSON(t, alice, srv.URL+"/api/orgs", map[string]string{
		"name": "Other",
		"slug": "other",
	}, http.StatusCreated, &otherOrg)
	postJSON(t, alice, srv.URL+"/api/orgs/"+otherOrg.Organization.ID+"/groups/"+custom.Group.ID+"/members", map[string]string{
		"user_id": aliceUser.User.ID,
	}, http.StatusBadRequest, nil)

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

func TestAPIOwnerAccessRequiresMembershipAndPolicyAdmin(t *testing.T) {
	srv, alice, _ := newAPITestServer(t)
	defer srv.Close()
	registerForAPI(t, alice, srv.URL, "owner-access@example.com")
	var org apiOrganizationResponse
	postJSON(t, alice, srv.URL+"/api/orgs", map[string]string{"name": "Owner Access", "slug": "owner-access"}, http.StatusCreated, &org)

	bob := apiClient(t)
	bobUser := registerForAPI(t, bob, srv.URL, "member-access@example.com")
	outsider := apiClient(t)
	registerForAPI(t, outsider, srv.URL, "outside-access@example.com")

	getJSON(t, outsider, srv.URL+"/api/targets?owner_type=organization&owner_id="+org.Organization.ID, http.StatusForbidden, nil)
	postJSON(t, alice, srv.URL+"/api/orgs/"+org.Organization.ID+"/members", map[string]string{
		"user_id": bobUser.User.ID,
		"role":    "member",
	}, http.StatusOK, nil)
	var memberTarget apiTargetResponse
	postJSON(t, bob, srv.URL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"name":            "Member service",
		"alias":           "member-service",
		"target_type":     "direct",
		"host":            "127.0.0.1",
		"port":            22,
		"remote_username": "root",
		"auth_type":       "password",
		"secret":          "secret",
	}, http.StatusCreated, &memberTarget)
	if memberTarget.Target.ID == "" {
		t.Fatalf("member should be able to create organization target: %+v", memberTarget)
	}
	postJSON(t, bob, srv.URL+"/api/policies", map[string]any{
		"owner_type":     "organization",
		"owner_id":       org.Organization.ID,
		"name":           "member policy",
		"default_action": "deny",
	}, http.StatusForbidden, nil)
	postJSON(t, bob, srv.URL+"/api/llm-configs", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"name":            "member llm",
		"base_url":        "https://llm.example.com",
		"model":           "model",
		"timeout_seconds": 3,
	}, http.StatusForbidden, nil)
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
	var updatedLLM apiLLMConfigResponse
	patchJSON(t, client, srv.URL+"/api/llm-configs/"+llm.Config.ID, map[string]any{
		"name":            "reviewer edited",
		"base_url":        "https://llm2.example.com/",
		"model":           "ops-model-v2",
		"timeout_seconds": 5,
	}, http.StatusOK, &updatedLLM)
	if updatedLLM.Config.Name != "reviewer edited" ||
		updatedLLM.Config.BaseURL != "https://llm2.example.com" ||
		updatedLLM.Config.Model != "ops-model-v2" ||
		updatedLLM.Config.TimeoutSeconds != 5 {
		t.Fatalf("llm config update mismatch: %+v", updatedLLM)
	}
	llm.Config = updatedLLM.Config
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
	var updatedPrompt apiLLMPromptResponse
	patchJSON(t, client, srv.URL+"/api/llm-prompts/"+prompt.Prompt.ID, map[string]string{
		"title":   "High risk review edited",
		"content": "deny writes and risky shells",
	}, http.StatusOK, &updatedPrompt)
	if updatedPrompt.Prompt.Title != "High risk review edited" || updatedPrompt.Prompt.Content != "deny writes and risky shells" {
		t.Fatalf("prompt update mismatch: %+v", updatedPrompt)
	}
	prompt.Prompt = updatedPrompt.Prompt

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
	if len(target.Target.TagColors) != 2 || target.Target.TagColors["测试环境"] == "" {
		t.Fatalf("target tag colors missing: %+v", target.Target.TagColors)
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
	var edited apiTargetResponse
	patchJSON(t, client, srv.URL+"/api/targets/"+proxied.Target.ID, map[string]any{
		"name":            "Edited private subnet",
		"alias":           "edited-private",
		"host":            "10.0.0.9",
		"port":            2222,
		"remote_username": "ubuntu",
		"auth_type":       "private_key",
		"secret":          "updated-secret",
		"proxy_target_id": "",
		"tags":            []string{"private", "edited"},
	}, http.StatusOK, &edited)
	if edited.Target.Name != "Edited private subnet" ||
		edited.Target.Alias != "edited-private" ||
		edited.Target.Host != "10.0.0.9" ||
		edited.Target.Port != 2222 ||
		edited.Target.RemoteUsername != "ubuntu" ||
		edited.Target.AuthType != "private_key" ||
		edited.Target.ProxyTargetID != "" ||
		len(edited.Target.Tags) != 2 {
		t.Fatalf("target connection edit mismatch: %+v", edited.Target)
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
	patchJSON(t, client, srv.URL+"/api/target-tags", map[string]string{
		"owner_type": "organization",
		"owner_id":   org.Organization.ID,
		"name":       "prod",
		"color":      "green",
	}, http.StatusOK, nil)
	var colored apiTargetsResponse
	getJSON(t, client, srv.URL+"/api/targets?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &colored)
	if len(colored.Targets) < 1 || colored.Targets[0].TagColors["prod"] != "green" {
		t.Fatalf("target tag color update mismatch: %+v", colored)
	}
	var removable apiTargetResponse
	postJSON(t, client, srv.URL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"name":            "Delete target",
		"alias":           "delete-target",
		"target_type":     "direct",
		"host":            "10.0.1.7",
		"port":            22,
		"remote_username": "root",
		"auth_type":       "password",
		"secret":          "secret",
	}, http.StatusCreated, &removable)
	var dependent apiTargetResponse
	postJSON(t, client, srv.URL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        org.Organization.ID,
		"name":            "Proxy dependent",
		"alias":           "proxy-dependent",
		"target_type":     "direct",
		"host":            "10.0.1.8",
		"port":            22,
		"remote_username": "root",
		"auth_type":       "password",
		"secret":          "secret",
		"proxy_target_id": removable.Target.ID,
	}, http.StatusCreated, &dependent)
	deleteJSON(t, client, srv.URL+"/api/targets/"+removable.Target.ID, http.StatusNoContent)
	var afterTargetDelete apiTargetsResponse
	getJSON(t, client, srv.URL+"/api/targets?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &afterTargetDelete)
	for _, item := range afterTargetDelete.Targets {
		if item.ID == removable.Target.ID {
			t.Fatalf("deleted target still listed: %+v", afterTargetDelete)
		}
		if item.ID == dependent.Target.ID && item.ProxyTargetID != "" {
			t.Fatalf("dependent target proxy should be cleared after delete: %+v", item)
		}
	}
	target.Target = renamed.Target

	var policy apiPolicyResponse
	postJSON(t, client, srv.URL+"/api/policies", map[string]any{
		"owner_type":                    "organization",
		"owner_id":                      org.Organization.ID,
		"name":                          "strict",
		"default_action":                "deny",
		"llm_config_id":                 llm.Config.ID,
		"llm_prompt_id":                 prompt.Prompt.ID,
		"ip_allowlist":                  "10.0.0.0/8",
		"allow_interactive":             true,
		"allow_manual_review":           true,
		"manual_review_timeout_seconds": 45,
	}, http.StatusCreated, &policy)
	if policy.Policy.LLMConfigID != llm.Config.ID || policy.Policy.LLMPromptID != prompt.Prompt.ID ||
		policy.Policy.IPAllowlist != "10.0.0.0/8" || !policy.Policy.AllowInteractive || !policy.Policy.AllowManualReview ||
		policy.Policy.ManualReviewTimeoutSeconds != 45 {
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
	if len(listedPolicies.Policies) != 1 ||
		len(listedPolicies.Policies[0].Rules) != 1 ||
		listedPolicies.Policies[0].Rules[0].Pattern != "whoami" ||
		len(listedPolicies.Policies[0].TargetIDs) != 1 ||
		listedPolicies.Policies[0].TargetIDs[0] != target.Target.ID ||
		len(listedPolicies.Policies[0].TargetTags) != 1 ||
		listedPolicies.Policies[0].TargetTags[0] != "prod" ||
		len(listedPolicies.Policies[0].UserGroupIDs) != 1 ||
		listedPolicies.Policies[0].UserGroupIDs[0] != groups.Groups[0].ID {
		t.Fatalf("policy bindings and rules mismatch: %+v", listedPolicies)
	}
	var updatedPolicy apiPolicyResponse
	patchJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID, map[string]any{
		"name":                          "strict edited",
		"default_action":                "allow",
		"llm_config_id":                 llm.Config.ID,
		"llm_prompt_id":                 prompt.Prompt.ID,
		"ip_allowlist":                  "private",
		"allow_port_forward":            true,
		"allow_upload":                  true,
		"allow_download":                false,
		"allow_interactive":             true,
		"allow_manual_review":           true,
		"manual_review_timeout_seconds": 12,
	}, http.StatusOK, &updatedPolicy)
	if updatedPolicy.Policy.Name != "strict edited" || updatedPolicy.Policy.DefaultAction != "allow" ||
		updatedPolicy.Policy.IPAllowlist != "private" || !updatedPolicy.Policy.AllowPortForward ||
		!updatedPolicy.Policy.AllowUpload || updatedPolicy.Policy.AllowDownload || !updatedPolicy.Policy.AllowInteractive ||
		!updatedPolicy.Policy.AllowManualReview || updatedPolicy.Policy.ManualReviewTimeoutSeconds != 12 {
		t.Fatalf("policy update mismatch: %+v", updatedPolicy.Policy)
	}
	var copiedPolicy apiPolicyResponse
	postJSON(t, client, srv.URL+"/api/policies/"+policy.Policy.ID+"/copy", map[string]string{
		"name": "strict copy",
	}, http.StatusCreated, &copiedPolicy)
	if copiedPolicy.Policy.Name != "strict copy" || len(copiedPolicy.Policy.Rules) != 1 || len(copiedPolicy.Policy.TargetIDs) != 1 ||
		len(copiedPolicy.Policy.TargetTags) != 1 || len(copiedPolicy.Policy.UserGroupIDs) != 1 ||
		copiedPolicy.Policy.IPAllowlist != "private" || !copiedPolicy.Policy.AllowUpload || !copiedPolicy.Policy.AllowManualReview ||
		copiedPolicy.Policy.ManualReviewTimeoutSeconds != 12 {
		t.Fatalf("policy copy mismatch: %+v", copiedPolicy.Policy)
	}
	deleteJSON(t, client, srv.URL+"/api/policies/"+copiedPolicy.Policy.ID+"/rules/"+copiedPolicy.Policy.Rules[0].ID, http.StatusOK)
	deleteJSON(t, client, srv.URL+"/api/policies/"+copiedPolicy.Policy.ID+"/targets/"+target.Target.ID, http.StatusOK)
	deleteJSON(t, client, srv.URL+"/api/policies/"+copiedPolicy.Policy.ID+"/target-tags/prod", http.StatusOK)
	deleteJSON(t, client, srv.URL+"/api/policies/"+copiedPolicy.Policy.ID+"/user-groups/"+groups.Groups[0].ID, http.StatusOK)
	getJSON(t, client, srv.URL+"/api/policies?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &listedPolicies)
	for _, listedPolicy := range listedPolicies.Policies {
		if listedPolicy.ID == copiedPolicy.Policy.ID &&
			(len(listedPolicy.Rules) != 0 ||
				len(listedPolicy.TargetIDs) != 0 ||
				len(listedPolicy.TargetTags) != 0 ||
				len(listedPolicy.UserGroupIDs) != 0) {
			t.Fatalf("policy detach mismatch: %+v", listedPolicy)
		}
	}
	deleteJSON(t, client, srv.URL+"/api/policies/"+copiedPolicy.Policy.ID, http.StatusOK)
	getJSON(t, client, srv.URL+"/api/policies?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &listedPolicies)
	if len(listedPolicies.Policies) != 1 || listedPolicies.Policies[0].ID != policy.Policy.ID {
		t.Fatalf("policy delete mismatch: %+v", listedPolicies)
	}
	deleteJSON(t, client, srv.URL+"/api/llm-configs/"+llm.Config.ID, http.StatusOK)
	deleteJSON(t, client, srv.URL+"/api/llm-prompts/"+prompt.Prompt.ID, http.StatusOK)
	getJSON(t, client, srv.URL+"/api/llm-configs?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &llms)
	if len(llms.Configs) != 0 {
		t.Fatalf("deleted llm config still listed: %+v", llms)
	}
	getJSON(t, client, srv.URL+"/api/llm-prompts?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &prompts)
	if len(prompts.Prompts) != 1 || !prompts.Prompts[0].IsReadonly {
		t.Fatalf("deleted custom prompt should leave only readonly default prompt: %+v", prompts)
	}
	directPolicy, err := app.store.Repository().GetCommandPolicy(contextBackground(), policy.Policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if directPolicy.LLMConfigID != "" || directPolicy.LLMPromptID != "" {
		t.Fatalf("direct policy resource delete should clear references: %+v", directPolicy)
	}
	getJSON(t, client, srv.URL+"/api/policies?owner_type=organization&owner_id="+org.Organization.ID, http.StatusOK, &listedPolicies)
	if len(listedPolicies.Policies) != 1 || listedPolicies.Policies[0].LLMConfigID != "" || listedPolicies.Policies[0].LLMPromptID != "" {
		t.Fatalf("policy resource delete should clear references: %+v", listedPolicies)
	}

	started := time.Now().UTC().Add(-2 * time.Hour)
	audit, err := app.createAuditLog(contextBackground(), store.CreateCommandAuditLogParams{
		UserID:               user.User.ID,
		TargetID:             target.Target.ID,
		OrganizationID:       org.Organization.ID,
		PublicKeyFingerprint: "SHA256:audit-key",
		SessionID:            "session-1",
		Command:              "whoami",
		RequestType:          store.RequestExec,
		PolicyDecision:       store.DecisionAllow,
		PolicyReason:         "whitelist",
		ExitCode:             intPtr(0),
		StartedAt:            started,
		RemoteAddress:        "127.0.0.1:12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	storeTarget, err := app.store.Repository().GetSSHTarget(contextBackground(), target.Target.ID)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := newTerminalRecorder(app.auditRecordingsPath, "session-shell", 100, 30, storeTarget)
	if err != nil {
		t.Fatal(err)
	}
	recorder.WriteOutput([]byte("hello from shell\r\n"))
	recording, err := recorder.Close()
	if err != nil {
		t.Fatal(err)
	}
	shellAudit, err := app.createAuditLog(contextBackground(), store.CreateCommandAuditLogParams{
		UserID:               user.User.ID,
		TargetID:             target.Target.ID,
		OrganizationID:       org.Organization.ID,
		PublicKeyFingerprint: "SHA256:audit-key",
		SessionID:            "session-shell",
		Command:              "interactive terminal",
		RequestType:          store.RequestShell,
		PolicyDecision:       store.DecisionAllow,
		PolicyReason:         "policy capability allowed: strict edited",
		StartedAt:            started.Add(time.Hour),
		RemoteAddress:        "127.0.0.1:12345",
		RecordingPath:        recording.RelativePath,
		RecordingSize:        recording.Size,
		RecordingSHA256:      recording.SHA256,
		RecordingDurationMS:  recording.DurationMS,
		RecordingWidth:       recording.Width,
		RecordingHeight:      recording.Height,
	})
	if err != nil {
		t.Fatal(err)
	}
	denyAudit, err := app.createAuditLog(contextBackground(), store.CreateCommandAuditLogParams{
		UserID:               user.User.ID,
		TargetID:             target.Target.ID,
		OrganizationID:       org.Organization.ID,
		PublicKeyFingerprint: "SHA256:audit-key",
		SessionID:            "session-deny",
		Command:              "mkdir test",
		RequestType:          store.RequestExec,
		PolicyDecision:       store.DecisionDeny,
		PolicyReason:         "whitelist missing: ls",
		StartedAt:            started.Add(2 * time.Hour),
		RemoteAddress:        "127.0.0.1:12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	bobClient := apiClient(t)
	bobUser := registerForAPI(t, bobClient, srv.URL, "audit-bob@example.com")
	if err := app.store.Repository().AddOrganizationMember(contextBackground(), org.Organization.ID, bobUser.User.ID, store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	bobAudit, err := app.createAuditLog(contextBackground(), store.CreateCommandAuditLogParams{
		UserID:               bobUser.User.ID,
		TargetID:             target.Target.ID,
		OrganizationID:       org.Organization.ID,
		PublicKeyFingerprint: "SHA256:bob-audit-key",
		SessionID:            "session-bob",
		Command:              "id",
		RequestType:          store.RequestExec,
		PolicyDecision:       store.DecisionAllow,
		PolicyReason:         "whitelist",
		ExitCode:             intPtr(0),
		StartedAt:            started.Add(3 * time.Hour),
		RemoteAddress:        "127.0.0.1:12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs apiAuditLogsResponse
	getJSON(t, client, srv.URL+"/api/audit", http.StatusOK, &logs)
	if len(logs.Logs) != 3 || logs.Total != 3 {
		t.Fatalf("audit logs mismatch: %+v", logs)
	}
	var orgLogs apiAuditLogsResponse
	getJSON(t, client, srv.URL+"/api/audit?organization_id="+org.Organization.ID, http.StatusOK, &orgLogs)
	if len(orgLogs.Logs) != 4 || orgLogs.Total != 4 {
		t.Fatalf("organization owner should see organization audit logs: %+v", orgLogs)
	}
	var bobAdminLogs apiAuditLogsResponse
	getJSON(t, bobClient, srv.URL+"/api/audit?organization_id="+org.Organization.ID, http.StatusOK, &bobAdminLogs)
	if len(bobAdminLogs.Logs) != 4 || bobAdminLogs.Total != 4 {
		t.Fatalf("organization admin should see organization audit logs: %+v", bobAdminLogs)
	}
	var filteredLogs apiAuditLogsResponse
	getJSON(t, client, srv.URL+"/api/audit?query=whoami&page=1&page_size=1&started_from="+url.QueryEscape(started.Format(time.RFC3339))+"&started_to="+url.QueryEscape(started.Add(30*time.Minute).Format(time.RFC3339)), http.StatusOK, &filteredLogs)
	if len(filteredLogs.Logs) != 1 || filteredLogs.Total != 1 || filteredLogs.Logs[0].ID != audit.ID || filteredLogs.Logs[0].Command != "whoami" {
		t.Fatalf("audit filtered logs mismatch: %+v", filteredLogs)
	}
	if filteredLogs.Logs[0].UserEmail != user.User.Email ||
		filteredLogs.Logs[0].UserDisplayName != user.User.DisplayName ||
		filteredLogs.Logs[0].PublicKeyFingerprint != "SHA256:audit-key" ||
		filteredLogs.Logs[0].TargetName != target.Target.Name ||
		filteredLogs.Logs[0].TargetAlias != target.Target.Alias ||
		filteredLogs.Logs[0].TargetEndpoint != "root@127.0.0.1:22" {
		t.Fatalf("audit log enriched fields mismatch: %+v", filteredLogs.Logs[0])
	}
	var deniedLogs apiAuditLogsResponse
	getJSON(t, client, srv.URL+"/api/audit?decision=deny", http.StatusOK, &deniedLogs)
	if len(deniedLogs.Logs) != 1 || deniedLogs.Total != 1 || deniedLogs.Logs[0].ID != denyAudit.ID || deniedLogs.Logs[0].PolicyDecision != store.DecisionDeny {
		t.Fatalf("audit decision filter mismatch: %+v", deniedLogs)
	}
	var shellLogs apiAuditLogsResponse
	getJSON(t, client, srv.URL+"/api/audit?request_type=shell", http.StatusOK, &shellLogs)
	if len(shellLogs.Logs) != 1 || shellLogs.Total != 1 || shellLogs.Logs[0].ID != shellAudit.ID || shellLogs.Logs[0].RequestType != store.RequestShell {
		t.Fatalf("audit request type filter mismatch: %+v", shellLogs)
	}
	var replay struct {
		Log   apiAuditLog       `json:"log"`
		Lines []json.RawMessage `json:"lines"`
	}
	getJSON(t, client, srv.URL+"/api/audit/"+shellAudit.ID+"/recording", http.StatusOK, &replay)
	if replay.Log.ID != shellAudit.ID || !replay.Log.HasRecording || replay.Log.RecordingWidth != 100 || len(replay.Lines) < 2 {
		t.Fatalf("audit replay mismatch: %+v", replay)
	}
	getJSON(t, bobClient, srv.URL+"/api/audit/"+shellAudit.ID+"/recording", http.StatusOK, &replay)
	if replay.Log.ID != shellAudit.ID {
		t.Fatalf("organization admin should replay organization audit recording: %+v", replay)
	}
	if err := app.store.Repository().AddOrganizationMember(contextBackground(), org.Organization.ID, bobUser.User.ID, store.RoleMember); err != nil {
		t.Fatal(err)
	}
	var bobMemberLogs apiAuditLogsResponse
	getJSON(t, bobClient, srv.URL+"/api/audit?organization_id="+org.Organization.ID, http.StatusOK, &bobMemberLogs)
	if len(bobMemberLogs.Logs) != 1 || bobMemberLogs.Total != 1 || bobMemberLogs.Logs[0].ID != bobAudit.ID {
		t.Fatalf("organization member should only see own audit logs: %+v", bobMemberLogs)
	}
	getJSON(t, bobClient, srv.URL+"/api/audit/"+shellAudit.ID+"/recording", http.StatusForbidden, nil)
}

func TestAPIAgentEnrollmentReturnsInstallScripts(t *testing.T) {
	srv, client, app := newAPITestServer(t)
	app.cfg.PublicSSHPort = 22022
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
	if !strings.Contains(enrollment.ServiceSH, "sudo sh") || !strings.Contains(enrollment.ServiceSH, " install") {
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
	if !strings.Contains(shBody, ".sha256") || !strings.Contains(shBody, "sha256sum -c") {
		t.Fatalf("shell install script missing checksum verification:\n%s", shBody)
	}
	if !strings.Contains(shBody, `--ssh-port "22022"`) {
		t.Fatalf("shell install script missing public ssh port hint:\n%s", shBody)
	}
	if !strings.Contains(shBody, `mkdir -p /var/lib/gosshd`) || !strings.Contains(shBody, `--id-file "/var/lib/gosshd/agent.json"`) || !strings.Contains(shBody, `--root "/root"`) {
		t.Fatalf("shell service install script should not depend on systemd HOME:\n%s", shBody)
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
	if !strings.Contains(psBody, "New-Service -Name $ServiceName") || !strings.Contains(psBody, "sc.exe start $serviceName") {
		t.Fatalf("powershell install script missing service install flow:\n%s", psBody)
	}
	if !strings.Contains(psBody, ".sha256") || !strings.Contains(psBody, "Get-FileHash") {
		t.Fatalf("powershell install script missing checksum verification:\n%s", psBody)
	}
	if strings.Contains(psBody, "$quote") || strings.Contains(psBody, "$quotehttp") {
		t.Fatalf("powershell install script should not concatenate quote variables with URLs:\n%s", psBody)
	}
	if !strings.Contains(psBody, `$server = "`) || !strings.Contains(psBody, `& $tmp --server $server --enrollment-token $enrollmentToken`) {
		t.Fatalf("powershell install script missing safe server/token variables:\n%s", psBody)
	}
	if !strings.Contains(psBody, `$sshPort = "22022"`) || !strings.Contains(psBody, `--ssh-port $sshPort`) {
		t.Fatalf("powershell install script missing public ssh port hint:\n%s", psBody)
	}
	if !strings.Contains(psBody, `$serviceIDFile = Join-Path $targetDir "agent.json"`) ||
		!strings.Contains(psBody, "--id-file \"' + $serviceIDFile + '\" --root \"") ||
		!strings.Contains(psBody, "obj= LocalSystem") ||
		!strings.Contains(psBody, "StartName") {
		t.Fatalf("powershell service install should run independently from the login session:\n%s", psBody)
	}
	if strings.Contains(psBody, "Get-Service") || !strings.Contains(psBody, "Get-CimInstance -ClassName Win32_Service") {
		t.Fatalf("powershell install script should use CIM service checks instead of Get-Service:\n%s", psBody)
	}
	if !strings.Contains(psBody, "failed to create $ServiceName service") || !strings.Contains(psBody, "failed to start $serviceName service") {
		t.Fatalf("powershell install script should surface service failures:\n%s", psBody)
	}
	if !strings.Contains(psBody, "/download/winpty/windows/amd64") {
		t.Fatalf("powershell install script should download winpty through gosshd server:\n%s", psBody)
	}
	if strings.Contains(psBody, "github.com/rprichard/winpty/releases/download") {
		t.Fatalf("powershell install script should not require direct GitHub access for winpty:\n%s", psBody)
	}
	if strings.Contains(psBody, "winpty install skipped") {
		t.Fatalf("powershell install script should fail visibly when winpty install fails:\n%s", psBody)
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
	enablePublicRegistrationForTest(t, app)
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		if app.store != nil {
			app.Close()
		}
	})
	return srv, apiClient(t), app
}

type ProvidersForTest struct {
	RegistrationEnabled bool `json:"registration_enabled"`
	Branding            struct {
		AppName        string `json:"app_name"`
		AppDescription string `json:"app_description"`
	} `json:"branding"`
}

func enablePublicRegistrationForTest(t *testing.T, app *App) {
	t.Helper()
	ctx := context.Background()
	if err := app.ensureServices(ctx); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(authSettings{PublicRegistration: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Repository().UpsertSystemSetting(ctx, settingAuth, payload, "test"); err != nil {
		t.Fatal(err)
	}
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

func attachTestSession(t *testing.T, client *http.Client, baseURL string, app *App, userID string) {
	t.Helper()
	token := "test-session-" + userID
	sum := sha256.Sum256([]byte(token))
	if _, err := app.store.Repository().CreateSession(context.Background(), userID, sum[:], time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar.SetCookies(parsed, []*http.Cookie{{Name: app.sessionCookieName(), Value: token}})
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

func deleteJSON(t *testing.T, client *http.Client, url string, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("DELETE %s status mismatch: got %d want %d", url, resp.StatusCode, wantStatus)
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
