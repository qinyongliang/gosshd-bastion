package store

import (
	"context"
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
		"policy_user_groups",
		"llm_policy_configs",
		"llm_prompt_resources",
		"command_audit_logs",
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
		Alias:           "test2",
		TargetType:      TargetDirect,
		Host:            "127.0.0.1",
		Port:            22,
		RemoteUsername:  "root",
		AuthType:        AuthPassword,
		EncryptedSecret: []byte("secret"),
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

	policy, err := repo.CreateCommandPolicy(ctx, CreateCommandPolicyParams{
		OwnerType:     OwnerOrganization,
		OwnerID:       personal.ID,
		Name:          "strict",
		DefaultAction: DecisionDeny,
		LLMConfigID:   llm.ID,
		LLMPromptID:   prompts[0].ID,
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
	if policies[0].LLMConfigID != llm.ID {
		t.Fatalf("policy llm config mismatch: %#v", policies[0])
	}
	if policies[0].LLMPromptID != prompts[0].ID {
		t.Fatalf("policy llm prompt mismatch: %#v", policies[0])
	}

	started := time.Now().UTC()
	ended := started.Add(time.Second)
	audit, err := repo.CreateCommandAuditLog(ctx, CreateCommandAuditLogParams{
		UserID:         user.ID,
		TargetID:       target.ID,
		SessionID:      "session-1",
		Command:        "whoami",
		RequestType:    RequestExec,
		PolicyDecision: DecisionAllow,
		PolicyReason:   "whitelist",
		ExitCode:       intPtr(0),
		StartedAt:      started,
		EndedAt:        &ended,
		RemoteAddress:  "127.0.0.1:12345",
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
}

func intPtr(v int) *int {
	return &v
}
