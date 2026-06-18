package bastion

import (
	"bytes"
	"context"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func NormalizeAuthorizedKey(raw string) (string, string, error) {
	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(strings.TrimSpace(raw)))
	if err != nil {
		return "", "", err
	}
	normalized := string(gossh.MarshalAuthorizedKey(key))
	normalized = string(bytes.TrimRight([]byte(normalized), "\n"))
	return normalized + "\n", gossh.FingerprintSHA256(key), nil
}

func (s *Service) NormalizeAuthorizedKey(raw string) (string, string, error) {
	return NormalizeAuthorizedKey(raw)
}

func (s *Service) LookupUserByPublicKey(ctx context.Context, key gossh.PublicKey) (store.User, error) {
	return s.repo.GetUserByPublicKeyFingerprint(ctx, gossh.FingerprintSHA256(key))
}

func (s *Service) EvaluateCommand(ctx context.Context, userID, targetID, command string) (Decision, error) {
	policies, err := s.repo.ListPoliciesForTarget(ctx, targetID)
	if err != nil {
		return Decision{}, err
	}
	trimmed := strings.TrimSpace(command)
	result := Decision{Action: store.DecisionAllow, Reason: "no policy"}
	for _, policy := range policies {
		applies, err := s.policyAppliesToUser(ctx, policy, userID)
		if err != nil {
			return Decision{}, err
		}
		if !applies {
			continue
		}
		matched := false
		policyDecision := Decision{Action: policy.DefaultAction, Reason: "default " + policy.DefaultAction}
		for _, rule := range policy.Rules {
			if !ruleMatches(rule, trimmed) {
				continue
			}
			matched = true
			if rule.RuleType == store.RuleWhitelist {
				policyDecision = Decision{Action: store.DecisionAllow, Reason: "whitelist: " + rule.Pattern}
				break
			}
			if rule.RuleType == store.RuleBlacklist {
				return Decision{Action: store.DecisionDeny, Reason: "blacklist: " + rule.Pattern}, nil
			}
		}
		if !matched && policy.LLMConfigID != "" {
			cfg, err := s.repo.GetLLMPolicyConfig(ctx, policy.LLMConfigID)
			if err != nil {
				return Decision{}, err
			}
			prompt := store.LLMPromptResource{Content: store.DefaultLLMPromptContent}
			if policy.LLMPromptID != "" {
				prompt, err = s.repo.GetLLMPromptResource(ctx, policy.LLMPromptID)
				if err != nil {
					return Decision{}, err
				}
			} else if prompts, err := s.repo.ListLLMPromptResources(ctx, policy.OwnerType, policy.OwnerID); err == nil && len(prompts) > 0 {
				prompt = prompts[0]
			}
			policyDecision, err = s.llmClient.ReviewCommand(ctx, cfg, LLMReviewInput{
				UserID:   userID,
				TargetID: targetID,
				Command:  trimmed,
				Prompt:   prompt.Content,
			})
			if err != nil {
				return policyDecision, nil
			}
			policyDecision.Reason = "llm: " + policyDecision.Reason
		}
		if policyDecision.Action == store.DecisionDeny {
			return policyDecision, nil
		}
		result = policyDecision
	}
	return result, nil
}

func (s *Service) policyAppliesToUser(ctx context.Context, policy store.CommandPolicy, userID string) (bool, error) {
	if len(policy.UserGroupIDs) == 0 {
		return true, nil
	}
	for _, groupID := range policy.UserGroupIDs {
		ok, err := s.repo.UserInGroup(ctx, groupID, userID)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func ruleMatches(rule store.PolicyRule, command string) bool {
	pattern := strings.TrimSpace(rule.Pattern)
	switch rule.PatternType {
	case store.PatternExact:
		return command == pattern
	case store.PatternPrefix:
		return strings.HasPrefix(command, pattern)
	case store.PatternContains:
		return strings.Contains(command, pattern)
	default:
		return false
	}
}
