package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/store"
)

type mcpOK struct {
	OK bool `json:"ok"`
}

func (a *App) mcpHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.userForMCPRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return a.newMCPServer(user)
		}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		handler.ServeHTTP(w, r)
	})
}

func (a *App) userForMCPRequest(r *http.Request) (store.User, error) {
	if user, err := a.userForRequest(r); err == nil {
		return user, nil
	}
	tokenValue := bearerToken(r)
	if tokenValue == "" {
		return store.User{}, store.ErrNotFound
	}
	if err := a.ensureServices(r.Context()); err != nil {
		return store.User{}, err
	}
	token, err := a.store.Repository().GetMCPTokenByHash(r.Context(), codeHash(tokenValue))
	if err != nil {
		return store.User{}, err
	}
	user, err := a.store.Repository().GetUser(r.Context(), token.UserID)
	if err != nil {
		return store.User{}, err
	}
	_ = a.store.Repository().TouchMCPToken(r.Context(), token.ID, time.Now().UTC())
	return user, nil
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	prefix := "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func (a *App) newMCPServer(actor store.User) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "gosshd-bastion", Version: a.cfg.version()}, nil)

	mcp.AddTool(s, &mcp.Tool{Name: "auth_register", Description: "Register a user and create their personal organization."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpRegisterInput) (*mcp.CallToolResult, apiUserResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiUserResponse{}, err
			}
			if !actor.IsSystemAdmin {
				return nil, apiUserResponse{}, errors.New("system admin required")
			}
			user, _, err := a.auth.Register(ctx, in.Email, in.DisplayName, in.Password)
			if err != nil {
				return nil, apiUserResponse{}, err
			}
			return nil, apiUserResponse{User: apiUserFromStore(user)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "org_list", Description: "List organizations for a user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUserInput) (*mcp.CallToolResult, apiOrganizationsPayload, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiOrganizationsPayload{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiOrganizationsPayload{}, err
			}
			orgs, err := a.store.Repository().ListOrganizationsForUser(ctx, userID)
			if err != nil {
				return nil, apiOrganizationsPayload{}, err
			}
			return nil, apiOrganizationsFromStore(orgs), nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "org_create", Description: "Create a non-personal organization for a user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpOrgCreateInput) (*mcp.CallToolResult, apiOrganizationResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiOrganizationResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiOrganizationResponse{}, err
			}
			org, err := a.store.Repository().CreateOrganization(ctx, store.CreateOrganizationParams{
				Name:        in.Name,
				Slug:        in.Slug,
				OwnerUserID: userID,
			})
			if err != nil {
				return nil, apiOrganizationResponse{}, err
			}
			return nil, apiOrganizationResponse{Organization: apiOrganizationFromStore(org)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "org_invite_create", Description: "Create an invite code for a non-personal organization."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpOrgInviteInput) (*mcp.CallToolResult, apiInviteResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiInviteResponse{}, err
			}
			if _, err := mcpUserID(actor, in.UserID); err != nil {
				return nil, apiInviteResponse{}, err
			}
			if err := a.requireOrganizationAdmin(ctx, in.OrganizationID, actor); err != nil {
				return nil, apiInviteResponse{}, err
			}
			org, err := a.store.Repository().GetOrganization(ctx, in.OrganizationID)
			if err != nil {
				return nil, apiInviteResponse{}, err
			}
			if org.IsPersonal {
				return nil, apiInviteResponse{}, errPersonalInvite
			}
			code, hash, err := randomCode()
			if err != nil {
				return nil, apiInviteResponse{}, err
			}
			role := in.Role
			if role == "" {
				role = store.RoleMember
			}
			if role != store.RoleMember && role != store.RoleAdmin {
				return nil, apiInviteResponse{}, errors.New("invite role must be member or admin")
			}
			if role == store.RoleAdmin {
				if err := a.requireOrganizationOwner(ctx, in.OrganizationID, actor); err != nil {
					return nil, apiInviteResponse{}, errors.New("organization owner required for admin invites")
				}
			}
			if _, err := a.store.Repository().CreateOrganizationInvite(ctx, store.CreateOrganizationInviteParams{
				OrganizationID: in.OrganizationID,
				CodeHash:       hash,
				Role:           role,
				ExpiresAt:      time.Now().UTC().Add(7 * 24 * time.Hour),
				CreatedBy:      actor.ID,
			}); err != nil {
				return nil, apiInviteResponse{}, err
			}
			return nil, apiInviteResponse{Code: code}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "org_join", Description: "Join an organization with an invite code."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpJoinInput) (*mcp.CallToolResult, apiOrganizationResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiOrganizationResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiOrganizationResponse{}, err
			}
			org, err := a.joinOrganizationWithCode(ctx, userID, in.Code)
			if err != nil {
				return nil, apiOrganizationResponse{}, err
			}
			return nil, apiOrganizationResponse{Organization: apiOrganizationFromStore(org)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "org_leave", Description: "Leave a non-personal organization."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpOrgMemberInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			if err := a.store.Repository().LeaveOrganization(ctx, in.OrganizationID, userID); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "public_key_add", Description: "Add an SSH public key for a user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPublicKeyInput) (*mcp.CallToolResult, apiPublicKeyResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiPublicKeyResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiPublicKeyResponse{}, err
			}
			normalized, fingerprint, err := a.bastion.NormalizeAuthorizedKey(in.AuthorizedKey)
			if err != nil {
				return nil, apiPublicKeyResponse{}, err
			}
			key, err := a.store.Repository().CreatePublicKey(ctx, store.CreatePublicKeyParams{
				UserID:        userID,
				Name:          in.Name,
				AuthorizedKey: normalized,
				Fingerprint:   fingerprint,
			})
			if err != nil {
				return nil, apiPublicKeyResponse{}, err
			}
			return nil, apiPublicKeyResponse{Key: apiPublicKeyFromStore(key)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "target_create", Description: "Create a direct or agent-backed SSH target."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTargetCreateInput) (*mcp.CallToolResult, apiTargetResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiTargetResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiTargetResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, userID)
			if err != nil {
				return nil, apiTargetResponse{}, err
			}
			target, err := a.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
				OwnerType:       ownerType,
				OwnerID:         ownerID,
				Name:            in.Name,
				Alias:           in.Alias,
				TargetType:      in.TargetType,
				Host:            in.Host,
				Port:            in.Port,
				RemoteUsername:  in.RemoteUsername,
				AuthType:        in.AuthType,
				EncryptedSecret: []byte(in.Secret),
				AgentID:         in.AgentID,
				ProxyTargetID:   in.ProxyTargetID,
				Tags:            in.Tags,
				CreatedBy:       userID,
			})
			if err != nil {
				return nil, apiTargetResponse{}, err
			}
			return nil, apiTargetResponse{Target: apiTargetFromStore(target)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "target_delete", Description: "Delete an SSH target and remove its policy/tag bindings."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpTargetDeleteInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			target, err := a.store.Repository().GetSSHTarget(ctx, in.TargetID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			if _, _, err := a.resolveOwner(ctx, target.OwnerType, target.OwnerID, userID); err != nil {
				return nil, mcpOK{}, err
			}
			if err := a.store.Repository().DeleteSSHTarget(ctx, target.ID); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "agent_enrollment_create", Description: "Create an agent enrollment and install commands."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAgentEnrollmentInput) (*mcp.CallToolResult, apiAgentEnrollmentResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, userID)
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			token, hash, err := randomCode()
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			defaultHost, defaultPort := agentEnrollmentDefaults(in.DefaultHost, in.DefaultPort)
			enrollment, err := a.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
				OwnerType:   ownerType,
				OwnerID:     ownerID,
				TokenHash:   hash,
				Label:       in.Label,
				DefaultHost: defaultHost,
				DefaultPort: defaultPort,
				CreatedBy:   userID,
				ExpiresAt:   time.Now().UTC().Add(30 * 24 * time.Hour),
			})
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			base := a.cfg.publicHost()
			if base == "" {
				return nil, apiAgentEnrollmentResponse{}, errors.New("public host is required for agent enrollment install commands")
			}
			if !strings.Contains(base, "://") {
				base = "http://" + base
			}
			return nil, agentEnrollmentResponse(enrollment.ID, token, base), nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "llm_config_create", Description: "Create an LLM provider config for an owner."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpLLMConfigInput) (*mcp.CallToolResult, apiLLMConfigResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiLLMConfigResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiLLMConfigResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, userID)
			if err != nil {
				return nil, apiLLMConfigResponse{}, err
			}
			if err := a.requireOrganizationAdmin(ctx, ownerID, actor); err != nil {
				return nil, apiLLMConfigResponse{}, err
			}
			cfg, err := a.store.Repository().CreateLLMPolicyConfig(ctx, store.CreateLLMPolicyConfigParams{
				OwnerType:       ownerType,
				OwnerID:         ownerID,
				Name:            in.Name,
				BaseURL:         in.BaseURL,
				EncryptedAPIKey: []byte(in.APIKey),
				Model:           in.Model,
				TimeoutSeconds:  in.TimeoutSeconds,
			})
			if err != nil {
				return nil, apiLLMConfigResponse{}, err
			}
			return nil, apiLLMConfigResponse{Config: apiLLMConfigFromStore(cfg)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "llm_prompt_create", Description: "Create an LLM prompt resource for an owner."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpLLMPromptInput) (*mcp.CallToolResult, apiLLMPromptResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiLLMPromptResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiLLMPromptResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, userID)
			if err != nil {
				return nil, apiLLMPromptResponse{}, err
			}
			if err := a.requireOrganizationAdmin(ctx, ownerID, actor); err != nil {
				return nil, apiLLMPromptResponse{}, err
			}
			prompt, err := a.store.Repository().CreateLLMPromptResource(ctx, store.CreateLLMPromptResourceParams{
				OwnerType: ownerType,
				OwnerID:   ownerID,
				Title:     in.Title,
				Content:   in.Content,
			})
			if err != nil {
				return nil, apiLLMPromptResponse{}, err
			}
			return nil, apiLLMPromptResponse{Prompt: apiLLMPromptFromStore(prompt)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "policy_create", Description: "Create a command security group/policy."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPolicyCreateInput) (*mcp.CallToolResult, apiPolicyResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiPolicyResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiPolicyResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, userID)
			if err != nil {
				return nil, apiPolicyResponse{}, err
			}
			if err := a.requireOrganizationAdmin(ctx, ownerID, actor); err != nil {
				return nil, apiPolicyResponse{}, err
			}
			policy, err := a.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
				OwnerType:         ownerType,
				OwnerID:           ownerID,
				Name:              in.Name,
				DefaultAction:     in.DefaultAction,
				LLMConfigID:       in.LLMConfigID,
				LLMPromptID:       in.LLMPromptID,
				IPAllowlist:       in.IPAllowlist,
				AllowPortForward:  in.AllowPortForward,
				AllowUpload:       in.AllowUpload,
				AllowDownload:     in.AllowDownload,
				AllowInteractive:  in.AllowInteractive,
				AllowManualReview: in.AllowManualReview,
			})
			if err != nil {
				return nil, apiPolicyResponse{}, err
			}
			return nil, apiPolicyResponse{Policy: apiPolicyFromStore(policy)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "policy_rule_add", Description: "Add a whitelist or blacklist rule to a policy."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPolicyRuleInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
			}
			if _, err := a.policyForWrite(ctx, in.PolicyID, actor); err != nil {
				return nil, mcpOK{}, err
			}
			if _, err := a.store.Repository().CreatePolicyRule(ctx, store.CreatePolicyRuleParams{
				PolicyID:    in.PolicyID,
				RuleType:    in.RuleType,
				PatternType: in.PatternType,
				Pattern:     in.Pattern,
			}); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "policy_bind_target", Description: "Bind a policy to an SSH target."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPolicyTargetInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
			}
			policy, err := a.policyForWrite(ctx, in.PolicyID, actor)
			if err != nil {
				return nil, mcpOK{}, err
			}
			target, err := a.store.Repository().GetSSHTarget(ctx, in.TargetID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			if target.OwnerType != policy.OwnerType || target.OwnerID != policy.OwnerID {
				return nil, mcpOK{}, errors.New("policy target must belong to the same owner")
			}
			if err := a.store.Repository().AttachPolicyToTarget(ctx, in.PolicyID, in.TargetID); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "policy_bind_target_tag", Description: "Bind a policy to every SSH target with a tag."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPolicyTagInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			policy, err := a.policyForWrite(ctx, in.PolicyID, actor)
			if err != nil {
				return nil, mcpOK{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, userID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			if ownerType != policy.OwnerType || ownerID != policy.OwnerID {
				return nil, mcpOK{}, errors.New("policy tag must belong to the same owner")
			}
			if err := a.store.Repository().AttachPolicyToTargetTag(ctx, in.PolicyID, ownerType, ownerID, in.Tag); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "policy_bind_user_group", Description: "Bind a policy to an organization user group."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPolicyGroupInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
			}
			policy, err := a.policyForWrite(ctx, in.PolicyID, actor)
			if err != nil {
				return nil, mcpOK{}, err
			}
			group, err := a.store.Repository().GetOrganizationUserGroup(ctx, in.GroupID)
			if err != nil {
				return nil, mcpOK{}, err
			}
			if policy.OwnerType != store.OwnerOrganization || group.OrganizationID != policy.OwnerID {
				return nil, mcpOK{}, errors.New("policy user group must belong to the same organization")
			}
			if err := a.store.Repository().AttachPolicyToUserGroup(ctx, in.PolicyID, in.GroupID); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "audit_list", Description: "List command audit logs for a user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUserInput) (*mcp.CallToolResult, apiAuditLogsResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiAuditLogsResponse{}, err
			}
			userID, err := mcpUserID(actor, in.UserID)
			if err != nil {
				return nil, apiAuditLogsResponse{}, err
			}
			page, err := a.audit.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{UserID: userID, Limit: 100})
			if err != nil {
				return nil, apiAuditLogsResponse{}, err
			}
			out := apiAuditLogsResponse{Logs: []apiAuditLog{}, Total: page.Total, Page: 1, PageSize: 100}
			for _, log := range page.Logs {
				out.Logs = append(out.Logs, apiAuditLogFromStore(log))
			}
			return nil, out, nil
		})

	return s
}

