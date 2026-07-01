import type {
  AdminOrg,
  AdminUser,
  AuditLog,
  AuditRecording,
  FileEntry,
  FileProperties,
  FileReadResult,
  LLMConfig,
  ManualReview,
  Member,
  MCPToken,
  MCPTokenCreateResponse,
  Organization,
  Owner,
  Policy,
  PromptResource,
  Providers,
  PublicKey,
  Runtime,
  Target,
  SSHCredential,
  TargetFolder,
  TargetSystemSnapshot,
  User,
  UserSettings,
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
  mcpTokens: () => request<{ tokens: MCPToken[] }>("/api/mcp-tokens"),
  createMCPToken: (body: Record<string, unknown>) => request<MCPTokenCreateResponse>("/api/mcp-tokens", post(body)),
  updateMCPToken: (id: string, body: Record<string, unknown>) => request<{ token: MCPToken }>(`/api/mcp-tokens/${id}`, patch(body)),
  deleteMCPToken: (id: string) => request<void>(`/api/mcp-tokens/${id}`, { method: "DELETE" }),

  targets: (owner: Owner) => request<{ targets: Target[] }>(`/api/targets?${ownerQuery(owner)}`),
  createTarget: (body: Record<string, unknown>) => request<{ target: Target }>("/api/targets", post(body)),
  copyTarget: (id: string) => request<{ target: Target }>(`/api/targets/${id}/copy`, post({})),
  updateTarget: (id: string, body: Record<string, unknown>) => request<{ target: Target }>(`/api/targets/${id}`, patch(body)),
  deleteTarget: (id: string) => request<void>(`/api/targets/${id}`, { method: "DELETE" }),
  credentials: (owner: Owner) => request<{ credentials: SSHCredential[] }>(`/api/credentials?${ownerQuery(owner)}`),
  createCredential: (body: Record<string, unknown>) => request<{ credential: SSHCredential }>("/api/credentials", post(body)),
  updateCredential: (id: string, body: Record<string, unknown>) => request<{ credential: SSHCredential }>(`/api/credentials/${id}`, patch(body)),
  deleteCredential: (id: string) => request<void>(`/api/credentials/${id}`, { method: "DELETE" }),
  targetFolders: (owner: Owner) => request<{ folders: TargetFolder[] }>(`/api/target-folders?${ownerQuery(owner)}`),
  createTargetFolder: (body: Record<string, unknown>) => request<{ folder: TargetFolder }>("/api/target-folders", post(body)),
  updateTargetFolder: (id: string, body: Record<string, unknown>) => request<{ folder: TargetFolder }>(`/api/target-folders/${id}`, patch(body)),
  deleteTargetFolder: (id: string) => request<void>(`/api/target-folders/${id}`, { method: "DELETE" }),
  mySettings: () => request<UserSettings>("/api/me/settings"),
  updateMySettings: (body: Record<string, unknown>) => request<UserSettings>("/api/me/settings", put(body)),
  updateTargetTagColor: (body: Record<string, unknown>) => request<void>("/api/target-tags", patch(body)),
  enrollPrivateNode: (body: Record<string, unknown>) => request<Enrollment>("/api/agent-enrollments", post(body)),

  llmConfigs: (owner: Owner) => request<{ configs: LLMConfig[] }>(`/api/llm-configs?${ownerQuery(owner)}`),
  createLLMConfig: (body: Record<string, unknown>) => request<{ config: LLMConfig }>("/api/llm-configs", post(body)),
  updateLLMConfig: (id: string, body: Record<string, unknown>) => request<{ config: LLMConfig }>(`/api/llm-configs/${id}`, patch(body)),
  deleteLLMConfig: (id: string) => request<void>(`/api/llm-configs/${id}`, { method: "DELETE" }),
  prompts: (owner: Owner) => request<{ prompts: PromptResource[] }>(`/api/llm-prompts?${ownerQuery(owner)}`),
  createPrompt: (body: Record<string, unknown>) => request<{ prompt: PromptResource }>("/api/llm-prompts", post(body)),
  updatePrompt: (id: string, body: Record<string, unknown>) => request<{ prompt: PromptResource }>(`/api/llm-prompts/${id}`, patch(body)),
  deletePrompt: (id: string) => request<void>(`/api/llm-prompts/${id}`, { method: "DELETE" }),

  policies: (owner: Owner) => request<{ policies: Policy[] }>(`/api/policies?${ownerQuery(owner)}`),
  createPolicy: (body: Record<string, unknown>) => request<{ policy: Policy }>("/api/policies", post(body)),
  updatePolicy: (id: string, body: Record<string, unknown>) => request<{ policy: Policy }>(`/api/policies/${id}`, patch(body)),
  deletePolicy: (id: string) => request<void>(`/api/policies/${id}`, { method: "DELETE" }),
  copyPolicy: (id: string) => request<{ policy: Policy }>(`/api/policies/${id}/copy`, post({})),
  addRule: (policyID: string, body: Record<string, unknown>) => request<void>(`/api/policies/${policyID}/rules`, post(body)),
  deleteRule: (policyID: string, ruleID: string) => request<void>(`/api/policies/${policyID}/rules/${ruleID}`, { method: "DELETE" }),
  bindTarget: (policyID: string, targetID: string) => request<void>(`/api/policies/${policyID}/targets`, post({ target_id: targetID })),
  unbindTarget: (policyID: string, targetID: string) => request<void>(`/api/policies/${policyID}/targets/${targetID}`, { method: "DELETE" }),
  bindTargetTag: (policyID: string, body: Record<string, unknown>) => request<void>(`/api/policies/${policyID}/target-tags`, post(body)),
  unbindTargetTag: (policyID: string, tag: string) => request<void>(`/api/policies/${policyID}/target-tags/${encodeURIComponent(tag)}`, { method: "DELETE" }),
  bindGroup: (policyID: string, groupID: string) => request<void>(`/api/policies/${policyID}/user-groups`, post({ group_id: groupID })),
  unbindGroup: (policyID: string, groupID: string) => request<void>(`/api/policies/${policyID}/user-groups/${groupID}`, { method: "DELETE" }),

  audit: (params: Record<string, unknown>) => request<{ logs: AuditLog[]; total: number; page: number; page_size: number }>(`/api/audit?${queryString(params)}`),
  auditRecording: (id: string) => request<AuditRecording>(`/api/audit/${id}/recording`),
  targetSystem: (targetID: string) => request<TargetSystemSnapshot>(`/api/targets/${targetID}/system`),
  manualReviews: (orgID: string, timeoutSeconds = 25, knownIDs: string[] = [], sessionID = "") =>
    request<{ reviews: ManualReview[] }>(`/api/manual-reviews?${queryString({
      organization_id: orgID,
      session_id: sessionID,
      timeout_seconds: timeoutSeconds,
      known_ids: knownIDs.join(","),
    })}`),
  decideManualReview: (id: string, allow: boolean) => request<{ ok: true }>(`/api/manual-reviews/${id}/decision`, post({ allow })),

  adminSettings: () => request<Record<string, unknown>>("/api/admin/settings"),
  updateBrandingSettings: (body: Record<string, unknown>) => request<void>("/api/admin/settings/branding", put(body)),
  updateAuthSettings: (body: Record<string, unknown>) => request<void>("/api/admin/settings/auth", put(body)),
  updateDingTalkSettings: (body: Record<string, unknown>) => request<void>("/api/admin/settings/dingtalk", put(body)),
  updateLDAPSettings: (body: Record<string, unknown>) => request<void>("/api/admin/settings/ldap", put(body)),
  adminUsers: () => request<{ users: AdminUser[] }>("/api/admin/users"),
  updateAdminUser: (id: string, body: Record<string, unknown>) => request<void>(`/api/admin/users/${id}`, patch(body)),
  deleteAdminUser: (id: string) => request<void>(`/api/admin/users/${id}`, { method: "DELETE" }),
  resetAdminUserPassword: (id: string, body: Record<string, unknown>) => request<void>(`/api/admin/users/${id}/password`, put(body)),
  adminOrgs: () => request<{ organizations: AdminOrg[] }>("/api/admin/orgs"),
  deleteAdminOrg: (id: string) => request<void>(`/api/admin/orgs/${id}`, { method: "DELETE" }),
  adminOrgMembers: (orgID: string) => request<{ members: Member[] }>(`/api/admin/orgs/${orgID}/members`),
  adminUpdateOrgMember: (orgID: string, userID: string, body: Record<string, unknown>) =>
    request<void>(`/api/admin/orgs/${orgID}/members/${userID}`, patch(body)),
  adminTransferOrgOwner: (orgID: string, userID: string) => request<void>(`/api/admin/orgs/${orgID}/transfer-owner`, post({ user_id: userID })),

  targetTerminalURL: (targetID: string, cols: number, rows: number) => terminalURL(`/api/targets/${targetID}/terminal/ws`, cols, rows),
  listFiles: (targetID: string, path: string, sort?: string, order?: string) => request<{ path: string; entries: FileEntry[] }>(`/api/targets/${targetID}/files?${queryString({ path, sort, order })}`),
  fileProperties: (targetID: string, path: string) => request<FileProperties>(`/api/targets/${targetID}/files/stat?${queryString({ path })}`),
  readFile: (targetID: string, path: string) => request<FileReadResult>(`/api/targets/${targetID}/files/read?${queryString({ path })}`),
  writeFile: (targetID: string, path: string, content: string) => request<{ path: string; size: number }>(`/api/targets/${targetID}/files/write`, post({ path, content })),
  touchFile: (targetID: string, path: string) => request<{ path: string }>(`/api/targets/${targetID}/files/touch`, post({ path })),
  downloadFile: (targetID: string, path: string) => `/api/targets/${targetID}/files/download?${queryString({ path })}`,
  openFile: (targetID: string, path: string) => request<{ path: string }>(`/api/targets/${targetID}/files/open?${queryString({ path })}`, post({})),
  mkdirFile: (targetID: string, path: string) => request<{ path: string }>(`/api/targets/${targetID}/files/mkdir`, post({ path })),
  deleteFile: (targetID: string, path: string) => request<{ path: string }>(`/api/targets/${targetID}/files/delete`, post({ path })),
  moveFile: (targetID: string, source: string, destination: string) => request<{ source: string; destination: string }>(`/api/targets/${targetID}/files/move`, post({ source, destination })),
  copyFile: (targetID: string, source: string, destination: string) => request<{ source: string; destination: string }>(`/api/targets/${targetID}/files/copy`, post({ source, destination })),
  uploadFile: (targetID: string, path: string, file: File) => {
    const body = new FormData();
    body.append("file", file);
    return request<{ path: string }>(`/api/targets/${targetID}/files/upload?${queryString({ path })}`, { method: "POST", body });
  },
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

function terminalURL(path: string, cols: number, rows: number) {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const host = window.location.host;
  const params = new URLSearchParams({ cols: String(cols), rows: String(rows) });
  return `${protocol}//${host}${path}?${params.toString()}`;
}
