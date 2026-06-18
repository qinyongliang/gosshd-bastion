package bastion

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestPolicyEvaluationWhitelistBlacklistDefaultAndUserGroups(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "alice@example.com", DisplayName: "Alice", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Ops", Slug: "ops", OwnerUserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := repo.ListOrganizationUserGroups(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := repo.CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "test2",
		TargetType: store.TargetDirect, Host: "127.0.0.1", Port: 22,
		RemoteUsername: "root", AuthType: store.AuthPassword, CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "strict", DefaultAction: store.DecisionDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{PolicyID: policy.ID, RuleType: store.RuleWhitelist, PatternType: store.PatternExact, Pattern: "whoami"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{PolicyID: policy.ID, RuleType: store.RuleBlacklist, PatternType: store.PatternContains, Pattern: "rm -rf"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToUserGroup(ctx, policy.ID, groups[0].ID); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repo)
	decision, err := svc.EvaluateCommand(ctx, user.ID, target.ID, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow {
		t.Fatalf("whoami decision mismatch: %+v", decision)
	}
	decision, err = svc.EvaluateCommand(ctx, user.ID, target.ID, "rm -rf /tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("blacklist decision mismatch: %+v", decision)
	}
	decision, err = svc.EvaluateCommand(ctx, user.ID, target.ID, "hostname")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("default decision mismatch: %+v", decision)
	}
}

func TestPolicyEvaluationUsesLLMWhenNoRuleMatches(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "alice@example.com", DisplayName: "Alice", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Ops LLM", Slug: "ops-llm", OwnerUserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	target, err := repo.CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "test2",
		TargetType: store.TargetDirect, Host: "127.0.0.1", Port: 22,
		RemoteUsername: "root", AuthType: store.AuthPassword, CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := llmTestServer(t, `{"allow":false,"reason":"needs approval"}`)
	defer srv.Close()
	llm, err := repo.CreateLLMPolicyConfig(ctx, store.CreateLLMPolicyConfigParams{
		OwnerType: store.OwnerOrganization,
		OwnerID:   org.ID,
		Name:      "reviewer",
		BaseURL:   srv.URL,
		Model:     "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := repo.CreateLLMPromptResource(ctx, store.CreateLLMPromptResourceParams{
		OwnerType: store.OwnerOrganization,
		OwnerID:   org.ID,
		Title:     "review commands",
		Content:   "review commands",
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType:     store.OwnerOrganization,
		OwnerID:       org.ID,
		Name:          "llm review",
		DefaultAction: store.DecisionAllow,
		LLMConfigID:   llm.ID,
		LLMPromptID:   prompt.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}

	decision, err := NewService(repo).EvaluateCommand(ctx, user.ID, target.ID, "hostname")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny || decision.Reason != "llm: needs approval" {
		t.Fatalf("llm decision mismatch: %+v", decision)
	}
}

func TestPolicyEvaluationAppliesByTargetTag(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "alice@example.com", DisplayName: "Alice", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Ops Tags", Slug: "ops-tags", OwnerUserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	target, err := repo.CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "test2",
		TargetType: store.TargetDirect, Host: "127.0.0.1", Port: 22,
		RemoteUsername: "root", AuthType: store.AuthPassword, Tags: []string{"测试环境"}, CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "readonly", DefaultAction: store.DecisionDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{PolicyID: policy.ID, RuleType: store.RuleWhitelist, PatternType: store.PatternExact, Pattern: "hostname"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTargetTag(ctx, policy.ID, store.OwnerOrganization, org.ID, "测试环境"); err != nil {
		t.Fatal(err)
	}

	decision, err := NewService(repo).EvaluateCommand(ctx, user.ID, target.ID, "hostname")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow {
		t.Fatalf("tag policy allow mismatch: %+v", decision)
	}
	decision, err = NewService(repo).EvaluateCommand(ctx, user.ID, target.ID, "cat /etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("tag policy deny mismatch: %+v", decision)
	}
	updated, err := repo.UpdateSSHTarget(ctx, target.ID, store.UpdateSSHTargetParams{Tags: []string{"prod"}, ReplaceTags: true})
	if err != nil {
		t.Fatal(err)
	}
	decision, err = NewService(repo).EvaluateCommand(ctx, user.ID, updated.ID, "cat /etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow || decision.Reason != "no policy" {
		t.Fatalf("tag removal should detach policy dynamically: %+v", decision)
	}
}
