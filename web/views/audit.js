import { state } from "../state.js";
import { dateLocale, t } from "../i18n.js";
import { badge, emptyState, escapeHTML, raw } from "../components/html.js";
import { cloudTable, drawer, resourceHeader, resourceToolbar, rowButton } from "../components/management.js";

export function renderAudit() {
  return raw(`
    ${resourceHeader({
      title: t("audit.title"),
      subtitle: t("audit.sub"),
      stats: [
        { label: t("audit.total"), value: state.auditTotal || 0 },
        { label: t("audit.page"), value: state.auditPage || 1, tone: "info" },
        { label: t("audit.recordings"), value: state.audit.filter((log) => log.has_recording).length, tone: "success" },
      ],
    }).__raw}
    ${resourceToolbar({
      searchAction: "set-audit-filter",
      query: state.auditQuery,
      searchPlaceholder: t("audit.searchPlaceholder"),
      chips: auditTimeFilters(),
      formID: "audit-filter-form",
      actions: `<button type="button" data-click="clear-audit-filter">${escapeHTML(t("targets.clearFilters"))}</button>`,
    }).__raw}
    ${auditTable().__raw}
    ${auditPagination()}
    ${auditReplayDrawer().__raw || ""}
  `);
}

function auditTimeFilters() {
  return `
    <label class="compact-date"><span>${escapeHTML(t("audit.from"))}</span><input type="datetime-local" form="audit-filter-form" name="started_from" value="${escapeHTML(state.auditStartedFrom)}" /></label>
    <label class="compact-date"><span>${escapeHTML(t("audit.to"))}</span><input type="datetime-local" form="audit-filter-form" name="started_to" value="${escapeHTML(state.auditStartedTo)}" /></label>
  `;
}

function auditTable() {
  return cloudTable([
    t("audit.user"),
    t("audit.publicKey"),
    t("audit.target"),
    t("audit.command"),
    t("audit.type"),
    t("audit.decision"),
    t("audit.reason"),
    t("audit.exit"),
    t("audit.started"),
    t("management.operations"),
  ], state.audit.map((log) => [
    auditUser(log),
    auditPublicKey(log),
    auditTarget(log),
    `<code>${escapeHTML(log.command || "-")}</code>`,
    escapeHTML(log.request_type || "-"),
    log.policy_decision === "allow" ? badge(t("common.allow"), "success").__raw : badge(t("common.deny"), "danger").__raw,
    escapeHTML(log.policy_reason || "-"),
    log.exit_code ?? "",
    new Date(log.started_at).toLocaleString(dateLocale()),
    log.has_recording ? rowButton(t("audit.replay"), "open-audit-replay", { "audit-id": log.id }) : `<span class="muted">-</span>`,
  ]), {
    empty: emptyState(t("audit.emptyTitle"), t("audit.emptyBody")).__raw,
  });
}

function auditPagination() {
  const page = state.auditPage || 1;
  const pageSize = state.auditPageSize || 20;
  const total = state.auditTotal || 0;
  const pages = Math.max(1, Math.ceil(total / pageSize));
  return `
    <div class="pagination-bar">
      <span>${escapeHTML(t("audit.page"))} ${page} / ${pages}</span>
      <div>
        <button type="button" data-click="audit-page" data-page="${page - 1}" ${page <= 1 ? "disabled" : ""}>${escapeHTML(t("audit.prev"))}</button>
        <button type="button" data-click="audit-page" data-page="${page + 1}" ${page >= pages ? "disabled" : ""}>${escapeHTML(t("audit.next"))}</button>
      </div>
    </div>
  `;
}

function auditReplayDrawer() {
  if (state.ui.drawer !== "audit-replay" || !state.auditReplay) return "";
  const log = state.auditReplay.log || {};
  const lines = state.auditReplay.lines || [];
  return drawer(state, "audit-replay", {
    title: t("audit.replayTitle"),
    subtitle: auditReplaySubtitle(log),
    body: `
      <section class="terminal-player" data-terminal-replay data-lines="${escapeHTML(JSON.stringify(lines))}">
        <div class="terminal-controls">
          <button type="button" data-terminal-play>${escapeHTML(t("audit.play"))}</button>
          <label><span>${escapeHTML(t("audit.speed"))}</span><select data-terminal-speed><option value="0.5">0.5x</option><option value="1" selected>1x</option><option value="2">2x</option><option value="4">4x</option></select></label>
          <input type="range" min="0" max="100" value="0" data-terminal-progress />
        </div>
        <div class="terminal-output" data-terminal-output></div>
      </section>
    `,
  });
}

function auditReplaySubtitle(log) {
  const target = log.target_alias || log.target_name || "-";
  const started = log.started_at ? new Date(log.started_at).toLocaleString(dateLocale()) : "";
  return `${target}${started ? " · " + started : ""}`;
}

function auditUser(log) {
  const name = log.user_display_name || log.user_email || "-";
  const email = log.user_email && log.user_email !== name ? `<small>${escapeHTML(log.user_email)}</small>` : "";
  return `<strong>${escapeHTML(name)}</strong>${email}`;
}

function auditPublicKey(log) {
  const name = log.public_key_name || "";
  const fingerprint = log.public_key_fingerprint || "-";
  return `${name ? `<strong>${escapeHTML(name)}</strong>` : ""}<code>${escapeHTML(fingerprint)}</code>`;
}

function auditTarget(log) {
  const name = log.target_name || log.target_alias || "-";
  const endpoint = log.target_endpoint || "";
  return `<strong>${escapeHTML(name)}</strong>${endpoint ? `<small>${escapeHTML(endpoint)}</small>` : ""}`;
}
