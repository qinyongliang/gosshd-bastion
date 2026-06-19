import { getLocale } from "./i18n.js";
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
  selectedAdminOrgID: "",
  targetTagFilters: [],
  targetQuery: "",
  policyQuery: "",
  adminUserQuery: "",
  ui: {
    modal: "",
    drawer: "",
    targetID: "",
    policyID: "",
    adminOrgID: "",
    agentPlatform: "linux",
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

export function filteredAdminUsers() {
  const query = state.adminUserQuery.trim().toLowerCase();
  if (!query) return state.adminUsers;
  return state.adminUsers.filter((user) => [user.email, user.display_name, user.auth_provider]
    .join(" ")
    .toLowerCase()
    .includes(query));
}
