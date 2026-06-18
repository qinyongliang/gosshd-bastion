package store

import "time"

const (
	OwnerUser         = "user"
	OwnerOrganization = "organization"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	TargetDirect = "direct"
	TargetAgent  = "agent"

	AuthPassword   = "password"
	AuthPrivateKey = "private_key"

	DecisionAllow = "allow"
	DecisionDeny  = "deny"

	RuleWhitelist = "whitelist"
	RuleBlacklist = "blacklist"

	PatternExact    = "exact"
	PatternPrefix   = "prefix"
	PatternContains = "contains"

	RequestExec  = "exec"
	RequestShell = "shell"
	RequestSFTP  = "sftp"
)

type User struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash []byte
	CreatedAt    time.Time
}

type Session struct {
	ID        string
	UserID    string
	TokenHash []byte
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Organization struct {
	ID          string
	Name        string
	Slug        string
	OwnerUserID string
	CreatedAt   time.Time
}

type OrganizationMember struct {
	OrganizationID string
	UserID         string
	Role           string
	CreatedAt      time.Time
}

type OrganizationUserGroup struct {
	ID             string
	OrganizationID string
	Name           string
	Slug           string
	IsDefault      bool
	CreatedAt      time.Time
}

type OrganizationUserGroupMember struct {
	GroupID   string
	UserID    string
	CreatedAt time.Time
}

type OrganizationInvite struct {
	ID             string
	OrganizationID string
	CodeHash       []byte
	Role           string
	ExpiresAt      time.Time
	CreatedBy      string
	CreatedAt      time.Time
	ConsumedAt     *time.Time
}

type PublicKey struct {
	ID            string
	UserID        string
	Name          string
	AuthorizedKey string
	Fingerprint   string
	CreatedAt     time.Time
}

type SSHTarget struct {
	ID              string
	OwnerType       string
	OwnerID         string
	Alias           string
	TargetType      string
	Host            string
	Port            int
	RemoteUsername  string
	AuthType        string
	EncryptedSecret []byte
	AgentID         string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AgentEnrollment struct {
	ID              string
	OwnerType       string
	OwnerID         string
	TokenHash       []byte
	Label           string
	DefaultHost     string
	DefaultPort     int
	CreatedBy       string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	ConsumedAgentID string
}

type Agent struct {
	ID               string
	OwnerType        string
	OwnerID          string
	EnrollmentID     string
	Label            string
	CurrentRuntimeID string
	LastSeenAt       time.Time
	CreatedAt        time.Time
}

type CommandPolicy struct {
	ID            string
	OwnerType     string
	OwnerID       string
	Name          string
	DefaultAction string
	LLMConfigID   string
	CreatedAt     time.Time
	Rules         []PolicyRule
	UserGroupIDs  []string
}

type PolicyRule struct {
	ID          string
	PolicyID    string
	RuleType    string
	PatternType string
	Pattern     string
	CreatedAt   time.Time
}

type LLMPolicyConfig struct {
	ID              string
	OwnerType       string
	OwnerID         string
	Name            string
	BaseURL         string
	EncryptedAPIKey []byte
	Model           string
	Prompt          string
	TimeoutSeconds  int
	CreatedAt       time.Time
}

type CommandAuditLog struct {
	ID             string
	UserID         string
	TargetID       string
	OrganizationID string
	SessionID      string
	Command        string
	RequestType    string
	PolicyDecision string
	PolicyReason   string
	ExitCode       *int
	StartedAt      time.Time
	EndedAt        *time.Time
	RemoteAddress  string
}

type CreateUserParams struct {
	Email        string
	DisplayName  string
	PasswordHash []byte
}

type CreateOrganizationParams struct {
	Name        string
	Slug        string
	OwnerUserID string
}

type CreateOrganizationUserGroupParams struct {
	OrganizationID string
	Name           string
	Slug           string
	IsDefault      bool
}

type CreateOrganizationInviteParams struct {
	OrganizationID string
	CodeHash       []byte
	Role           string
	ExpiresAt      time.Time
	CreatedBy      string
}

type CreatePublicKeyParams struct {
	UserID        string
	Name          string
	AuthorizedKey string
	Fingerprint   string
}

type CreateSSHTargetParams struct {
	OwnerType       string
	OwnerID         string
	Alias           string
	TargetType      string
	Host            string
	Port            int
	RemoteUsername  string
	AuthType        string
	EncryptedSecret []byte
	AgentID         string
	CreatedBy       string
}

type UpdateSSHTargetParams struct {
	Alias           string
	Host            string
	Port            int
	RemoteUsername  string
	AuthType        string
	EncryptedSecret []byte
	AgentID         string
}

type CreateAgentEnrollmentParams struct {
	OwnerType   string
	OwnerID     string
	TokenHash   []byte
	Label       string
	DefaultHost string
	DefaultPort int
	CreatedBy   string
	ExpiresAt   time.Time
}

type UpsertAgentParams struct {
	OwnerType        string
	OwnerID          string
	EnrollmentID     string
	Label            string
	CurrentRuntimeID string
}

type CreateCommandPolicyParams struct {
	OwnerType     string
	OwnerID       string
	Name          string
	DefaultAction string
	LLMConfigID   string
}

type CreatePolicyRuleParams struct {
	PolicyID    string
	RuleType    string
	PatternType string
	Pattern     string
}

type CreateCommandAuditLogParams struct {
	UserID         string
	TargetID       string
	OrganizationID string
	SessionID      string
	Command        string
	RequestType    string
	PolicyDecision string
	PolicyReason   string
	ExitCode       *int
	StartedAt      time.Time
	EndedAt        *time.Time
	RemoteAddress  string
}

type AuditLogFilter struct {
	UserID   string
	TargetID string
}
