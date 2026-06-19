package bastion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLLMPolicyAllowsWithoutReasonAndDisablesThinking(t *testing.T) {
	var request map[string]any
	srv := llmTestServerWithRequest(t, `{"allow":true}`, &request)
	defer srv.Close()

	client := NewLLMClient()
	decision, err := client.ReviewCommand(context.Background(), store.LLMPolicyConfig{
		BaseURL: srv.URL,
		Model:   "test-model",
	}, LLMReviewInput{Command: "whoami", Prompt: "custom review"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != store.DecisionAllow || decision.Reason != "allow" {
		t.Fatalf("decision mismatch: %+v", decision)
	}
	if request["enable_thinking"] != false {
		t.Fatalf("llm request should disable thinking: %+v", request)
	}
	if format, ok := request["response_format"].(map[string]any); !ok || format["type"] != "json_object" {
		t.Fatalf("llm request should ask for json_object response: %+v", request)
	}
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("llm request missing messages: %+v", request)
	}
	system, ok := messages[0].(map[string]any)
	if !ok || !strings.Contains(system["content"].(string), "Do not output chain-of-thought") {
		t.Fatalf("system prompt should suppress reasoning: %+v", messages)
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
	return llmTestServerWithRequest(t, content, nil)
}

func llmTestServerWithRequest(t *testing.T, content string, requestOut *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if requestOut != nil {
			*requestOut = req
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		})
	}))
}
