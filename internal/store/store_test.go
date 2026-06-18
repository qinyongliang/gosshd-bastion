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
		"organization_invites",
		"user_public_keys",
		"ssh_targets",
		"agent_enrollments",
		"agents",
		"command_policies",
		"policy_rules",
		"policy_targets",
		"llm_policy_configs",
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
		OwnerType:       OwnerUser,
		OwnerID:         user.ID,
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
	resolved, err := repo.ResolveUserTarget(ctx, user.ID, "test2")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != target.ID {
		t.Fatalf("resolved target mismatch: got %s want %s", resolved.ID, target.ID)
	}

	policy, err := repo.CreateCommandPolicy(ctx, CreateCommandPolicyParams{
		OwnerType:     OwnerUser,
		OwnerID:       user.ID,
		Name:          "strict",
		DefaultAction: DecisionDeny,
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
	policies, err := repo.ListPoliciesForTarget(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || len(policies[0].Rules) != 1 || policies[0].Rules[0].ID != rule.ID {
		t.Fatalf("policy attachment mismatch: %#v", policies)
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
