import { state } from "../state.js";
import { dateLocale, t } from "../i18n.js";
import { badge, emptyState, escapeHTML, panel, streamList, table } from "../components/html.js";

export function renderAudit() {
  return panel(t("audit.title"), t("audit.sub"), `
    <div class="audit-telemetry">${streamList().__raw}</div>
    ${state.audit.length ? table([t("audit.command"), t("audit.decision"), t("audit.reason"), t("audit.exit"), t("audit.started")], state.audit.map((log) => [
      `<code>${escapeHTML(log.command)}</code>`,
      log.policy_decision === "allow" ? badge(t("common.allow"), "success").__raw : badge(t("common.deny"), "danger").__raw,
      escapeHTML(log.policy_reason),
      log.exit_code ?? "",
      new Date(log.started_at).toLocaleString(dateLocale()),
    ])) : emptyState(t("audit.emptyTitle"), t("audit.emptyBody")).__raw}
  `);
}
