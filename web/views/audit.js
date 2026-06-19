import { state } from "../state.js";
import { dateLocale, t } from "../i18n.js";
import { badge, emptyState, escapeHTML, panel, streamList, table } from "../components/html.js";

export function renderAudit() {
  return panel(t("audit.title"), t("audit.sub"), `
    <div class="audit-telemetry">${streamList().__raw}</div>
    ${state.audit.length ? table([t("audit.user"), t("audit.publicKey"), t("audit.target"), t("audit.command"), t("audit.decision"), t("audit.reason"), t("audit.exit"), t("audit.started")], state.audit.map((log) => [
      auditUser(log),
      auditPublicKey(log),
      auditTarget(log),
      `<code>${escapeHTML(log.command)}</code>`,
      log.policy_decision === "allow" ? badge(t("common.allow"), "success").__raw : badge(t("common.deny"), "danger").__raw,
      escapeHTML(log.policy_reason),
      log.exit_code ?? "",
      new Date(log.started_at).toLocaleString(dateLocale()),
    ])) : emptyState(t("audit.emptyTitle"), t("audit.emptyBody")).__raw}
  `);
}

function auditUser(log) {
  const name = log.user_display_name || log.user_email || log.user_id || "-";
  const email = log.user_email && log.user_email !== name ? `<small>${escapeHTML(log.user_email)}</small>` : "";
  return `<strong>${escapeHTML(name)}</strong>${email}`;
}

function auditPublicKey(log) {
  const name = log.public_key_name || "";
  const fingerprint = log.public_key_fingerprint || "-";
  return `${name ? `<strong>${escapeHTML(name)}</strong>` : ""}<code>${escapeHTML(fingerprint)}</code>`;
}

function auditTarget(log) {
  const name = log.target_name || log.target_alias || log.target_id || "-";
  const endpoint = log.target_endpoint || "";
  return `<strong>${escapeHTML(name)}</strong>${endpoint ? `<small>${escapeHTML(endpoint)}</small>` : ""}`;
}
