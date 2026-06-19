import { api } from "./api.js";
import { formData } from "./components/forms.js";
import { renderShell } from "./components/layout.js";
import { applyDocumentLocale, setLocale, t } from "./i18n.js";
import { applyDocumentTheme, setTheme } from "./theme.js";
import { routeFromLocation, bindRouter, navigate } from "./router.js";
import {
  activeOrg,
  canManageActiveOrg,
  owner,
  ownerPayload,
  setRoute,
  splitTags,
  state,
} from "./state.js";
import { renderAgents } from "./views/agents.js";
import { renderAudit } from "./views/audit.js";
import { renderAuth } from "./views/auth.js";
import { renderDashboard } from "./views/dashboard.js";
import { renderKeys } from "./views/keys.js";
import { renderOrgAdmin } from "./views/org-admin.js";
import { renderOrgs } from "./views/orgs.js";
import { renderPolicies } from "./views/policies.js";
import { renderSystemAdmin } from "./views/system-admin.js";
import { renderTargets } from "./views/targets.js";

const app = document.querySelector("#app");

boot();

async function boot() {
  applyDocumentLocale();
  applyDocumentTheme();
  setRoute(routeFromLocation());
  bindRouter(render);
  bindEvents();
  await refresh();
}

function bindEvents() {
  app.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = event.target;
    const action = form.dataset.action;
    const data = formData(form);
    await run(async () => {
      if (action === "set-target-filter") {
        state.targetQuery = data.query || "";
        render();
        return;
      }
      if (action === "set-policy-filter") {
        state.policyQuery = data.query || "";
        render();
        return;
      }
      if (action === "set-admin-user-filter") {
        state.adminUserQuery = data.query || "";
        render();
        return;
      }
      if (action === "register") await api.register(data);
      if (action === "login") await api.login(data);
      if (action === "register" || action === "login") {
        state.notice = t("status.signedIn");
        await refresh();
        return;
      }
      if (action === "create-org") {
        const out = await api.createOrg(data);
        state.activeOrgID = out.organization?.id || "";
        state.notice = t("status.saved");
        await refresh();
        return;
      }
      if (action === "join-org") {
        const out = await api.joinOrg(data.code);
        state.activeOrgID = out.organization?.id || "";
        state.notice = t("status.saved");
        await refresh();
        return;
      }
      if (action === "create-key") await api.createKey({ name: data.name, authorized_key: data.authorized_key });
      if (action === "create-group") await api.createGroup(activeOrg().id, data);
      if (action === "add-org-member") await api.addOrgMember(activeOrg().id, { ...data, role: data.role || "member" });
      if (action === "update-org-member") await api.updateOrgMember(activeOrg().id, form.dataset.userId, { role: data.role });
      if (action === "transfer-org-owner") await api.transferOrgOwner(activeOrg().id, data.user_id);
      if (action === "create-target") await createTarget(data);
      if (action === "rename-target") await renameTarget(form.dataset.targetId, data);
      if (action === "create-agent") {
        state.enrollment = await api.enrollAgent({ ...ownerPayload(data), default_port: Number(data.default_port || 22) });
        state.ui.modal = "";
        state.ui.drawer = "agent-enrollment";
      }
      if (action === "create-llm") await api.createLLMConfig({ ...ownerPayload(data), timeout_seconds: Number(data.timeout_seconds || 10) });
      if (action === "create-prompt") await api.createPrompt(ownerPayload(data));
      if (action === "create-policy") await api.createPolicy(ownerPayload(data));
      if (action === "add-rule") await api.addRule(data.policy_id, {
        rule_type: data.rule_type,
        pattern_type: data.pattern_type,
        pattern: data.pattern,
      });
      if (action === "bind-policy-target") await api.bindTarget(data.policy_id, data.target_id);
      if (action === "bind-policy-tag") await api.bindTargetTag(data.policy_id, { ...owner(), tag: data.tag });
      if (action === "bind-policy-group") await api.bindGroup(data.policy_id, data.group_id);
      if (action === "admin-save-dingtalk") await api.updateDingTalkSettings(adminDingTalkPayload(data));
      if (action === "admin-save-ldap") await api.updateLDAPSettings(adminLDAPPayload(data));
      if (action === "admin-update-user") await api.updateAdminUser(form.dataset.userId, { is_system_admin: data.is_system_admin === "true" });
      if (action === "admin-reset-password") await api.resetAdminUserPassword(form.dataset.userId, { password: data.password });
      if (action === "admin-select-org") {
        state.selectedAdminOrgID = data.org_id;
        state.ui.adminOrgID = data.org_id;
        state.ui.drawer = "admin-org";
        await refreshAdminMembers();
        render();
        return;
      }
      if (action === "admin-update-org-member") await api.adminUpdateOrgMember(state.selectedAdminOrgID, form.dataset.userId, { role: data.role });
      if (!["login", "register", "admin-select-org"].includes(action)) {
        form.reset();
      }
      if (form.dataset.closeOverlay === "modal") state.ui.modal = "";
      if (form.dataset.closeOverlay === "drawer") state.ui.drawer = "";
      state.notice = t("status.saved");
      await refreshData();
      render();
    });
  });

  app.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-click]");
    if (!button) return;
    if (button.classList.contains("overlay") && event.target !== button) return;
    const action = button.dataset.click;
    await run(async () => {
      if (action === "navigate") {
        state.ui.modal = "";
        state.ui.drawer = "";
        navigate(button.dataset.route);
      }
      if (action === "open-modal") {
        state.ui.modal = button.dataset.modal || "";
        state.ui.drawer = "";
      }
      if (action === "close-overlays") {
        state.ui.modal = "";
        state.ui.drawer = "";
      }
      if (action === "open-target-detail") {
        state.ui.targetID = button.dataset.targetId || "";
        state.ui.drawer = "target-detail";
        state.ui.modal = "";
      }
      if (action === "open-policy-detail") {
        state.ui.policyID = button.dataset.policyId || "";
        state.ui.drawer = "policy-detail";
        state.ui.modal = "";
      }
      if (action === "open-admin-org") {
        state.selectedAdminOrgID = button.dataset.orgId || "";
        state.ui.adminOrgID = state.selectedAdminOrgID;
        state.ui.drawer = "admin-org";
        state.ui.modal = "";
        await refreshAdminMembers();
      }
      if (action === "open-admin-password-reset") {
        state.ui.adminPasswordUserID = button.dataset.userId || "";
        state.ui.modal = "admin-reset-password";
        state.ui.drawer = "";
      }
      if (action === "set-agent-platform") {
        state.ui.agentPlatform = button.dataset.value || "linux";
      }
      if (action === "auth-mode") {
        state.authMode = button.dataset.mode === "register" ? "register" : "login";
        state.error = "";
        state.notice = "";
      }
      if (action === "set-locale") {
        state.locale = setLocale(button.dataset.locale);
        state.notice = "";
        state.error = "";
      }
      if (action === "set-theme") {
        state.theme = setTheme(button.dataset.theme);
        state.notice = "";
        state.error = "";
      }
      if (action === "logout") {
        await api.logout();
        Object.assign(state, { user: null, orgs: [], activeOrgID: "", members: [], selectedAdminOrgID: "", adminMembers: [], ui: { ...state.ui, modal: "", drawer: "" } });
      }
      if (action === "switch-org") {
        state.activeOrgID = button.dataset.id;
        state.ui.modal = "";
        state.ui.drawer = "";
        await refreshData();
      }
      if (action === "leave-org") {
        await api.leaveOrg(activeOrg().id);
        state.activeOrgID = "";
        await refresh();
        return;
      }
      if (action === "invite") {
        const out = await api.invite(activeOrg().id, "member");
        state.invite = out.code;
      }
      if (action === "delete-key") {
        if (!window.confirm(t("confirm.removeKey"))) return;
        await api.deleteKey(button.dataset.id);
        await refreshData();
      }
      if (action === "remove-org-member") {
        if (!window.confirm(t("confirm.removeMember"))) return;
        await api.removeOrgMember(activeOrg().id, button.dataset.userId);
        await refreshData();
      }
      if (action === "admin-transfer-org-owner") {
        if (!window.confirm(t("confirm.transferOwner"))) return;
        await api.adminTransferOrgOwner(state.selectedAdminOrgID, button.dataset.userId);
        await refreshAdminMembers();
      }
      if (action === "copy") {
        await copyText(button.dataset.value || "");
        state.notice = t("status.copied");
      }
      if (action === "toggle-target-tag") {
        const tag = button.dataset.tag || "";
        state.targetTagFilters = state.targetTagFilters.includes(tag)
          ? state.targetTagFilters.filter((item) => item !== tag)
          : [...state.targetTagFilters, tag];
      }
      if (action === "clear-target-filters") {
        state.targetQuery = "";
        state.targetTagFilters = [];
      }
      if (action === "clear-policy-filter") {
        state.policyQuery = "";
      }
      if (action === "clear-admin-user-filter") {
        state.adminUserQuery = "";
      }
      render();
    });
  });
}

