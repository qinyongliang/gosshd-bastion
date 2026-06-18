import { activeOrg, state } from "../state.js";
import { badge, emptyState, escapeHTML, metric, panel, raw, table } from "../components/html.js";

export function renderDashboard() {
  const org = activeOrg();
  const latestAudit = state.audit.slice(0, 5);
  return raw(`
    <div class="metrics">
      ${metric("SSH services", state.targets.length, "server").__raw}
      ${metric("Policies", state.policies.length, "policies").__raw}
      ${metric("User groups", state.groups.length, "org").__raw}
      ${metric("Audit rows", state.audit.length, "audit").__raw}
    </div>
    <div class="grid two">
      ${panel("Operating context", "Current organization and access role.", `
        <div class="summary-list">
          <span><strong>${escapeHTML(org?.name || "No organization")}</strong><small>${escapeHTML(org?.role || "member")}</small></span>
          <span><strong>${state.user.is_system_admin ? "System admin" : "User"}</strong><small>${escapeHTML(state.user.auth_provider || "local")}</small></span>
          <span><strong>${state.targets.length ? "Ready" : "Needs target"}</strong><small>ssh alias routing</small></span>
        </div>
      `).__raw}
      ${panel("Fast path", "Use the focused modules from the left navigation.", `
        <div class="action-grid">
          <button data-click="navigate" data-route="targets">Manage SSH services</button>
          <button data-click="navigate" data-route="agents">Create agent enrollment</button>
          <button data-click="navigate" data-route="policies">Bind command security groups</button>
          <button data-click="navigate" data-route="audit">Review command audit</button>
        </div>
      `).__raw}
    </div>
    ${panel("Recent command decisions", "SSH commands are recorded with the policy decision.", latestAudit.length ? table(["Command", "Decision", "Reason"], latestAudit.map((log) => [
      `<code>${escapeHTML(log.command)}</code>`,
      log.policy_decision === "allow" ? badge("allow", "success").__raw : badge("deny", "danger").__raw,
      escapeHTML(log.policy_reason),
    ])) : emptyState("No audit rows", "Run an SSH command through an alias to populate this table.").__raw).__raw}
  `);
}
