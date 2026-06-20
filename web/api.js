const jsonHeaders = { "Content-Type": "application/json" };

export async function request(path, options = {}) {
  const init = { credentials: "same-origin", ...options };
  const response = await fetch(path, init);
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(data?.error || `${response.status} ${response.statusText}`);
  }
  return data;
}

export const api = {
  me: () => request("/api/me"),
  changeOwnPassword: (body) => request("/api/me/password", put(body)),
  authProviders: () => request("/api/auth/providers"),
  register: (body) => request("/api/auth/register", post(body)),
  login: (body) => request("/api/auth/login", post(body)),
  logout: () => request("/api/auth/logout", post({})),
  createOrg: (body) => request("/api/orgs", post(body)),
  joinOrg: (code) => request("/api/orgs/join", post({ code })),
  leaveOrg: (id) => request(`/api/orgs/${id}/leave`, post({})),
  orgMembers: (orgID) => request(`/api/orgs/${orgID}/members`),
  addOrgMember: (orgID, body) => request(`/api/orgs/${orgID}/members`, post(body)),
  updateOrgMember: (orgID, userID, body) => request(`/api/orgs/${orgID}/members/${userID}`, patch(body)),
  removeOrgMember: (orgID, userID) => request(`/api/orgs/${orgID}/members/${userID}`, { method: "DELETE" }),
  transferOrgOwner: (orgID, userID) => request(`/api/orgs/${orgID}/transfer-owner`, post({ user_id: userID })),
  groups: (orgID) => request(`/api/orgs/${orgID}/groups`),
  createGroup: (orgID, body) => request(`/api/orgs/${orgID}/groups`, post(body)),
  invite: (orgID, role) => request(`/api/orgs/${orgID}/invites`, post({ role })),
  keys: () => request("/api/keys"),
  createKey: (body) => request("/api/keys", post(body)),
  deleteKey: (id) => request(`/api/keys/${id}`, { method: "DELETE" }),
  targets: (owner) => request(`/api/targets?${ownerQuery(owner)}`),
  createTarget: (body) => request("/api/targets", post(body)),
  updateTarget: (id, body) => request(`/api/targets/${id}`, patch(body)),
  updateTargetTagColor: (body) => request("/api/target-tags", patch(body)),
  enrollAgent: (body) => request("/api/agent-enrollments", post(body)),
  llmConfigs: (owner) => request(`/api/llm-configs?${ownerQuery(owner)}`),
  createLLMConfig: (body) => request("/api/llm-configs", post(body)),
  prompts: (owner) => request(`/api/llm-prompts?${ownerQuery(owner)}`),
  createPrompt: (body) => request("/api/llm-prompts", post(body)),
  policies: (owner) => request(`/api/policies?${ownerQuery(owner)}`),
  createPolicy: (body) => request("/api/policies", post(body)),
  updatePolicy: (id, body) => request(`/api/policies/${id}`, patch(body)),
  deletePolicy: (id) => request(`/api/policies/${id}`, { method: "DELETE" }),
  copyPolicy: (id, body) => request(`/api/policies/${id}/copy`, post(body || {})),
  addRule: (policyID, body) => request(`/api/policies/${policyID}/rules`, post(body)),
  bindTarget: (policyID, targetID) => request(`/api/policies/${policyID}/targets`, post({ target_id: targetID })),
  bindTargetTag: (policyID, body) => request(`/api/policies/${policyID}/target-tags`, post(body)),
  bindGroup: (policyID, groupID) => request(`/api/policies/${policyID}/user-groups`, post({ group_id: groupID })),
  audit: (params = {}) => request(`/api/audit?${queryString(params)}`),
  auditRecording: (id) => request(`/api/audit/${id}/recording`),
  adminSettings: () => request("/api/admin/settings"),
  updateDingTalkSettings: (body) => request("/api/admin/settings/dingtalk", put(body)),
  updateLDAPSettings: (body) => request("/api/admin/settings/ldap", put(body)),
  adminUsers: () => request("/api/admin/users"),
  updateAdminUser: (id, body) => request(`/api/admin/users/${id}`, patch(body)),
  resetAdminUserPassword: (id, body) => request(`/api/admin/users/${id}/password`, put(body)),
  adminOrgs: () => request("/api/admin/orgs"),
  adminOrgMembers: (orgID) => request(`/api/admin/orgs/${orgID}/members`),
  adminUpdateOrgMember: (orgID, userID, body) => request(`/api/admin/orgs/${orgID}/members/${userID}`, patch(body)),
  adminTransferOrgOwner: (orgID, userID) => request(`/api/admin/orgs/${orgID}/transfer-owner`, post({ user_id: userID })),
};

function post(body) {
  return { method: "POST", headers: jsonHeaders, body: JSON.stringify(body) };
}

function patch(body) {
  return { method: "PATCH", headers: jsonHeaders, body: JSON.stringify(body) };
}

function put(body) {
  return { method: "PUT", headers: jsonHeaders, body: JSON.stringify(body) };
}

function ownerQuery(owner) {
  const params = new URLSearchParams();
  if (owner?.owner_type) params.set("owner_type", owner.owner_type);
  if (owner?.owner_id) params.set("owner_id", owner.owner_id);
  return params.toString();
}

function queryString(values) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values || {})) {
    if (value !== undefined && value !== null && value !== "") params.set(key, value);
  }
  return params.toString();
}
