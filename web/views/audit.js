import { state } from "../state.js";
import { badge, emptyState, escapeHTML, panel, table } from "../components/html.js";

export function renderAudit() {
  return panel("Command audit", "Every SSH exec decision is recorded.", `
    ${state.audit.length ? table(["Command", "Decision", "Reason", "Exit", "Started"], state.audit.map((log) => [
      `<code>${escapeHTML(log.command)}</code>`,
      log.policy_decision === "allow" ? badge("allow", "success").__raw : badge("deny", "danger").__raw,
      escapeHTML(log.policy_reason),
      log.exit_code ?? "",
      new Date(log.started_at).toLocaleString(),
    ])) : emptyState("No audit rows", "Run an SSH command through an alias to populate this table.").__raw}
  `);
}
