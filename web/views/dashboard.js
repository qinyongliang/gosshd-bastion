import { activeOrg, state } from "../state.js";
import { optionText, t } from "../i18n.js";
import { accessSummaryGrid, badge, emptyState, escapeHTML, metric, panel, raw, table } from "../components/html.js";

export function renderDashboard() {
  const org = activeOrg();
  const latestAudit = state.audit.slice(0, 5);
  return raw(`
    <div class="signal-panel">
      ${panel(t("dashboard.topologyTitle"), t("dashboard.topologySub"), accessSummaryGrid().__raw).__raw}
      ${panel(t("dashboard.streamTitle"), t("dashboard.streamSub"), `
        <div class="summary-list">
          <span><strong>${escapeHTML(state.keys.length)}</strong><small>${escapeHTML(t("dashboard.flowPublicKey"))}</small></span>
          <span><strong>${escapeHTML(state.targets.length)}</strong><small>${escapeHTML(t("dashboard.flowSSHService"))}</small></span>
          <span><strong>${escapeHTML(state.policies.length)}</strong><small>${escapeHTML(t("dashboard.policies"))}</small></span>
          <span><strong>${escapeHTML(state.audit.length)}</strong><small>${escapeHTML(t("dashboard.auditRows"))}</small></span>
        </div>
      `).__raw}
    </div>
    <div class="metrics">
      ${metric(t("dashboard.sshServices"), state.targets.length, "server").__raw}
      ${metric(t("dashboard.policies"), state.policies.length, "policies").__raw}
      ${metric(t("dashboard.userGroups"), state.groups.length, "org").__raw}
      ${metric(t("dashboard.auditRows"), state.audit.length, "audit").__raw}
    </div>
    <div class="grid two">
      ${panel(t("dashboard.contextTitle"), t("dashboard.contextSub"), `
        <div class="summary-list">
          <span><strong>${escapeHTML(org?.name || t("dashboard.noOrganization"))}</strong><small>${escapeHTML(optionText("roles", org?.role || "member"))}</small></span>
          <span><strong>${state.user.is_system_admin ? t("dashboard.systemAdmin") : t("dashboard.user")}</strong><small>${escapeHTML(optionText("providers", state.user.auth_provider || "local"))}</small></span>
          <span><strong>${state.targets.length ? t("dashboard.ready") : t("dashboard.needsTarget")}</strong><small>${escapeHTML(t("dashboard.aliasRouting"))}</small></span>
        </div>
      `).__raw}
      ${panel(t("dashboard.fastTitle"), t("dashboard.fastSub"), `
        <div class="action-grid">
          <button data-click="navigate" data-route="targets">${escapeHTML(t("dashboard.manageTargets"))}</button>
          <button data-click="open-private-node-create">${escapeHTML(t("dashboard.createAgent"))}</button>
          <button data-click="navigate" data-route="policies">${escapeHTML(t("dashboard.bindPolicies"))}</button>
          <button data-click="navigate" data-route="audit">${escapeHTML(t("dashboard.reviewAudit"))}</button>
        </div>
      `).__raw}
    </div>
    ${panel(t("dashboard.decisionsTitle"), t("dashboard.decisionsSub"), latestAudit.length ? table([t("dashboard.command"), t("dashboard.decision"), t("dashboard.reason")], latestAudit.map((log) => [
      `<code>${escapeHTML(log.command)}</code>`,
      log.policy_decision === "allow" ? badge(t("common.allow"), "success").__raw : badge(t("common.deny"), "danger").__raw,
      escapeHTML(log.policy_reason),
    ])) : emptyState(t("dashboard.emptyTitle"), t("dashboard.emptyBody")).__raw).__raw}
  `);
}
