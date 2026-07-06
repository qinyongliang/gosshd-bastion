package store

import "time"

const (
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

	RequestExec        = "exec"
	RequestShell       = "shell"
	RequestWebTerminal = "web_terminal"
	RequestSFTP        = "sftp"
	RequestForward     = "forward"

	DefaultLLMPromptTitle   = "Default SSH Command Review"
	DefaultLLMPromptContent = "You are reviewing an SSH command for a bastion host. Respond with JSON only: {\"allow\":true|false,\"reason\":\"short reason\"}. When allow is true, reason may be omitted or empty. When allow is false, include a short reason. Do not output chain-of-thought, analysis, or reasoning steps. Deny destructive, privilege-escalation, persistence, credential-exfiltration, and unclear high-risk commands unless there is an explicit safe operational reason."

	DefaultManualReviewTimeoutSeconds = 30
)

type User struct {
	ID            string
	Email         string
	DisplayName   string
	PasswordHash  []byte
	IsSystemAdmin bool
	AuthProvider  string
	DisabledAt    *time.Time
	CreatedAt     time.Time
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
	IsPersonal  bool
	CreatedAt   time.Time
}

type OrganizationMember struct {
	OrganizationID string
	UserID         string
	Role           string
	CreatedAt      time.Time
}

type OrganizationMemberWithUser struct {
	OrganizationID string
	UserID         string
	Email          string
	DisplayName    string
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

type MCPToken struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  []byte
	TokenValue string
	ToolGroups []string
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

type SSHTarget struct {
	ID              string
	OwnerType       string
	OwnerID         string
	Name            string
	Alias           string
	TargetType      string
	Host            string
	Port            int
	RemoteUsername  string
	AuthType        string
	EncryptedSecret []byte
	AgentID         string
	ProxyTargetID   string
	CredentialID    string
	FolderID        string
	Tags            []string
	TagColors       map[string]string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SSHCredential struct {
	ID              string
	OwnerType       string
	OwnerID         string
	Name            string
	Username        string
	AuthType        string
	EncryptedSecret []byte
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type TargetFolder struct {
	ID        string
	OwnerType string
	OwnerID   string
	ParentID  string
	Name      string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type BatchCommandHistory struct {
	ID           string
	OwnerType    string
	OwnerID      string
	Command      string
	ExecuteCount int
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UpsertBatchCommandHistoryParams struct {
	OwnerType string
	OwnerID   string
	Command   string
	CreatedBy string
}

type BatchCommandHistoryFilter struct {
	OwnerType string
	OwnerID   string
	Query     string
	Limit     int
	Offset    int
}

type BatchCommandHistoryPage struct {
	Histories []BatchCommandHistory
	Total     int
}

type TargetTag struct {
	ID        string
	OwnerType string
	OwnerID   string
	Name      string
	Color     string
	CreatedAt time.Time
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
	ID                         string
	OwnerType                  string
	OwnerID                    string
	Name                       string
	DefaultAction              string
	LLMConfigID                string
	LLMPromptID                string
	IPAllowlist                string
	AllowPortForward           bool
	AllowUpload                bool
	AllowDownload              bool
	AllowSSHInteractive        bool
	AllowWebTerminal           bool
	AllowManualReview          bool
	ManualReviewTimeoutSeconds int
	CreatedAt                  time.Time
	Rules                      []PolicyRule
	TargetIDs                  []string
	UserGroupIDs               []string
	TargetTags                 []string
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
	TimeoutSeconds  int
	CreatedAt       time.Time
}

type LLMPromptResource struct {
	ID         string
	OwnerType  string
	OwnerID    string
	Title      string
	Content    string
	IsDefault  bool
	IsReadonly bool
	CreatedAt  time.Time
}

type ExternalIdentity struct {
	ID             string
	UserID         string
	Provider       string
	Subject        string
	Email          string
	DisplayName    string
	RawProfileJSON string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SystemSetting struct {
	Key       string
	ValueJSON string
	UpdatedAt time.Time
	UpdatedBy string
}

type OAuthState struct {
	StateHash     []byte
	Provider      string
	RedirectAfter string
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

type CreateLLMPolicyConfigParams struct {
	OwnerType       string
	OwnerID         string
	Name            string
	BaseURL         string
	EncryptedAPIKey []byte
	Model           string
	TimeoutSeconds  int
}

type UpdateLLMPolicyConfigParams struct {
	Name            string
	BaseURL         string
	EncryptedAPIKey []byte
	Model           string
	TimeoutSeconds  int
}

type CreateLLMPromptResourceParams struct {
	OwnerType  string
	OwnerID    string
	Title      string
	Content    string
	IsDefault  bool
	IsReadonly bool
}

type UpdateLLMPromptResourceParams struct {
	Title   string
	Content string
}

type CommandAuditLog struct {
	ID                   string
	UserID               string
	UserEmail            string
	UserDisplayName      string
	TargetID             string
	TargetName           string
	TargetAlias          string
	TargetHost           string
	TargetPort           int
	TargetUsername       string
	OrganizationID       string
	SessionID            string
	Command              string
	RequestType          string
	PolicyDecision       string
	PolicyReason         string
	PublicKeyFingerprint string
	PublicKeyName        string
	ExitCode             *int
	StartedAt            time.Time
	EndedAt              *time.Time
	RemoteAddress        string
	RecordingPath        string
	RecordingSize        int64
	RecordingSHA256      string
	RecordingDurationMS  int64
	RecordingWidth       int
	RecordingHeight      int
}

type CreateUserParams struct {
	Email         string
	DisplayName   string
	PasswordHash  []byte
	IsSystemAdmin bool
	AuthProvider  string
}

type CreateExternalIdentityParams struct {
	UserID         string
	Provider       string
	Subject        string
	Email          string
	DisplayName    string
	RawProfileJSON string
}

type CreateOrganizationParams struct {
	Name        string
	Slug        string
	OwnerUserID string
	IsPersonal  bool
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

type CreateMCPTokenParams struct {
	UserID     string
	Name       string
	TokenHash  []byte
	TokenValue string
	ToolGroups []string
}

type UpdateMCPTokenParams struct {
	UserID     string
	TokenID    string
	ToolGroups []string
}

type CreateSSHTargetParams struct {
	OwnerType       string
	OwnerID         string
	Name            string
	Alias           string
	TargetType      string
	Host            string
	Port            int
	RemoteUsername  string
	AuthType        string
	EncryptedSecret []byte
	AgentID         string
	ProxyTargetID   string
	CredentialID    string
	FolderID        string
	Tags            []string
	CreatedBy       string
}

type UpdateSSHTargetParams struct {
	Name              string
	Alias             string
	Host              string
	Port              int
	RemoteUsername    string
	AuthType          string
	EncryptedSecret   []byte
	AgentID           string
	ProxyTargetID     string
	ReplaceProxy      bool
	CredentialID      string
	ReplaceCredential bool
	FolderID          string
	ReplaceFolder     bool
	Tags              []string
	ReplaceTags       bool
}

type SSHTargetFilter struct {
	OwnerType string
	OwnerID   string
	Tags      []string
}

type CreateSSHCredentialParams struct {
	OwnerType       string
	OwnerID         string
	Name            string
	Username        string
	AuthType        string
	EncryptedSecret []byte
	CreatedBy       string
}

type UpdateSSHCredentialParams struct {
	Name            string
	Username        string
	AuthType        string
	EncryptedSecret []byte
}

type CreateTargetFolderParams struct {
	OwnerType string
	OwnerID   string
	ParentID  string
	Name      string
	CreatedBy string
}

type UpdateTargetFolderParams struct {
	ParentID      string
	ReplaceParent bool
	Name          string
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
	ID               string
	OwnerType        string
	OwnerID          string
	EnrollmentID     string
	Label            string
	CurrentRuntimeID string
}

type CreateCommandPolicyParams struct {
	OwnerType                  string
	OwnerID                    string
	Name                       string
	DefaultAction              string
	LLMConfigID                string
	LLMPromptID                string
	IPAllowlist                string
	AllowPortForward           bool
	AllowUpload                bool
	AllowDownload              bool
	AllowSSHInteractive        bool
	AllowWebTerminal           bool
	AllowManualReview          bool
	ManualReviewTimeoutSeconds int
}

type UpdateCommandPolicyParams struct {
	Name                       string
	DefaultAction              string
	LLMConfigID                string
	LLMPromptID                string
	IPAllowlist                string
	AllowPortForward           bool
	AllowUpload                bool
	AllowDownload              bool
	AllowSSHInteractive        bool
	AllowWebTerminal           bool
	AllowManualReview          bool
	ManualReviewTimeoutSeconds int
}

type CreatePolicyRuleParams struct {
	PolicyID    string
	RuleType    string
	PatternType string
	Pattern     string
}

type CreateCommandAuditLogParams struct {
	UserID               string
	UserEmail            string
	UserDisplayName      string
	TargetID             string
	TargetName           string
	TargetAlias          string
	TargetHost           string
	TargetPort           int
	TargetUsername       string
	OrganizationID       string
	PublicKeyFingerprint string
	PublicKeyName        string
	SessionID            string
	Command              string
	RequestType          string
	PolicyDecision       string
	PolicyReason         string
	ExitCode             *int
	StartedAt            time.Time
	EndedAt              *time.Time
	RemoteAddress        string
	RecordingPath        string
	RecordingSize        int64
	RecordingSHA256      string
	RecordingDurationMS  int64
	RecordingWidth       int
	RecordingHeight      int
}

type CompleteCommandAuditLogParams struct {
	ID                  string
	ExitCode            *int
	EndedAt             time.Time
	RecordingPath       string
	RecordingSize       int64
	RecordingSHA256     string
	RecordingDurationMS int64
	RecordingWidth      int
	RecordingHeight     int
}

type AuditLogFilter struct {
	UserID         string
	OrganizationID string
	TargetID       string
	Query          string
	PolicyDecision string
	RequestType    string
	StartedFrom    time.Time
	StartedTo      time.Time
	Limit          int
	Offset         int
}

type AuditLogPage struct {
	Logs  []CommandAuditLog
	Total int
}
