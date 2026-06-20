package bastion

import (
	"bytes"
	"context"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"time"

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
	return s.EvaluateCommandForSource(ctx, userID, targetID, command, "")
}

func (s *Service) EvaluateCommandForSource(ctx context.Context, userID, targetID, command, sourceIP string) (Decision, error) {
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
		if decision, ok := evaluateSourceIP(policy, sourceIP); ok {
			return decision, nil
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
			llmStarted := time.Now()
			policyDecision, err = s.llmClient.ReviewCommand(ctx, cfg, LLMReviewInput{
				UserID:   userID,
				TargetID: targetID,
				Command:  trimmed,
				Prompt:   prompt.Content,
			})
			policyDecision.Reason = appendLLMDuration(policyDecision.Reason, time.Since(llmStarted))
			if err != nil {
				return policyDecision, nil
			}
			if !strings.HasPrefix(strings.ToLower(policyDecision.Reason), "llm") {
				policyDecision.Reason = "llm: " + policyDecision.Reason
			}
		}
		if policyDecision.Action == store.DecisionDeny {
			return policyDecision, nil
		}
		result = policyDecision
	}
	return result, nil
}

func (s *Service) EvaluateAccess(ctx context.Context, userID, targetID, requestType, sourceIP string) (Decision, error) {
	policies, err := s.repo.ListPoliciesForTarget(ctx, targetID)
	if err != nil {
		return Decision{}, err
	}
	result := Decision{Action: store.DecisionAllow, Reason: "no policy"}
	for _, policy := range policies {
		applies, err := s.policyAppliesToUser(ctx, policy, userID)
		if err != nil {
			return Decision{}, err
		}
		if !applies {
			continue
		}
		if decision, ok := evaluateSourceIP(policy, sourceIP); ok {
			return decision, nil
		}
		switch requestType {
		case store.RequestShell:
			if !policy.AllowInteractive {
				return Decision{Action: store.DecisionDeny, Reason: "interactive terminal disabled by policy: " + policy.Name}, nil
			}
		case store.RequestSFTP:
			if !policy.AllowUpload && !policy.AllowDownload {
				return Decision{Action: store.DecisionDeny, Reason: "file transfer disabled by policy: " + policy.Name}, nil
			}
		case store.RequestForward:
			if !policy.AllowPortForward {
				return Decision{Action: store.DecisionDeny, Reason: "port forwarding disabled by policy: " + policy.Name}, nil
			}
		}
		result = Decision{Action: store.DecisionAllow, Reason: "policy capability allowed: " + policy.Name}
	}
	return result, nil
}

func (s *Service) EvaluateSFTPAccess(ctx context.Context, userID, targetID, sourceIP string) (Decision, bool, bool, error) {
	policies, err := s.repo.ListPoliciesForTarget(ctx, targetID)
	if err != nil {
		return Decision{}, false, false, err
	}
	applied := false
	allowUpload := false
	allowDownload := false
	result := Decision{Action: store.DecisionAllow, Reason: "no policy"}
	for _, policy := range policies {
		applies, err := s.policyAppliesToUser(ctx, policy, userID)
		if err != nil {
			return Decision{}, false, false, err
		}
		if !applies {
			continue
		}
		applied = true
		if decision, ok := evaluateSourceIP(policy, sourceIP); ok {
			return decision, false, false, nil
		}
		allowUpload = allowUpload || policy.AllowUpload
		allowDownload = allowDownload || policy.AllowDownload
		result = Decision{Action: store.DecisionAllow, Reason: "file transfer policy: " + policy.Name}
	}
	if !applied {
		return result, true, true, nil
	}
	if !allowUpload && !allowDownload {
		return Decision{Action: store.DecisionDeny, Reason: "file transfer disabled by policy"}, false, false, nil
	}
	return result, allowUpload, allowDownload, nil
}

func evaluateSourceIP(policy store.CommandPolicy, sourceIP string) (Decision, bool) {
	if strings.TrimSpace(policy.IPAllowlist) == "" {
		return Decision{}, false
	}
	if sourceIPAllowed(policy.IPAllowlist, sourceIP) {
		return Decision{}, false
	}
	return Decision{Action: store.DecisionDeny, Reason: "source IP not allowed by policy: " + policy.Name}, true
}

func sourceIPAllowed(allowlist, sourceIP string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(sourceIP))
	if err != nil {
		return false
	}
	for _, item := range splitAllowlist(allowlist) {
		lower := strings.ToLower(item)
		if lower == "private" || lower == "intranet" || lower == "内网" {
			if addr.IsPrivate() || addr.IsLoopback() {
				return true
			}
			continue
		}
		if strings.Contains(item, "/") {
			if prefix, err := netip.ParsePrefix(item); err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		if before, after, ok := strings.Cut(item, "-"); ok {
			start, startErr := netip.ParseAddr(strings.TrimSpace(before))
			end, endErr := netip.ParseAddr(strings.TrimSpace(after))
			if startErr == nil && endErr == nil && addr.Compare(start) >= 0 && addr.Compare(end) <= 0 {
				return true
			}
			continue
		}
		if exact, err := netip.ParseAddr(item); err == nil && exact == addr {
			return true
		}
	}
	return false
}

func splitAllowlist(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ';' || r == ' '
	})
	var out []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func appendLLMDuration(reason string, elapsed time.Duration) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "llm"
	}
	seconds := int(math.Ceil(elapsed.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return reason + " (" + strconv.Itoa(seconds) + "s)"
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
