package bastion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd/internal/store"
)

type LLMClient struct {
	httpClient *http.Client
}

type LLMReviewInput struct {
	UserID   string `json:"user_id,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	Command  string `json:"command"`
	Prompt   string `json:"-"`
}

func NewLLMClient() *LLMClient {
	return &LLMClient{httpClient: http.DefaultClient}
}

func (c *LLMClient) ReviewCommand(ctx context.Context, cfg store.LLMPolicyConfig, input LLMReviewInput) (Decision, error) {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	body := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": reviewSystemPrompt(input.Prompt)},
			{"role": "user", "content": reviewPrompt(input)},
		},
		"temperature": 0,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return denyWithError("llm request marshal failed", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return denyWithError("llm request creation failed", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if len(cfg.EncryptedAPIKey) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(cfg.EncryptedAPIKey))
	}

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return denyWithError("llm request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return denyWithError("llm request rejected", fmt.Errorf("unexpected status %s", resp.Status))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return denyWithError("llm response decode failed", err)
	}
	if len(out.Choices) == 0 {
		return denyWithError("llm response missing choices", errors.New("empty choices"))
	}
	var review struct {
		Allow  bool   `json:"allow"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.Choices[0].Message.Content)), &review); err != nil {
		return denyWithError("llm response content decode failed", err)
	}
	reason := strings.TrimSpace(review.Reason)
	if reason == "" {
		reason = "llm review"
	}
	if review.Allow {
		return Decision{Action: store.DecisionAllow, Reason: reason}, nil
	}
	return Decision{Action: store.DecisionDeny, Reason: reason}, nil
}

func reviewSystemPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return store.DefaultLLMPromptContent
	}
	return prompt
}

func reviewPrompt(input LLMReviewInput) string {
	payload, err := json.Marshal(input)
	if err != nil {
		return input.Command
	}
	return "Review this SSH command request and respond with JSON only: " + string(payload)
}

func denyWithError(reason string, err error) (Decision, error) {
	return Decision{Action: store.DecisionDeny, Reason: reason}, err
}