async function refresh() {
  try {
    state.providers = await api.authProviders();
  } catch {
    state.providers = { dingtalk: { enabled: false } };
  }
  try {
    const me = await api.me();
    state.user = me.user;
    state.orgs = me.organizations || [];
    if (!state.activeOrgID && state.orgs.length) state.activeOrgID = state.orgs[0].id;
    await refreshData();
  } catch {
    state.user = null;
  }
  render();
}

async function refreshData() {
  if (!state.user || !activeOrg()) return;
  const currentOwner = owner();
  const requests = [
    api.keys(),
    api.groups(activeOrg().id),
    api.targets(currentOwner),
    api.policies(currentOwner),
    api.audit(),
    api.llmConfigs(currentOwner),
    api.prompts(currentOwner),
  ];
  if (canManageActiveOrg()) requests.push(api.orgMembers(activeOrg().id));
  const [keys, groups, targets, policies, audit, llms, prompts, members] = await Promise.all(requests);
  state.keys = keys.keys || [];
  state.groups = groups.groups || [];
  state.targets = (targets.targets || []).map((target) => ({
    ...target,
    display_name: `${target.name || target.alias} (${target.alias})`,
  }));
  state.policies = policies.policies || [];
  state.audit = audit.logs || [];
  state.llms = llms.configs || [];
  state.prompts = prompts.prompts || [];
  state.members = members?.members || [];
  if (state.user.is_system_admin) await refreshAdminData();
}

