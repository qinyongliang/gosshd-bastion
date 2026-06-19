import { allTargetTags, filteredTargets, state } from "../state.js";
import { emptyState, escapeHTML, icon, panel, raw, table } from "../components/html.js";
import { optionText, t } from "../i18n.js";

export function renderTargets() {
  const tags = allTargetTags();
  const targets = filteredTargets();
  return raw(`
    ${panel(t("targets.addTitle"), t("targets.addSub"), `
      <form data-action="create-target" class="stack">
        <div class="form-grid">
          <input name="name" aria-label="${escapeHTML(t("targets.serviceName"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.serviceNamePlaceholder"))}" required />
          <input name="alias" aria-label="${escapeHTML(t("targets.alias"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.aliasPlaceholder"))}" required />
          <input name="tags" aria-label="${escapeHTML(t("targets.tags"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.tagsPlaceholder"))}" />
          <select name="target_type" aria-label="${escapeHTML(t("targets.type"))}"><option value="direct">${escapeHTML(t("targetTypes.direct"))}</option><option value="agent">${escapeHTML(t("targetTypes.agent"))}</option></select>
          <input name="host" aria-label="${escapeHTML(t("targets.host"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.hostPlaceholder"))}" required />
          <input name="port" aria-label="${escapeHTML(t("targets.port"))}" type="number" value="22" required />
          <input name="remote_username" aria-label="${escapeHTML(t("targets.remoteUsername"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.remoteUsernamePlaceholder"))}" required />
          <select name="auth_type" aria-label="${escapeHTML(t("targets.authType"))}"><option value="password">${escapeHTML(t("authTypes.password"))}</option><option value="private_key">${escapeHTML(t("authTypes.private_key"))}</option></select>
          <input name="secret" aria-label="${escapeHTML(t("targets.secret"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.secretPlaceholder"))}" />
          <input name="agent_id" aria-label="${escapeHTML(t("targets.agentID"))}" autocomplete="off" placeholder="${escapeHTML(t("targets.agentIDPlaceholder"))}" />
        </div>
        <button type="submit">${icon("server").__raw}${escapeHTML(t("targets.add"))}</button>
      </form>
    `).__raw}
    ${tags.length ? `<div class="filter-chips">${tags.map((tag) => `
      <button type="button" data-click="toggle-target-tag" data-tag="${escapeHTML(tag)}" class="${state.targetTagFilters.includes(tag) ? "active" : ""}">${escapeHTML(tag)}</button>
    `).join("")}</div>` : ""}
    ${panel(t("targets.listTitle"), t("targets.listSub"), targets.length ? table([t("targets.tableService"), t("targets.tableAlias"), t("targets.tableEndpoint"), t("targets.tableAuth"), t("targets.tableTags"), t("targets.tableRename")], targets.map((target) => [
      `<strong>${escapeHTML(target.name || target.alias)}</strong><small>${escapeHTML(optionText("targetTypes", target.target_type))}</small>`,
      escapeHTML(target.alias),
      escapeHTML(`${target.remote_username}@${target.host}:${target.port}`),
      escapeHTML(optionText("authTypes", target.auth_type)),
      targetTags(target),
      `<form data-action="rename-target" data-target-id="${escapeHTML(target.id)}" class="row-form"><input name="name" aria-label="${escapeHTML(t("targets.name"))}" value="${escapeHTML(target.name || "")}" placeholder="${escapeHTML(t("targets.namePlaceholder"))}" /><input name="alias" aria-label="${escapeHTML(t("targets.aliasShort"))}" value="${escapeHTML(target.alias)}" placeholder="${escapeHTML(t("targets.aliasShortPlaceholder"))}" /><input name="tags" aria-label="${escapeHTML(t("targets.tagsShort"))}" value="${escapeHTML((target.tags || []).join(", "))}" placeholder="${escapeHTML(t("targets.tagsShortPlaceholder"))}" /><button type="submit">${escapeHTML(t("common.save"))}</button></form>`,
    ])) : emptyState(t("targets.emptyTitle"), t("targets.emptyBody")).__raw).__raw}
  `);
}

function targetTags(target) {
  const tags = target.tags || [];
  if (!tags.length) return "";
  return `<div class="tag-row">${tags.map((tag) => `<span>${escapeHTML(tag)}</span>`).join("")}</div>`;
}
