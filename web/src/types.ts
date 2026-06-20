export type Owner = {
  owner_type: "organization";
  owner_id: string;
};

export type User = {
  id: string;
  email: string;
  display_name: string;
  is_system_admin: boolean;
  auth_provider: string;
};

export type Organization = {
  id: string;
  name: string;
  slug: string;
  is_personal: boolean;
  role?: "owner" | "admin" | "member";
};

export type Runtime = {
  ssh_host: string;
  ssh_port: number;
};

export type PublicKey = {
  id: string;
  name: string;
  fingerprint: string;
  created_at: string;
};

export type Member = {
  user_id: string;
  email: string;
  display_name: string;
  role: "owner" | "admin" | "member";
  joined_at?: string;
  created_at?: string;
};

export type UserGroup = {
  id: string;
  name: string;
  slug: string;
  is_default?: boolean;
  members?: Member[];
};

export type Target = {
  id: string;
  owner_type: string;
  owner_id: string;
  target_type: "direct" | "agent";
  name: string;
  alias: string;
  host: string;
  port: number;
  remote_username: string;
  auth_type: "password" | "private_key";
  tags: string[];
  tag_colors?: Record<string, string>;
  endpoint?: string;
  agent_id?: string;
  proxy_target_id?: string;
};

export type TargetTag = {
  tag: string;
  color: string;
};

export type LLMConfig = {
  id: string;
  name: string;
  base_url: string;
  model: string;
  timeout_seconds: number;
};

export type PromptResource = {
  id: string;
  title: string;
  content: string;
  is_default?: boolean;
  is_readonly?: boolean;
};

export type PolicyRule = {
  id: string;
  rule_type: "whitelist" | "blacklist";
  pattern_type: "exact" | "prefix" | "contains";
  pattern: string;
};

export type Policy = {
  id: string;
  name: string;
  default_action: "allow" | "deny";
  llm_config_id?: string;
  llm_prompt_id?: string;
  ip_allowlist?: string;
  allow_port_forward?: boolean;
  allow_upload?: boolean;
  allow_download?: boolean;
  allow_interactive?: boolean;
  rules?: PolicyRule[];
  target_ids?: string[];
  target_tags?: string[];
  user_group_ids?: string[];
};

export type AuditLog = {
  id: string;
  user_display_name?: string;
  user_email?: string;
  public_key_name?: string;
  public_key_fingerprint?: string;
  target_name?: string;
  target_alias?: string;
  target_endpoint?: string;
  command: string;
  request_type: string;
  policy_decision: "allow" | "deny";
  policy_reason: string;
  exit_code?: number;
  started_at: string;
  recording_path?: string;
};

export type AdminUser = User;

export type AdminOrg = Organization & {
  member_count?: number;
};

export type Providers = {
  dingtalk?: { enabled: boolean };
};

export type ConsoleData = {
  user: User;
  orgs: Organization[];
  activeOrg: Organization;
  setActiveOrgID: (id: string) => void;
  runtime: Runtime;
  keys: PublicKey[];
  members: Member[];
  groups: UserGroup[];
  targets: Target[];
  policies: Policy[];
  llms: LLMConfig[];
  prompts: PromptResource[];
  auditPage: { logs: AuditLog[]; total: number; page: number; page_size: number };
  refetchAll: () => void;
};
