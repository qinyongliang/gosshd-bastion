package server

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd/internal/store"
)

type mcpOK struct {
	OK bool `json:"ok"`
}

func (a *App) mcpHandler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return a.newMCPServer()
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return handler
}

func (a *App) newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "gosshd-bastion", Version: a.cfg.version()}, nil)

	mcp.AddTool(s, &mcp.Tool{Name: "auth_register", Description: "Register a user and create their personal organization."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpRegisterInput) (*mcp.CallToolResult, apiUserResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiUserResponse{}, err
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
			orgs, err := a.store.Repository().ListOrganizationsForUser(ctx, in.UserID)
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
			org, err := a.store.Repository().CreateOrganization(ctx, store.CreateOrganizationParams{
				Name:        in.Name,
				Slug:        in.Slug,
				OwnerUserID: in.UserID,
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
			if _, err := a.store.Repository().CreateOrganizationInvite(ctx, store.CreateOrganizationInviteParams{
				OrganizationID: in.OrganizationID,
				CodeHash:       hash,
				Role:           role,
				ExpiresAt:      time.Now().UTC().Add(7 * 24 * time.Hour),
				CreatedBy:      in.UserID,
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
			org, err := a.joinOrganizationWithCode(ctx, in.UserID, in.Code)
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
			if err := a.store.Repository().LeaveOrganization(ctx, in.OrganizationID, in.UserID); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "public_key_add", Description: "Add an SSH public key for a user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPublicKeyInput) (*mcp.CallToolResult, apiPublicKeyResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiPublicKeyResponse{}, err
			}
			normalized, fingerprint, err := a.bastion.NormalizeAuthorizedKey(in.AuthorizedKey)
			if err != nil {
				return nil, apiPublicKeyResponse{}, err
			}
			key, err := a.store.Repository().CreatePublicKey(ctx, store.CreatePublicKeyParams{
				UserID:        in.UserID,
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
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, in.UserID)
			if err != nil {
				return nil, apiTargetResponse{}, err
			}
			target, err := a.store.Repository().CreateSSHTarget(ctx, store.CreateSSHTargetParams{
				OwnerType:       ownerType,
				OwnerID:         ownerID,
				Alias:           in.Alias,
				TargetType:      in.TargetType,
				Host:            in.Host,
				Port:            in.Port,
				RemoteUsername:  in.RemoteUsername,
				AuthType:        in.AuthType,
				EncryptedSecret: []byte(in.Secret),
				AgentID:         in.AgentID,
				CreatedBy:       in.UserID,
			})
			if err != nil {
				return nil, apiTargetResponse{}, err
			}
			return nil, apiTargetResponse{Target: apiTargetFromStore(target)}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "agent_enrollment_create", Description: "Create an agent enrollment and install commands."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAgentEnrollmentInput) (*mcp.CallToolResult, apiAgentEnrollmentResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, in.UserID)
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			token, hash, err := randomCode()
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			enrollment, err := a.store.Repository().CreateAgentEnrollment(ctx, store.CreateAgentEnrollmentParams{
				OwnerType:   ownerType,
				OwnerID:     ownerID,
				TokenHash:   hash,
				Label:       in.Label,
				DefaultHost: in.DefaultHost,
				DefaultPort: in.DefaultPort,
				CreatedBy:   in.UserID,
				ExpiresAt:   time.Now().UTC().Add(30 * 24 * time.Hour),
			})
			if err != nil {
				return nil, apiAgentEnrollmentResponse{}, err
			}
			return nil, apiAgentEnrollmentResponse{ID: enrollment.ID, Token: token}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "llm_config_create", Description: "Create an LLM provider config for an owner."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpLLMConfigInput) (*mcp.CallToolResult, apiLLMConfigResponse, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, apiLLMConfigResponse{}, err
			}
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, in.UserID)
			if err != nil {
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
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, in.UserID)
			if err != nil {
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
			ownerType, ownerID, err := a.resolveOwner(ctx, in.OwnerType, in.OwnerID, in.UserID)
			if err != nil {
				return nil, apiPolicyResponse{}, err
			}
			policy, err := a.store.Repository().CreateCommandPolicy(ctx, store.CreateCommandPolicyParams{
				OwnerType:     ownerType,
				OwnerID:       ownerID,
				Name:          in.Name,
				DefaultAction: in.DefaultAction,
				LLMConfigID:   in.LLMConfigID,
				LLMPromptID:   in.LLMPromptID,
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
			if err := a.store.Repository().AttachPolicyToTarget(ctx, in.PolicyID, in.TargetID); err != nil {
				return nil, mcpOK{}, err
			}
			return nil, mcpOK{OK: true}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "policy_bind_user_group", Description: "Bind a policy to an organization user group."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in mcpPolicyGroupInput) (*mcp.CallToolResult, mcpOK, error) {
			if err := a.ensureServices(ctx); err != nil {
				return nil, mcpOK{}, err
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
			logs, err := a.store.Repository().ListCommandAuditLogs(ctx, store.AuditLogFilter{UserID: in.UserID})
			if err != nil {
				return nil, apiAuditLogsResponse{}, err
			}
			out := apiAuditLogsResponse{}
			for _, log := range logs {
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
	OwnerType string `json:"owner_type,omitempty"`
	OwnerID   string `json:"owner_id,omitempty"`
}

type mcpTargetCreateInput struct {
	mcpOwnerInput
	Alias          string `json:"alias"`
	TargetType     string `json:"target_type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	RemoteUsername string `json:"remote_username"`
	AuthType       string `json:"auth_type"`
	Secret         string `json:"secret,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
}

type mcpAgentEnrollmentInput struct {
	mcpOwnerInput
	Label       string `json:"label"`
	DefaultHost string `json:"default_host"`
	DefaultPort int    `json:"default_port"`
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
	Name          string `json:"name"`
	DefaultAction string `json:"default_action"`
	LLMConfigID   string `json:"llm_config_id,omitempty"`
	LLMPromptID   string `json:"llm_prompt_id,omitempty"`
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

type mcpPolicyGroupInput struct {
	PolicyID string `json:"policy_id"`
	GroupID  string `json:"group_id"`
}
