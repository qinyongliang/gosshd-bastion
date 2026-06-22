package bastion

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
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
		AllowManualReview: true,
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
	if decision.Action != store.DecisionAllow || decision.AllowManualReview {
		t.Fatalf("whoami decision mismatch: %+v", decision)
	}
	decision, err = svc.EvaluateCommand(ctx, user.ID, target.ID, "rm -rf /tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny || !decision.AllowManualReview {
		t.Fatalf("blacklist decision mismatch: %+v", decision)
	}
	decision, err = svc.EvaluateCommand(ctx, user.ID, target.ID, "hostname")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny || !decision.AllowManualReview {
		t.Fatalf("default decision mismatch: %+v", decision)
	}
}

func TestPolicyWhitelistRequiresEveryShellSegmentToMatch(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "reader@example.com", DisplayName: "Reader", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Read Only", Slug: "read-only", OwnerUserID: user.ID})
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
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "readonly", DefaultAction: store.DecisionDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"ls", "pwd", "wc"} {
		if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{
			PolicyID: policy.ID, RuleType: store.RuleWhitelist, PatternType: store.PatternContains, Pattern: pattern,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repo)
	decision, err := svc.EvaluateCommand(ctx, user.ID, target.ID, "ls")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow {
		t.Fatalf("single whitelisted command should be allowed: %+v", decision)
	}
	decision, err = svc.EvaluateCommand(ctx, user.ID, target.ID, "ls && pwd")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow {
		t.Fatalf("all whitelisted shell segments should be allowed: %+v", decision)
	}
	decision, err = svc.EvaluateCommand(ctx, user.ID, target.ID, "ls && mkdir test")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("compound command with unapproved segment should be denied: %+v", decision)
	}
}

