package bastion

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/qinyongliang/gosshd/internal/store"
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
