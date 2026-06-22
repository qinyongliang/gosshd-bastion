package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAppliesBastionSchema(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rows, err := st.DB().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"users",
		"sessions",
		"organizations",
		"organization_members",
		"organization_user_groups",
		"organization_user_group_members",
		"organization_invites",
		"user_public_keys",
		"ssh_targets",
		"agent_enrollments",
		"agents",
		"command_policies",
		"policy_rules",
		"policy_targets",
		"policy_target_tags",
		"policy_user_groups",
		"llm_policy_configs",
		"llm_prompt_resources",
		"command_audit_logs",
		"external_identities",
		"system_settings",
		"oauth_states",
	} {
		if !got[table] {
			t.Fatalf("schema missing table %s; got %#v", table, got)
		}
	}
}

func TestRepositoryCreatesUserOrganizationKeyTargetPolicyAndAudit(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()

	user, err := repo.CreateUser(ctx, CreateUserParams{
		Email:        "alice@example.com",
		DisplayName:  "Alice",
		PasswordHash: []byte("hash"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" {
		t.Fatalf("user id is empty")
	}

	org, err := repo.CreateOrganization(ctx, CreateOrganizationParams{
		Name:        "Ops",
		Slug:        "ops",
		OwnerUserID: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := repo.GetOrganizationMember(ctx, org.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != RoleOwner {
		t.Fatalf("owner role mismatch: got %q", member.Role)
	}
	personal, err := repo.GetPersonalOrganizationForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !personal.IsPersonal {
		t.Fatalf("personal organization missing: %#v", personal)
	}
	personalPrompts, err := repo.ListLLMPromptResources(ctx, OwnerOrganization, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(personalPrompts) != 1 || !personalPrompts[0].IsDefault || !personalPrompts[0].IsReadonly {
		t.Fatalf("personal default prompt missing: %#v", personalPrompts)
	}
	groups, err := repo.ListOrganizationUserGroups(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || !groups[0].IsDefault {
		t.Fatalf("default group missing: %#v", groups)
	}
	inGroup, err := repo.UserInGroup(ctx, groups[0].ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !inGroup {
		t.Fatalf("organization creator is not in default user group")
	}

	key, err := repo.CreatePublicKey(ctx, CreatePublicKeyParams{
		UserID:        user.ID,
		Name:          "laptop",
		AuthorizedKey: "ssh-rsa AAAATEST alice@example.com",
		Fingerprint:   "SHA256:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	lookupUser, err := repo.GetUserByPublicKeyFingerprint(ctx, "SHA256:test")
	if err != nil {
		t.Fatal(err)
	}
	if lookupUser.ID != user.ID || key.UserID != user.ID {
		t.Fatalf("public key user mismatch")
	}

	target, err := repo.CreateSSHTarget(ctx, CreateSSHTargetParams{
		OwnerType:       OwnerOrganization,
		OwnerID:         personal.ID,
		Name:            "Test service",
		Alias:           "test2",
		TargetType:      TargetDirect,
		Host:            "127.0.0.1",
		Port:            22,
		RemoteUsername:  "root",
		AuthType:        AuthPassword,
		EncryptedSecret: []byte("secret"),
		Tags:            []string{"测试环境", "db", "测试环境"},
		CreatedBy:       user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	personalTargets, err := repo.ListSSHTargets(ctx, OwnerOrganization, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(personalTargets) != 1 || personalTargets[0].ID != target.ID {
		t.Fatalf("personal target mismatch: %#v", personalTargets)
	}
	if personalTargets[0].Name != "Test service" || len(personalTargets[0].Tags) != 2 {
		t.Fatalf("target name/tags mismatch: %#v", personalTargets[0])
	}
	if len(personalTargets[0].TagColors) != 2 {
		t.Fatalf("target tag colors missing: %#v", personalTargets[0].TagColors)
	}
	if _, err := normalizeTargetTagColor(personalTargets[0].TagColors["测试环境"]); err != nil {
		t.Fatalf("target tag color is not from fixed palette: %#v", personalTargets[0].TagColors)
	}
	filteredTargets, err := repo.ListSSHTargetsFiltered(ctx, SSHTargetFilter{
		OwnerType: OwnerOrganization,
		OwnerID:   personal.ID,
		Tags:      []string{"测试环境"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredTargets) != 1 || filteredTargets[0].ID != target.ID {
		t.Fatalf("tag-filtered targets mismatch: %#v", filteredTargets)
	}
	updatedTarget, err := repo.UpdateSSHTarget(ctx, target.ID, UpdateSSHTargetParams{
		Name:        "Renamed service",
		Tags:        []string{"prod"},
		ReplaceTags: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedTarget.Name != "Renamed service" || len(updatedTarget.Tags) != 1 || updatedTarget.Tags[0] != "prod" {
		t.Fatalf("updated target name/tags mismatch: %#v", updatedTarget)
	}
	if err := repo.UpdateTargetTagColor(ctx, OwnerOrganization, personal.ID, "prod", "blue"); err != nil {
		t.Fatal(err)
	}
	coloredTarget, err := repo.GetSSHTarget(ctx, updatedTarget.ID)
	if err != nil {
		t.Fatal(err)
	}
	if coloredTarget.TagColors["prod"] != "blue" {
		t.Fatalf("updated target tag color mismatch: %#v", coloredTarget.TagColors)
	}
	if err := repo.UpdateTargetTagColor(ctx, OwnerOrganization, personal.ID, "prod", "infrared"); err == nil {
		t.Fatal("expected invalid tag color to fail")
	}
	target = updatedTarget

	llm, err := repo.CreateLLMPolicyConfig(ctx, CreateLLMPolicyConfigParams{
		OwnerType:       OwnerOrganization,
		OwnerID:         personal.ID,
		Name:            "reviewer",
		BaseURL:         "https://llm.example.com/",
		EncryptedAPIKey: []byte("key"),
		Model:           "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if llm.BaseURL != "https://llm.example.com" || llm.TimeoutSeconds != 10 {
		t.Fatalf("llm config defaults mismatch: %#v", llm)
	}
	llmConfigs, err := repo.ListLLMPolicyConfigs(ctx, OwnerOrganization, personal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(llmConfigs) != 1 || llmConfigs[0].ID != llm.ID {
		t.Fatalf("llm config list mismatch: %#v", llmConfigs)
	}
	prompts, err := repo.ListLLMPromptResources(ctx, OwnerOrganization, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !prompts[0].IsReadonly {
		t.Fatalf("organization default prompt mismatch: %#v", prompts)
	}
	customPrompt, err := repo.CreateLLMPromptResource(ctx, CreateLLMPromptResourceParams{
		OwnerType: OwnerOrganization,
		OwnerID:   personal.ID,
		Title:     "custom review",
		Content:   "deny risky changes",
	})
	if err != nil {
		t.Fatal(err)
	}

	policy, err := repo.CreateCommandPolicy(ctx, CreateCommandPolicyParams{
		OwnerType:     OwnerOrganization,
		OwnerID:       personal.ID,
		Name:          "strict",
		DefaultAction: DecisionDeny,
		LLMConfigID:   llm.ID,
		LLMPromptID:   customPrompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := repo.CreatePolicyRule(ctx, CreatePolicyRuleParams{
		PolicyID:    policy.ID,
		RuleType:    RuleWhitelist,
		PatternType: PatternExact,
		Pattern:     "whoami",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTargetTag(ctx, policy.ID, OwnerOrganization, personal.ID, "prod"); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}
	policies, err := repo.ListPoliciesForTarget(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || len(policies[0].Rules) != 1 || policies[0].Rules[0].ID != rule.ID || len(policies[0].UserGroupIDs) != 1 {
		t.Fatalf("policy attachment mismatch: %#v", policies)
	}
	if len(policies[0].TargetTags) != 1 || policies[0].TargetTags[0] != "prod" {
		t.Fatalf("policy target tag mismatch: %#v", policies[0])
	}
	if policies[0].LLMConfigID != llm.ID {
		t.Fatalf("policy llm config mismatch: %#v", policies[0])
	}
	if policies[0].LLMPromptID != customPrompt.ID {
		t.Fatalf("policy llm prompt mismatch: %#v", policies[0])
	}
	if _, err := repo.UpdateCommandPolicy(ctx, policy.ID, UpdateCommandPolicyParams{
		Name:             "strict edited",
		DefaultAction:    DecisionAllow,
		LLMConfigID:      llm.ID,
		LLMPromptID:      customPrompt.ID,
		IPAllowlist:      "private",
		AllowPortForward: true,
		AllowUpload:      true,
		AllowDownload:    false,
		AllowInteractive: true,
	}); err != nil {
		t.Fatal(err)
	}
	deletedTarget, err := repo.CreateSSHTarget(ctx, CreateSSHTargetParams{
		OwnerType:      OwnerOrganization,
		OwnerID:        personal.ID,
		Name:           "Delete me",
		Alias:          "delete-me",
		TargetType:     TargetDirect,
		Host:           "10.0.0.7",
		Port:           22,
		RemoteUsername: "root",
		AuthType:       AuthPassword,
		CreatedBy:      user.ID,
		Tags:           []string{"trash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyDependent, err := repo.CreateSSHTarget(ctx, CreateSSHTargetParams{
		OwnerType:      OwnerOrganization,
		OwnerID:        personal.ID,
		Name:           "Proxy dependent",
		Alias:          "proxy-dependent",
		TargetType:     TargetDirect,
		Host:           "10.0.0.8",
		Port:           22,
		RemoteUsername: "root",
		AuthType:       AuthPassword,
		ProxyTargetID:  deletedTarget.ID,
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, deletedTarget.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSSHTarget(ctx, deletedTarget.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSSHTarget(ctx, deletedTarget.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted target lookup error mismatch: %v", err)
	}
	proxyDependent, err = repo.GetSSHTarget(ctx, proxyDependent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proxyDependent.ProxyTargetID != "" {
		t.Fatalf("deleted proxy target should be cleared from dependents: %#v", proxyDependent)
	}
	cleanedPolicy, err := repo.GetCommandPolicy(ctx, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, targetID := range cleanedPolicy.TargetIDs {
		if targetID == deletedTarget.ID {
			t.Fatalf("deleted target should be removed from policy bindings: %#v", cleanedPolicy.TargetIDs)
		}
	}
	if err := repo.DeleteLLMPolicyConfig(ctx, llm.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteLLMPromptResource(ctx, customPrompt.ID); err != nil {
		t.Fatal(err)
	}
	clearedPolicy, err := repo.GetCommandPolicy(ctx, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if clearedPolicy.LLMConfigID != "" || clearedPolicy.LLMPromptID != "" {
		t.Fatalf("deleted policy resources should clear references: %#v", clearedPolicy)
	}

	started := time.Now().UTC()
	ended := started.Add(time.Second)
	audit, err := repo.CreateCommandAuditLog(ctx, CreateCommandAuditLogParams{
		UserID:               user.ID,
		TargetID:             target.ID,
		PublicKeyFingerprint: key.Fingerprint,
		SessionID:            "session-1",
		Command:              "whoami",
		RequestType:          RequestExec,
		PolicyDecision:       DecisionAllow,
		PolicyReason:         "whitelist",
		ExitCode:             intPtr(0),
		StartedAt:            started,
		EndedAt:              &ended,
		RemoteAddress:        "127.0.0.1:12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	logs, err := repo.ListCommandAuditLogs(ctx, AuditLogFilter{UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != audit.ID || logs[0].Command != "whoami" {
		t.Fatalf("audit log mismatch: %#v", logs)
	}
	if logs[0].UserEmail != user.Email ||
		logs[0].UserDisplayName != user.DisplayName ||
		logs[0].PublicKeyFingerprint != key.Fingerprint ||
		logs[0].PublicKeyName != key.Name ||
		logs[0].TargetName != target.Name ||
		logs[0].TargetAlias != target.Alias ||
		logs[0].TargetHost != target.Host ||
		logs[0].TargetPort != target.Port ||
		logs[0].TargetUsername != target.RemoteUsername {
		t.Fatalf("audit log enriched fields mismatch: %#v", logs[0])
	}
}

func intPtr(v int) *int {
	return &v
}

func TestRepositoryEnsuresBootstrapAdmin(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()

	admin, createdPassword, err := repo.EnsureBootstrapAdmin(ctx, "admin-pass")
	if err != nil {
		t.Fatal(err)
	}
	if createdPassword != "admin-pass" {
		t.Fatalf("bootstrap password mismatch: %q", createdPassword)
	}
	if admin.Email != "admin" || !admin.IsSystemAdmin || admin.AuthProvider != "local" || len(admin.PasswordHash) == 0 {
		t.Fatalf("admin mismatch: %#v", admin)
	}
	personal, err := repo.GetPersonalOrganizationForUser(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !personal.IsPersonal {
		t.Fatalf("admin personal organization missing: %#v", personal)
	}

	again, createdPassword, err := repo.EnsureBootstrapAdmin(ctx, "new-pass")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != admin.ID || createdPassword != "" {
		t.Fatalf("existing admin should be reused without password echo: %#v %q", again, createdPassword)
	}
}

func TestRepositorySystemSettingsExternalIdentityAndOAuthState(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()

	user, err := repo.CreateUser(ctx, CreateUserParams{
		Email:        "dora@example.com",
		DisplayName:  "Dora",
		PasswordHash: []byte("hash"),
		AuthProvider: "local",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.UpsertSystemSetting(ctx, "dingtalk", []byte(`{"enabled":true}`), user.ID); err != nil {
		t.Fatal(err)
	}
	setting, err := repo.GetSystemSetting(ctx, "dingtalk")
	if err != nil {
		t.Fatal(err)
	}
	if string(setting.ValueJSON) != `{"enabled":true}` || setting.UpdatedBy != user.ID {
		t.Fatalf("setting mismatch: %#v", setting)
	}

	identity, err := repo.CreateExternalIdentity(ctx, CreateExternalIdentityParams{
		UserID:         user.ID,
		Provider:       "dingtalk",
		Subject:        "union-1",
		Email:          "dora@example.com",
		DisplayName:    "Dora Ding",
		RawProfileJSON: `{"unionid":"union-1"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := repo.GetExternalIdentity(ctx, "dingtalk", "union-1")
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != identity.ID || found.UserID != user.ID || found.Email != "dora@example.com" {
		t.Fatalf("identity mismatch: %#v", found)
	}

	expires := time.Now().UTC().Add(time.Hour)
	if err := repo.CreateOAuthState(ctx, "dingtalk", []byte("state-hash"), "/targets", expires); err != nil {
		t.Fatal(err)
	}
	state, err := repo.ConsumeOAuthState(ctx, "dingtalk", []byte("state-hash"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Provider != "dingtalk" || state.RedirectAfter != "/targets" {
		t.Fatalf("oauth state mismatch: %#v", state)
	}
	if _, err := repo.ConsumeOAuthState(ctx, "dingtalk", []byte("state-hash")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected state to be single-use, got %v", err)
	}

	if err := repo.CreateOAuthState(ctx, "dingtalk", []byte("expired"), "/", time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumeOAuthState(ctx, "dingtalk", []byte("expired")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired state to be rejected, got %v", err)
	}
}

func TestRepositoryOrganizationMembersRolesAndOwnerTransfer(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()

	alice, err := repo.CreateUser(ctx, CreateUserParams{Email: "alice@example.com", DisplayName: "Alice", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := repo.CreateUser(ctx, CreateUserParams{Email: "bob@example.com", DisplayName: "Bob", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, CreateOrganizationParams{Name: "Ops", Slug: "ops-transfer", OwnerUserID: alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AddOrganizationMember(ctx, org.ID, bob.ID, RoleMember); err != nil {
		t.Fatal(err)
	}
	members, err := repo.ListOrganizationMembers(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("member list mismatch: %#v", members)
	}
	if err := repo.UpdateOrganizationMemberRole(ctx, org.ID, bob.ID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	bobMember, err := repo.GetOrganizationMember(ctx, org.ID, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobMember.Role != RoleAdmin {
		t.Fatalf("bob role mismatch: %#v", bobMember)
	}
	if err := repo.TransferOrganizationOwner(ctx, org.ID, bob.ID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	org, err = repo.GetOrganization(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if org.OwnerUserID != bob.ID {
		t.Fatalf("owner id not transferred: %#v", org)
	}
	aliceMember, err := repo.GetOrganizationMember(ctx, org.ID, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	bobMember, err = repo.GetOrganizationMember(ctx, org.ID, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if aliceMember.Role != RoleAdmin || bobMember.Role != RoleOwner {
		t.Fatalf("roles after transfer mismatch: alice=%#v bob=%#v", aliceMember, bobMember)
	}
	defaultGroup, err := repo.GetDefaultOrganizationUserGroup(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{alice.ID, bob.ID} {
		inGroup, err := repo.UserInGroup(ctx, defaultGroup.ID, userID)
		if err != nil {
			t.Fatal(err)
		}
		if !inGroup {
			t.Fatalf("user %s missing from default group", userID)
		}
	}

	personal, err := repo.GetPersonalOrganizationForUser(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.TransferOrganizationOwner(ctx, personal.ID, bob.ID, RoleAdmin); err == nil {
		t.Fatalf("expected personal organization transfer to fail")
	}
	if err := repo.RemoveOrganizationMember(ctx, org.ID, bob.ID); err == nil {
		t.Fatalf("expected removing current owner to fail")
	}
}