async function refreshAdminData() {
  const [settings, users, orgs] = await Promise.all([
    api.adminSettings(),
    api.adminUsers(),
    api.adminOrgs(),
  ]);
  state.adminSettings = settings || {};
  state.adminUsers = users.users || [];
  state.adminOrgs = orgs.organizations || [];
  if (!state.selectedAdminOrgID && state.adminOrgs.length) state.selectedAdminOrgID = state.adminOrgs[0].id;
  if (state.selectedAdminOrgID) await refreshAdminMembers();
}

async function refreshAdminMembers() {
  if (!state.selectedAdminOrgID) {
    state.adminMembers = [];
    return;
  }
  const members = await api.adminOrgMembers(state.selectedAdminOrgID);
  state.adminMembers = members.members || [];
}

async function run(fn) {
  state.error = "";
  state.notice = "";
  try {
    await fn();
  } catch (error) {
    state.error = error.message;
    render();
  }
}

function render() {
  if (!state.user) {
    app.innerHTML = renderAuth(state);
    return;
  }
  const shell = renderShell(renderRoute());
  app.innerHTML = shell.__raw || shell;
}

function renderRoute() {
  const views = {
    dashboard: renderDashboard,
    orgs: renderOrgs,
    "org-admin": renderOrgAdmin,
    keys: renderKeys,
    targets: renderTargets,
    agents: renderAgents,
    policies: renderPolicies,
    audit: renderAudit,
    "system-admin": renderSystemAdmin,
  };
  return (views[state.route] || renderDashboard)();
}

async function createTarget(data) {
  await api.createTarget({
    ...ownerPayload(data),
    port: Number(data.port || 22),
    tags: splitTags(data.tags),
  });
}

async function renameTarget(id, data) {
  await api.updateTarget(id, {
    name: data.name,
    alias: data.alias,
    tags: splitTags(data.tags),
  });
}

function adminDingTalkPayload(data) {
  return {
    enabled: data.enabled === "true",
    client_id: data.client_id || "",
    client_secret: data.client_secret || "",
    auth_url: data.auth_url || "",
    token_url: data.token_url || "",
    userinfo_url: data.userinfo_url || "",
    redirect_url: data.redirect_url || "",
    default_org_id: data.default_org_id || "",
    default_role: data.default_role || "member",
  };
}

function adminLDAPPayload(data) {
  return {
    enabled: data.enabled === "true",
    server_url: data.server_url || "",
    bind_dn: data.bind_dn || "",
    bind_password: data.bind_password || "",
    base_dn: data.base_dn || "",
    user_filter: data.user_filter || "",
    email_attr: data.email_attr || "",
    name_attr: data.name_attr || "",
  };
}

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return;
    } catch {
      // HTTP deployments can expose navigator.clipboard but still reject writes; fall back below.
    }
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  try {
    const ok = document.execCommand?.("copy");
    if (!ok) throw new Error(t("status.copyUnavailable"));
  } finally {
    textarea.remove();
  }
}