type apiOrganizationsPayload struct {
	Organizations []apiOrganization `json:"organizations"`
}

func apiOrganizationsFromStore(orgs []store.Organization) apiOrganizationsPayload {
	out := apiOrganizationsPayload{}
	for _, org := range orgs {
		out.Organizations = append(out.Organizations, apiOrganizationFromStore(org))
	}
	return out
}

func mcpUserID(actor store.User, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return actor.ID, nil
	}
	if actor.IsSystemAdmin || requested == actor.ID {
		return requested, nil
	}
	return "", errors.New("user_id must match authenticated user")
}

type mcpRegisterInput struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type mcpUserInput struct {
	UserID string `json:"user_id"`
}

type mcpOrgCreateInput struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
}

type mcpOrgInviteInput struct {
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`
	Role           string `json:"role,omitempty"`
}

type mcpJoinInput struct {
	UserID string `json:"user_id"`
	Code   string `json:"code"`
}

type mcpOrgMemberInput struct {
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`
}

type mcpPublicKeyInput struct {
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	AuthorizedKey string `json:"authorized_key"`
}

type mcpOwnerInput struct {
	UserID    string `json:"user_id"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
}

type mcpTargetCreateInput struct {
	mcpOwnerInput
	Name           string   `json:"name,omitempty"`
	Alias          string   `json:"alias"`
	TargetType     string   `json:"target_type"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	RemoteUsername string   `json:"remote_username"`
	AuthType       string   `json:"auth_type"`
	Secret         string   `json:"secret,omitempty"`
	AgentID        string   `json:"agent_id,omitempty"`
	ProxyTargetID  string   `json:"proxy_target_id,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

type mcpTargetDeleteInput struct {
	UserID   string `json:"user_id"`
	TargetID string `json:"target_id"`
}

type mcpAgentEnrollmentInput struct {
	mcpOwnerInput
	Label       string `json:"label"`
	DefaultHost string `json:"default_host,omitempty"`
	DefaultPort int    `json:"default_port,omitempty"`
}

type mcpLLMConfigInput struct {
	mcpOwnerInput
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key,omitempty"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type mcpLLMPromptInput struct {
	mcpOwnerInput
	Title   string `json:"title"`
	Content string `json:"content"`
}

type mcpPolicyCreateInput struct {
	mcpOwnerInput
	Name              string `json:"name"`
	DefaultAction     string `json:"default_action"`
	LLMConfigID       string `json:"llm_config_id,omitempty"`
	LLMPromptID       string `json:"llm_prompt_id,omitempty"`
	IPAllowlist       string `json:"ip_allowlist,omitempty"`
	AllowPortForward  bool   `json:"allow_port_forward,omitempty"`
	AllowUpload       bool   `json:"allow_upload,omitempty"`
	AllowDownload     bool   `json:"allow_download,omitempty"`
	AllowInteractive  bool   `json:"allow_interactive,omitempty"`
	AllowManualReview bool   `json:"allow_manual_review,omitempty"`
}

type mcpPolicyRuleInput struct {
	PolicyID    string `json:"policy_id"`
	RuleType    string `json:"rule_type"`
	PatternType string `json:"pattern_type"`
	Pattern     string `json:"pattern"`
}

type mcpPolicyTargetInput struct {
	PolicyID string `json:"policy_id"`
	TargetID string `json:"target_id"`
}

type mcpPolicyTagInput struct {
	mcpOwnerInput
	PolicyID string `json:"policy_id"`
	Tag      string `json:"tag"`
}

type mcpPolicyGroupInput struct {
	PolicyID string `json:"policy_id"`
	GroupID  string `json:"group_id"`
}
