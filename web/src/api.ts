import type {
  AdminOrg,
  AdminUser,
  AuditLog,
  LLMConfig,
  Member,
  Organization,
  Owner,
  Policy,
  PromptResource,
  Providers,
  PublicKey,
  Runtime,
  Target,
  User,
  UserGroup,
} from "./types";

const jsonHeaders = { "Content-Type": "application/json" };

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, { credentials: "same-origin", ...options });
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new ApiError(data?.error || `${response.status} ${response.statusText}`, response.status);
  }
  return data as T;
}

export const api = {
  me: () => request<{ user: User; organizations: Organization[]; runtime: Runtime }>("/api/me"),
  changeOwnPassword: (body: Record<string, unknown>) => request<void>("/api/me/password", put(body)),
  authProviders: () => request<Providers>("/api/auth/providers"),
  register: (body: Record<string, unknown>) => request<{ user: User }>("/api/auth/register", post(body)),
  login: (body: Record<string, unknown>) => request<{ user: User }>("/api/auth/login", post(body)),
  logout: () => request<void>("/api/auth/logout", post({})),

  createOrg: (body: Record<string, unknown>) => request<{ organization: Organization }>("/api/orgs", post(body)),
  joinOrg: (code: string) => request<{ organization: Organization }>("/api/orgs/join", post({ code })),
  invite: (orgID: string, role: string) => request<{ code: string }>(`/api/orgs/${orgID}/invites`, post({ role })),
  leaveOrg: (id: string) => request<void>(`/api/orgs/${id}/leave`, post({})),
  orgMembers: (orgID: string) => request<{ members: Member[] }>(`/api/orgs/${orgID}/members`),
  addOrgMember: (orgID: string, body: Record<string, unknown>) => request<void>(`/api/orgs/${orgID}/members`, post(body)),
  updateOrgMember: (orgID: string, userID: string, body: Record<string, unknown>) =>
    request<void>(`/api/orgs/${orgID}/members/${userID}`, patch(body)),
  removeOrgMember: (orgID: string, userID: string) =>
    request<void>(`/api/orgs/${orgID}/members/${userID}`, { method: "DELETE" }),
  transferOrgOwner: (orgID: string, userID: string) => request<void>(`/api/orgs/${orgID}/transfer-owner`, post({ user_id: userID })),
  groups: (orgID: string) => request<{ groups: UserGroup[] }>(`/api/orgs/${orgID}/groups`),
  createGroup: (orgID: string, body: Record<string, unknown>) => request<void>(`/api/orgs/${orgID}/groups`, post(body)),

  keys: () => request<{ keys: PublicKey[] }>("/api/keys"),
  createKey: (body: Record<string, unknown>) => request<{ key: PublicKey }>("/api/keys", post(body)),
  deleteKey: (id: string) => request<void>(`/api/keys/${id}`, { method: "DELETE" }),

  targets: (owner: Owner) => request<{ targets: Target[] }>(`/api/targets?${ownerQuery(owner)}`),
  createTarget: (body: Record<string, unknown>) => request<{ target: Target }>("/api/targets", post(body)),
  updateTarget: (id: string, body: Record<string, unknown>) => request<{ target: Target }>(`/api/targets/${id}`, patch(body)),
  updateTargetTagColor: (body: Record<string, unknown>) => request<void>("/api/target-tags", patch(body)),
  enrollPrivateNode: (body: Record<string, unknown>) => request<Enrollment>("/api/agent-enrollments", post(body)),

  llmConfigs: (owner: Owner) => request<{ configs: LLMConfig[] }>(`/api/llm-configs?${ownerQuery(owner)}`),
  createLLMConfig: (body: Record<string, unknown>) => request<void>("/api/llm-configs", post(body)),
  prompts: (owner: Owner) => request<{ prompts: PromptResource[] }>(`/api/llm-prompts?${ownerQuery(owner)}`),
  createPrompt: (body: Record<string, unknown>) => request<void>("/api/llm-prompts", post(body)),

  policies: (owner: Owner) => request<{ policies: Policy[] }>(`/api/policies?${ownerQuery(owner)}`),
  createPolicy: (body: Record<string, unknown>) => request<{ policy: Policy }>("/api/policies", post(body)),
  updatePolicy: (id: string, body: Record<string, unknown>) => request<{ policy: Policy }>(`/api/policies/${id}`, patch(body)),
  deletePolicy: (id: string) => request<void>(`/api/policies/${id}`, { method: "DELETE" }),
  copyPolicy: (id: string) => request<{ policy: Policy }>(`/api/policies/${id}/copy`, post({})),
  addRule: (policyID: string, body: Record<string, unknown>) => request<void>(`/api/policies/${policyID}/rules`, post(body)),
  bindTarget: (policyID: string, targetID: string) => request<void>(`/api/policies/${policyID}/targets`, post({ target_id: targetID })),
  bindTargetTag: (policyID: string, body: Record<string, unknown>) => request<void>(`/api/policies/${policyID}/target-tags`, post(body)),
  bindGroup: (policyID: string, groupID: string) => request<void>(`/api/policies/${policyID}/user-groups`, post({ group_id: groupID })),

  audit: (params: Record<string, unknown>) => request<{ logs: AuditLog[]; total: number; page: number; page_size: number }>(`/api/audit?${queryString(params)}`),
  auditRecording: (id: string) => request<{ lines: unknown[] }>(`/api/audit/${id}/recording`),

  adminSettings: () => request<Record<string, unknown>>("/api/admin/settings"),
  updateDingTalkSettings: (body: Record<string, unknown>) => request<void>("/api/admin/settings/dingtalk", put(body)),
  updateLDAPSettings: (body: Record<string, unknown>) => request<void>("/api/admin/settings/ldap", put(body)),
  adminUsers: () => request<{ users: AdminUser[] }>("/api/admin/users"),
  updateAdminUser: (id: string, body: Record<string, unknown>) => request<void>(`/api/admin/users/${id}`, patch(body)),
  resetAdminUserPassword: (id: string, body: Record<string, unknown>) => request<void>(`/api/admin/users/${id}/password`, put(body)),
  adminOrgs: () => request<{ organizations: AdminOrg[] }>("/api/admin/orgs"),
  adminOrgMembers: (orgID: string) => request<{ members: Member[] }>(`/api/admin/orgs/${orgID}/members`),
  adminUpdateOrgMember: (orgID: string, userID: string, body: Record<string, unknown>) =>
    request<void>(`/api/admin/orgs/${orgID}/members/${userID}`, patch(body)),
  adminTransferOrgOwner: (orgID: string, userID: string) => request<void>(`/api/admin/orgs/${orgID}/transfer-owner`, post({ user_id: userID })),
};

export type Enrollment = {
  id: string;
  token?: string;
  install_sh?: string;
  install_ps1?: string;
  service_sh?: string;
  service_ps1?: string;
};

function post(body: Record<string, unknown>): RequestInit {
  return { method: "POST", headers: jsonHeaders, body: JSON.stringify(body) };
}

function patch(body: Record<string, unknown>): RequestInit {
  return { method: "PATCH", headers: jsonHeaders, body: JSON.stringify(body) };
}

function put(body: Record<string, unknown>): RequestInit {
  return { method: "PUT", headers: jsonHeaders, body: JSON.stringify(body) };
}

function ownerQuery(owner: Owner) {
  const params = new URLSearchParams();
  params.set("owner_type", owner.owner_type);
  params.set("owner_id", owner.owner_id);
  return params.toString();
}

function queryString(values: Record<string, unknown>) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== null && value !== "") params.set(key, String(value));
  }
  return params.toString();
}
