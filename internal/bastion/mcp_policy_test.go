package bastion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestMCPCommandsRequireAIReview(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		policy, llm, whitelist, blacklist bool
		response                          string
		want                              string
		calls                             int
	}{
		{name: "no policy", want: store.DecisionDeny},
		{name: "default allow without AI", policy: true, want: store.DecisionDeny},
		{name: "blacklist without AI", policy: true, blacklist: true, want: store.DecisionDeny},
		{name: "whitelist without AI", policy: true, whitelist: true, want: store.DecisionDeny},
		{name: "whitelist cannot bypass AI denial", policy: true, llm: true, whitelist: true, response: `{"allow":false,"reason":"unsafe"}`, want: store.DecisionDeny, calls: 1},
		{name: "AI approves whitelist", policy: true, llm: true, whitelist: true, response: `{"allow":true,"reason":"safe"}`, want: store.DecisionAllow, calls: 1},
		{name: "AI approves unmatched command", policy: true, llm: true, response: `{"allow":true}`, want: store.DecisionAllow, calls: 1},
		{name: "AI failure fails closed", policy: true, llm: true, whitelist: true, response: `invalid`, want: store.DecisionDeny, calls: 1},
		{name: "blacklist still denies", policy: true, llm: true, blacklist: true, response: `{"allow":true}`, want: store.DecisionDeny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			repo := st.Repository()
			user, err := repo.CreateUser(ctx, store.CreateUserParams{Email: "reviewer@example.com", DisplayName: "Reviewer", PasswordHash: []byte("hash")})
			if err != nil {
				t.Fatal(err)
			}
			org, err := repo.CreateOrganization(ctx, store.CreateOrganizationParams{Name: "Review", Slug: "review", OwnerUserID: user.ID})
			if err != nil {
				t.Fatal(err)
			}

			target, err := repo.CreateSSHTarget(ctx, store.CreateSSHTargetParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Alias: "review-target", TargetType: store.TargetDirect, Host: "192.0.2.10", Port: 22, RemoteUsername: "tester", AuthType: store.AuthPassword, CreatedBy: user.ID})
			if err != nil {
				t.Fatal(err)
			}
			command := "python3 -c 'print(123)'"
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body.Messages) < 2 || !strings.Contains(body.Messages[1].Content, command) {
					t.Error("AI did not receive full command")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": tc.response}}}})
			}))
			defer srv.Close()
			configID := ""
			if tc.llm {
				cfg, err := repo.CreateLLMPolicyConfig(ctx, store.CreateLLMPolicyConfigParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "review", BaseURL: srv.URL, Model: "test-model"})
				if err != nil {
					t.Fatal(err)
				}
				configID = cfg.ID
			}
			if tc.policy {
				policy, err := repo.CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{OwnerType: store.OwnerOrganization, OwnerID: org.ID, Name: "review", DefaultAction: store.DecisionAllow, LLMConfigID: configID, AllowManualReview: true})
				if err != nil {
					t.Fatal(err)
				}
				if err := repo.AttachPolicyToTarget(ctx, policy.ID, target.ID); err != nil {
					t.Fatal(err)
				}
				if tc.whitelist || tc.blacklist {
					ruleType := store.RuleWhitelist
					if tc.blacklist {
						ruleType = store.RuleBlacklist
					}
					if _, err := repo.CreatePolicyRule(ctx, store.CreatePolicyRuleParams{PolicyID: policy.ID, RuleType: ruleType, PatternType: store.PatternExact, Pattern: command}); err != nil {
						t.Fatal(err)
					}
				}
			}
			decision, err := NewService(repo).EvaluateMCPCommandForSource(ctx, user.ID, target.ID, command, "192.0.2.20")
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action != tc.want || calls != tc.calls {
				t.Fatalf("decision=%+v AI calls=%d, want %s calls=%d", decision, calls, tc.want, tc.calls)
			}
			if !tc.llm && decision.AllowManualReview {
				t.Fatal("manual approval must not bypass missing MCP AI review")
			}
			if tc.want == store.DecisionAllow && !strings.Contains(decision.Reason, "llm") {
				t.Fatalf("AI approval must be visible in audit reason: %+v", decision)
			}
			// Existing non-MCP whitelist behavior remains unchanged.
			if tc.whitelist {
				decision, err = NewService(repo).EvaluateCommand(ctx, user.ID, target.ID, command)
				if err != nil || decision.Action != store.DecisionAllow {
					t.Fatalf("ordinary whitelist changed: %+v %v", decision, err)
				}
			}
		})
	}
}
