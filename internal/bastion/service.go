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
		requiresLLM := shellCommandHasChannel(trimmed)
		policyDecision := Decision{Action: policy.DefaultAction, Reason: "default " + policy.DefaultAction}
		ruleDecision, matched, missingWhitelist := evaluatePolicyRules(policy.Rules, trimmed, requiresLLM)
		if matched {
			policyDecision = ruleDecision
		}
		if !matched && requiresLLM && policy.LLMConfigID == "" {
			matched = true
			policyDecision = Decision{Action: store.DecisionDeny, Reason: "channel requires LLM review: " + policy.Name}
		}
		if !matched && len(missingWhitelist) > 0 && policy.DefaultAction == store.DecisionDeny && policy.LLMConfigID == "" {
			matched = true
			policyDecision = Decision{Action: store.DecisionDeny, Reason: "whitelist missing: " + missingWhitelist[0]}
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

func evaluatePolicyRules(rules []store.PolicyRule, command string, requiresLLM bool) (Decision, bool, []string) {
	if requiresLLM {
		return Decision{}, false, nil
	}
	segments := shellCommandSegments(command)
	for _, rule := range rules {
		if rule.RuleType != store.RuleBlacklist {
			continue
		}
		if ruleMatches(rule, command) || ruleMatchesAnySegment(rule, segments) {
			return Decision{Action: store.DecisionDeny, Reason: "blacklist: " + rule.Pattern}, true, nil
		}
	}
	hasWhitelist := false
	var missing []string
	var lastPattern string
	for _, segment := range segments {
		allowed := false
		for _, rule := range rules {
			if rule.RuleType != store.RuleWhitelist {
				continue
			}
			hasWhitelist = true
			if ruleMatches(rule, segment) {
				allowed = true
				lastPattern = rule.Pattern
				break
			}
		}
		if !allowed {
			missing = append(missing, segment)
		}
	}
	if !hasWhitelist {
		return Decision{}, false, nil
	}
	if len(missing) > 0 {
		return Decision{}, false, missing
	}
	return Decision{Action: store.DecisionAllow, Reason: "whitelist: " + lastPattern}, true, nil
}

func ruleMatchesAnySegment(rule store.PolicyRule, segments []string) bool {
	for _, segment := range segments {
		if ruleMatches(rule, segment) {
			return true
		}
	}
	return false
}

func shellCommandSegments(command string) []string {
	segments, _ := shellCommandParts(command)
	return segments
}

func shellCommandHasChannel(command string) bool {
	_, hasChannel := shellCommandParts(command)
	return hasChannel
}

func shellCommandParts(command string) ([]string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, false
	}
	var segments []string
	var current strings.Builder
	var quote byte
	escaped := false
	hasChannel := false
	flush := func() {
		segment := strings.TrimSpace(current.String())
		if segment != "" {
			segments = append(segments, segment)
		}
		current.Reset()
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		if quote != '\'' && c == '\\' {
			current.WriteByte(c)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			current.WriteByte(c)
			continue
		}
		if c == ';' || c == '\n' || c == '\r' {
			flush()
			continue
		}
		if c == '|' {
			hasChannel = true
			flush()
			if i+1 < len(command) && command[i+1] == '|' {
				i++
			}
			continue
		}
		if c == '&' && !isShellRedirectionAmpersand(command, i) {
			flush()
			if i+1 < len(command) && command[i+1] == '&' {
				i++
			}
			continue
		}
		current.WriteByte(c)
	}
	flush()
	return segments, hasChannel
}

func isShellRedirectionAmpersand(command string, index int) bool {
	if index+1 < len(command) && command[index+1] == '>' {
		return true
	}
	for i := index - 1; i >= 0; i-- {
		if command[i] == ' ' || command[i] == '\t' {
			continue
		}
		return command[i] == '>'
	}
	return false
}

func ruleMatches(rule store.PolicyRule, command string) bool {
	pattern := strings.TrimSpace(rule.Pattern)
	switch rule.PatternType {
	case store.PatternExact:
		return command == pattern
	case store.PatternPrefix:
		return strings.HasPrefix(command, pattern)
	case store.PatternContains:
		if pattern == ">" || pattern == ">>" {
			return hasUnsafeOutputRedirection(command, pattern == ">>")
		}
		if isBareShellPattern(pattern) {
			return commandNameMatches(command, pattern)
		}
		return strings.Contains(command, pattern)
	default:
		return false
	}
}

func hasUnsafeOutputRedirection(command string, appendOnly bool) bool {
	var quote byte
	escaped := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		if escaped {
			escaped = false
			continue
		}
		if quote != '\'' && c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c != '>' {
			continue
		}
		isAppend := i+1 < len(command) && command[i+1] == '>'
		if appendOnly && !isAppend {
			continue
		}
		operatorEnd := i + 1
		if isAppend {
			operatorEnd++
		}
		target := shellRedirectionTarget(command[operatorEnd:])
		if isSafeRedirectionTarget(target) {
			i = operatorEnd + len(target) - 1
			continue
		}
		return true
	}
	return false
}

func shellRedirectionTarget(raw string) string {
	raw = strings.TrimLeft(raw, " \t")
	if raw == "" {
		return ""
	}
	var out strings.Builder
	var quote byte
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if escaped {
			out.WriteByte(c)
			escaped = false
			continue
		}
		if quote != '\'' && c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			out.WriteByte(c)
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ' ' || c == '\t' || c == ';' || c == '|' || c == '&' {
			break
		}
		out.WriteByte(c)
	}
	return strings.TrimSpace(out.String())
}

func isSafeRedirectionTarget(target string) bool {
	target = strings.TrimSpace(target)
	switch target {
	case "/dev/null", "&1", "&2":
		return true
	default:
		return false
	}
}

func isBareShellPattern(pattern string) bool {
	if pattern == "" {
		return false
	}
	for _, r := range pattern {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '-', '.', '/':
			continue
		default:
			return false
		}
	}
	return true
}

func commandNameMatches(command, pattern string) bool {
	for _, segment := range shellCommandSegments(command) {
		if commandNameInSegmentMatches(segment, pattern) {
			return true
		}
	}
	return false
}

func commandNameInSegmentMatches(segment, pattern string) bool {
	words := shellWords(segment)
	for i := 0; i < len(words); i++ {
		word := strings.TrimSpace(words[i])
		if word == "" {
			continue
		}
		base := shellTokenBase(word)
		if isShellEnvAssignment(word) {
			continue
		}
		if isShellCommandWrapper(base) {
			continue
		}
		if strings.HasPrefix(word, "-") {
			continue
		}
		return word == pattern || base == pattern
	}
	return false
}

func isShellCommandWrapper(base string) bool {
	switch base {
	case "sudo", "doas", "env", "command", "builtin", "exec", "nohup", "time":
		return true
	default:
		return false
	}
}

func isShellEnvAssignment(word string) bool {
	index := strings.IndexByte(word, '=')
	if index <= 0 {
		return false
	}
	name := word[:index]
	for i, r := range name {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func shellTokenBase(token string) string {
	token = strings.TrimRight(token, "/")
	if index := strings.LastIndexByte(token, '/'); index >= 0 {
		return token[index+1:]
	}
	return token
}

func shellWords(segment string) []string {
	var words []string
	var current strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		word := strings.TrimSpace(current.String())
		if word != "" {
			words = append(words, word)
		}
		current.Reset()
	}
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		if quote != '\'' && c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			current.WriteByte(c)
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ' ' || c == '\t' || c == '<' || c == '>' {
			flush()
			continue
		}
		current.WriteByte(c)
	}
	flush()
	return words
}