func TestPolicyPipeCommandsUseLLMReview(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "pipe@example.com", DisplayName: "Pipe", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Pipe Review", Slug: "pipe-review", OwnerUserID: user.ID})
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
	var llmRequest map[string]any
	srv := llmTestServerWithRequest(t, `{"allow":true}`, &llmRequest)
	defer srv.Close()
	llm, err := repo.CreateLLMPolicyConfig(ctx, store.CreateLLMPolicyConfigParams{
		OwnerType: store.OwnerOrganization,
		OwnerID:   org.ID,
		Name:      "pipe reviewer",
		BaseURL:   srv.URL,
		Model:     "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "readonly with llm", DefaultAction: store.DecisionDeny,
		LLMConfigID: llm.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"ls", "wc"} {
		if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{
			PolicyID: policy.ID, RuleType: store.RuleWhitelist, PatternType: store.PatternContains, Pattern: pattern,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, pattern := range []string{"rm", "dd", ">"} {
		if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{
			PolicyID: policy.ID, RuleType: store.RuleBlacklist, PatternType: store.PatternContains, Pattern: pattern,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}

	decision, err := NewService(repo).EvaluateCommand(ctx, user.ID, target.ID, "ip addr 2>/dev/null | grep 'inet '; docker info --format '{{.ServerVersion}}' | wc -l")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow || !strings.HasPrefix(decision.Reason, "llm") {
		t.Fatalf("pipe command should be decided by llm: %+v", decision)
	}
	messages, ok := llmRequest["messages"].([]any)
	if !ok || len(messages) < 2 || !strings.Contains(messages[1].(map[string]any)["content"].(string), "docker info --format") {
		t.Fatalf("llm request should include pipe command: %+v", llmRequest)
	}
}

func TestPolicyPipeCommandWithoutLLMIsDenied(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "no-llm-pipe@example.com", DisplayName: "Pipe", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Pipe No LLM", Slug: "pipe-no-llm", OwnerUserID: user.ID})
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
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "readonly without llm", DefaultAction: store.DecisionAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"ls", "wc"} {
		if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{
			PolicyID: policy.ID, RuleType: store.RuleWhitelist, PatternType: store.PatternContains, Pattern: pattern,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}

	decision, err := NewService(repo).EvaluateCommand(ctx, user.ID, target.ID, "ls | wc -l")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("pipe command without llm should be denied: %+v", decision)
	}
}

func TestContainsBareCommandRulesMatchCommandTokens(t *testing.T) {
	rmRule := store.PolicyRule{RuleType: store.RuleBlacklist, PatternType: store.PatternContains, Pattern: "rm"}
	ddRule := store.PolicyRule{RuleType: store.RuleBlacklist, PatternType: store.PatternContains, Pattern: "dd"}
	lsRule := store.PolicyRule{RuleType: store.RuleWhitelist, PatternType: store.PatternContains, Pattern: "ls"}

	if ruleMatches(rmRule, "docker info --format '{{.ServerVersion}}'") {
		t.Fatalf("rm should not match --format")
	}
	if ruleMatches(ddRule, "ip addr 2>/dev/null") {
		t.Fatalf("dd should not match addr")
	}
	if ruleMatches(lsRule, "false") {
		t.Fatalf("ls should not match false")
	}
	if !ruleMatches(rmRule, "rm -rf /tmp/x") {
		t.Fatalf("rm command should match")
	}
	if !ruleMatches(rmRule, "sudo /bin/rm -rf /tmp/x") {
		t.Fatalf("sudo /bin/rm command should match")
	}
	if !ruleMatches(ddRule, "dd if=/dev/zero of=/tmp/x bs=1M count=1") {
		t.Fatalf("dd command should match")
	}
	if !ruleMatches(lsRule, "ls -la /tmp") {
		t.Fatalf("ls command should match")
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
	if decision.Action != store.DecisionDeny || !regexp.MustCompile(`^llm: needs approval \([0-9]+s\)$`).MatchString(decision.Reason) {
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
		t.Fatalf("tag removal should use default allow without a matching policy: %+v", decision)
	}
}

func TestPolicyAccessCapabilitiesAndSourceIPAllowlist(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "gosshd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := st.Repository()
	user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "ops@example.com", DisplayName: "Ops", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatal(err)
	}
	org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Ops Access", Slug: "ops-access", OwnerUserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	target, err := repo.CreateSSHTarget(ctx, store.CreateSSHTargetParams{
		OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "test2",
		TargetType: store.TargetDirect, Host: "10.0.0.8", Port: 22,
		RemoteUsername: "root", AuthType: store.AuthPassword, CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
		OwnerType:        store.OwnerOrganization,
		OwnerID:          org.ID,
		Name:             "private-readonly",
		DefaultAction:    store.DecisionAllow,
		IPAllowlist:      "10.0.0.0/8, 192.168.1.10-192.168.1.20",
		AllowInteractive: true,
		AllowUpload:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repo)
	decision, err := svc.EvaluateAccess(ctx, user.ID, target.ID, store.RequestShell, "10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow {
		t.Fatalf("interactive shell should be allowed from private source: %+v", decision)
	}
	decision, err = svc.EvaluateAccess(ctx, user.ID, target.ID, store.RequestShell, "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("shell should be denied outside allowlist: %+v", decision)
	}
	decision, err = svc.EvaluateAccess(ctx, user.ID, target.ID, store.RequestForward, "10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("port forwarding should be denied when capability is off: %+v", decision)
	}
	decision, allowUpload, allowDownload, err := svc.EvaluateSFTPAccess(ctx, user.ID, target.ID, "192.168.1.15")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow || !allowUpload || allowDownload {
		t.Fatalf("sftp capability mismatch: decision=%+v upload=%v download=%v", decision, allowUpload, allowDownload)
	}
	decision, allowUpload, allowDownload, err = svc.EvaluateSFTPAccess(ctx, user.ID, target.ID, "192.168.2.15")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny || allowUpload || allowDownload {
		t.Fatalf("sftp should be denied outside allowlist: decision=%+v upload=%v download=%v", decision, allowUpload, allowDownload)
	}
}
