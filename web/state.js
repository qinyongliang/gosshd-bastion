import { getLocale } from "./i18n.js";
import { fallbackTagColor, normalizeTagColor } from "./tag-colors.js";
import { getTheme } from "./theme.js";

export const state = {
  user: null,
  providers: { dingtalk: { enabled: false } },
  orgs: [],
  activeOrgID: "",
  route: currentRoute(),
  keys: [],
  groups: [],
  members: [],
  targets: [],
  policies: [],
  audit: [],
  llms: [],
  prompts: [],
  adminSettings: {},
  adminUsers: [],
  adminOrgs: [],
  adminMembers: [],
  runtime: { ssh_host: "", ssh_port: 22 },
  selectedAdminOrgID: "",
  targetTagFilters: [],
  targetQuery: "",
  policyQuery: "",
  memberQuery: "",
  memberSort: "role",
  adminUserQuery: "",
  ui: {
    modal: "",
    modalLayer: "",
    drawer: "",
    targetID: "",
    policyID: "",
    adminOrgID: "",
    adminPasswordUserID: "",
    memberUserID: "",
    memberTransferUserID: "",
    sidebarOpen: false,
    agentPlatform: "linux",
    targetCreateMode: "direct",
    targetCreateStep: 0,
    targetCreateDraft: {},
  },
  locale: getLocale(),
  theme: getTheme(),
  authMode: "login",
  notice: "",
  error: "",
  enrollment: null,
  invite: "",
};

export function currentRoute() {
  const path = window.location.pathname.replace(/^\/+/, "") || "dashboard";
  return path.split("/")[0] || "dashboard";
}

export function setRoute(route) {
  state.route = route || "dashboard";
}

export function activeOrg() {
  return state.orgs.find((org) => org.id === state.activeOrgID) || state.orgs[0];
}

export function owner() {
  return { owner_type: "organization", owner_id: activeOrg()?.id };
}

export function ownerPayload(data) {
  return { ...data, owner_type: "organization", owner_id: activeOrg().id };
}

export function canManageActiveOrg() {
  const org = activeOrg();
  return Boolean(state.user?.is_system_admin || org?.role === "owner" || org?.role === "admin");
}

export function canTransferActiveOrg() {
  const org = activeOrg();
  return Boolean(state.user?.is_system_admin || org?.role === "owner");
}

export function splitTags(raw) {
  return String(raw || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function allTargetTags() {
  return [...new Set(state.targets.flatMap((target) => target.tags || []))].sort((a, b) => a.localeCompare(b));
}

export function allTargetTagDetails() {
  const tags = new Map();
  for (const target of state.targets) {
    for (const tag of target.tags || []) {
      if (!tags.has(tag)) tags.set(tag, tagColorForName(tag));
    }
  }
  return [...tags.entries()]
    .map(([name, color]) => ({ name, color }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function tagColorForName(name) {
  for (const target of state.targets) {
    const color = normalizeTagColor(target.tag_colors?.[name]);
    if (color) return color;
  }
  return fallbackTagColor(name);
}

export function filteredTargets() {
  const query = state.targetQuery.trim().toLowerCase();
  return state.targets.filter((target) => {
    const tags = target.tags || [];
    const matchesTags = !state.targetTagFilters.length || state.targetTagFilters.every((tag) => tags.includes(tag));
    const haystack = [target.name, target.alias, target.host, target.remote_username, target.target_type, target.auth_type, tags.join(" ")]
      .join(" ")
      .toLowerCase();
    return matchesTags && (!query || haystack.includes(query));
  });
}

export function filteredPolicies() {
  const query = state.policyQuery.trim().toLowerCase();
  if (!query) return state.policies;
  return state.policies.filter((policy) => {
    const haystack = [
      policy.name,
      policy.default_action,
      (policy.target_tags || []).join(" "),
      (policy.rules || []).map((rule) => `${rule.rule_type} ${rule.pattern_type} ${rule.pattern}`).join(" "),
    ]
      .join(" ")
      .toLowerCase();
    return haystack.includes(query);
  });
}

export function filteredMembers() {
  const query = state.memberQuery.trim().toLowerCase();
  const rows = state.members.filter((member) => {
    if (!query) return true;
    return [
      member.email,
      member.display_name,
      member.role,
      member.user_id,
    ]
      .join(" ")
      .toLowerCase()
      .includes(query);
  });
  return [...rows].sort((a, b) => compareMembers(a, b, state.memberSort));
}

export function filteredAdminUsers() {
  const query = state.adminUserQuery.trim().toLowerCase();
  if (!query) return state.adminUsers;
  return state.adminUsers.filter((user) => [user.email, user.display_name, user.auth_provider]
    .join(" ")
    .toLowerCase()
    .includes(query));
}

function compareMembers(a, b, sort) {
  if (sort === "name") return memberName(a).localeCompare(memberName(b));
  if (sort === "joined_desc") return dateValue(b.created_at) - dateValue(a.created_at);
  if (sort === "joined_asc") return dateValue(a.created_at) - dateValue(b.created_at);
  const roleDelta = roleRank(a.role) - roleRank(b.role);
  if (roleDelta) return roleDelta;
  return memberName(a).localeCompare(memberName(b));
}

function memberName(member) {
  return String(member.display_name || member.email || member.user_id || "");
}

function roleRank(role) {
  return { owner: 0, admin: 1, member: 2 }[role] ?? 3;
}

function dateValue(value) {
  const time = new Date(value || 0).getTime();
  return Number.isNaN(time) ? 0 : time;
}
