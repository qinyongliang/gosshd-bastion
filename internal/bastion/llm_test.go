package bastion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

func TestLLMPolicyAllowsJSONAllowResponse(t *testing.T) {
	srv := llmTestServer(t, `{"allow":true,"reason":"safe command"}`)
	defer srv.Close()

	client := NewLLMClient()
	decision, err := client.ReviewCommand(context.Background(), store.LLMPolicyConfig{
		BaseURL: srv.URL,
		Model:   "test-model",
	}, LLMReviewInput{Command: "whoami", Prompt: "review command"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow || decision.Reason != "safe command" {
		t.Fatalf("decision mismatch: %+v", decision)
	}
}

func TestLLMPolicyDeniesJSONDenyResponse(t *testing.T) {
	srv := llmTestServer(t, `{"allow":false,"reason":"dangerous"}`)
	defer srv.Close()

	client := NewLLMClient()
	decision, err := client.ReviewCommand(context.Background(), store.LLMPolicyConfig{
		BaseURL: srv.URL,
		Model:   "test-model",
	}, LLMReviewInput{Command: "rm -rf /", Prompt: "review command"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionDeny || decision.Reason != "dangerous" {
		t.Fatalf("decision mismatch: %+v", decision)
	}
}

func TestLLMPolicyFailsClosedOnInvalidJSON(t *testing.T) {
	srv := llmTestServer(t, `not-json`)
	defer srv.Close()

	client := NewLLMClient()
	decision, err := client.ReviewCommand(context.Background(), store.LLMPolicyConfig{
		BaseURL: srv.URL,
		Model:   "test-model",
	}, LLMReviewInput{Command: "hostname", Prompt: "review command"})
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if decision.Action != store.DecisionDeny {
		t.Fatalf("invalid json should deny: %+v", decision)
	}
}

func llmTestServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		})
	}))
}
