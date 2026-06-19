import { getLocale } from "./i18n.js";

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
  locale: getLocale(),
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
  if (!state.targetTagFilters.length) return state.targets;
  return state.targets.filter((target) => state.targetTagFilters.every((tag) => (target.tags || []).includes(tag)));
}
