import { allTargetTags, filteredTargets, state } from "../state.js";
import { emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { cloudTable, detailList, drawer, modal, resourceHeader, resourceToolbar, rowButton, sectionBlock, selectionSummary, stepper } from "../components/management.js";
import { optionText, t } from "../i18n.js";

export function renderTargets() {
  const tags = allTargetTags();
  const targets = filteredTargets();
  return raw(`
    ${resourceHeader({
      title: t("targets.listTitle"),
      subtitle: t("targets.listSub"),
      actions: `<button type="button" class="primary" data-click="open-modal" data-modal="create-target">${icon("plus").__raw}${escapeHTML(t("targets.add"))}</button>`,
      stats: [
        { label: t("targets.total"), value: state.targets.length },
        { label: t("targets.directCount"), value: state.targets.filter((target) => target.target_type === "direct").length, tone: "success" },
        { label: t("targets.agentCount"), value: state.targets.filter((target) => target.target_type === "agent").length, tone: "info" },
        { label: t("targets.tagCount"), value: tags.length, tone: "warning" },
      ],
    }).__raw}
    ${resourceToolbar({
      searchAction: "set-target-filter",
      query: state.targetQuery,
      searchPlaceholder: t("targets.searchPlaceholder"),
      chips: tags.map((tag) => `
        <button type="button" data-click="toggle-target-tag" data-tag="${escapeHTML(tag)}" class="${state.targetTagFilters.includes(tag) ? "active" : ""}">${escapeHTML(tag)}</button>
      `).join(""),
      actions: `
        ${selectionSummary(0)}
        <button type="button" data-click="clear-target-filters">${escapeHTML(t("targets.clearFilters"))}</button>
      `,
    }).__raw}
    ${targetTable(targets).__raw}
    ${createTargetModal().__raw || ""}
    ${targetDrawer().__raw || ""}
  `);
}

function targetTable(targets) {
  return cloudTable([
    "",
    t("targets.tableService"),
    t("targets.tableAlias"),
    t("targets.tableEndpoint"),
    t("targets.tableAuth"),
    t("targets.tableTags"),
    t("management.operations"),
  ], targets.map((target) => [
    `<input type="checkbox" aria-label="${escapeHTML(t("targets.selectedRows"))}" />`,
    `<strong>${escapeHTML(target.name || target.alias)}</strong><small>${escapeHTML(optionText("targetTypes", target.target_type))}</small>`,
    `<code>${escapeHTML(target.alias)}</code>`,
    `<span>${escapeHTML(`${target.remote_username}@${target.host}:${target.port}`)}</span>`,
    `<span>${escapeHTML(optionText("authTypes", target.auth_type))}</span>`,
    targetTags(target),
    `<div class="row-actions">${rowButton(t("targets.openDetails"), "open-target-detail", { "target-id": target.id })}</div>`,
  ]), {
    empty: emptyState(t("targets.emptyTitle"), t("targets.emptyBody")).__raw,
  });
}

function createTargetModal() {
  return modal(state, "create-target", {
    title: t("targets.createModalTitle"),
    subtitle: t("targets.createModalSub"),
    size: "wide",
    body: `
      ${stepper([t("targets.basicInfo"), t("targets.connection"), t("targets.authentication"), t("management.relationships")]).__raw}
      <form data-action="create-target" data-close-overlay="modal" class="modal-form">
        <div class="form-section">
          <h3>${escapeHTML(t("targets.basicInfo"))}</h3>
          <div class="form-grid">
            <label class="field"><span>${escapeHTML(t("targets.serviceName"))}</span><input name="name" autocomplete="off" placeholder="${escapeHTML(t("targets.serviceNamePlaceholder"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.alias"))}</span><input name="alias" autocomplete="off" placeholder="${escapeHTML(t("targets.aliasPlaceholder"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.type"))}</span><select name="target_type"><option value="direct">${escapeHTML(t("targetTypes.direct"))}</option><option value="agent">${escapeHTML(t("targetTypes.agent"))}</option></select></label>
          </div>
        </div>
        <div class="form-section">
          <h3>${escapeHTML(t("targets.connection"))}</h3>
          <div class="form-grid">
            <label class="field"><span>${escapeHTML(t("targets.host"))}</span><input name="host" autocomplete="off" placeholder="${escapeHTML(t("targets.hostPlaceholder"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.port"))}</span><input name="port" type="number" value="22" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.remoteUsername"))}</span><input name="remote_username" autocomplete="off" placeholder="${escapeHTML(t("targets.remoteUsernamePlaceholder"))}" required /></label>
          </div>
        </div>
        <div class="form-section">
          <h3>${escapeHTML(t("targets.authentication"))}</h3>
          <div class="form-grid">
            <label class="field"><span>${escapeHTML(t("targets.authType"))}</span><select name="auth_type"><option value="password">${escapeHTML(t("authTypes.password"))}</option><option value="private_key">${escapeHTML(t("authTypes.private_key"))}</option></select></label>
            <label class="field span-two"><span>${escapeHTML(t("targets.secret"))}</span><input name="secret" autocomplete="off" placeholder="${escapeHTML(t("targets.secretPlaceholder"))}" /></label>
          </div>
        </div>
        <div class="form-section">
          <h3>${escapeHTML(t("management.relationships"))}</h3>
          <div class="form-grid">
            <label class="field span-two"><span>${escapeHTML(t("targets.tags"))}</span><input name="tags" autocomplete="off" placeholder="${escapeHTML(t("targets.tagsPlaceholder"))}" /></label>
            <label class="field"><span>${escapeHTML(t("targets.agentID"))}</span><input name="agent_id" autocomplete="off" placeholder="${escapeHTML(t("targets.agentIDPlaceholder"))}" /></label>
          </div>
        </div>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${icon("server").__raw}${escapeHTML(t("targets.add"))}</button>
        </footer>
      </form>
    `,
  });
}

function targetDrawer() {
  const target = state.targets.find((item) => item.id === state.ui.targetID);
  if (!target) return "";
  return drawer(state, "target-detail", {
    title: target.name || target.alias,
    subtitle: t("targets.detailSub"),
    meta: targetTags(target),
    body: `
      ${sectionBlock(t("targets.routePreview"), `${target.alias}@public-ip`, `
        <div class="route-preview">
          <span>${escapeHTML(t("dashboard.streamIdentity"))}</span>
          <b>${escapeHTML(target.alias)}</b>
          <span>${escapeHTML(t("dashboard.streamRoute"))}</span>
          <b>${escapeHTML(`${target.remote_username}@${target.host}:${target.port}`)}</b>
        </div>
      `).__raw}
      ${sectionBlock(t("targets.basicInfo"), t("targets.detailTitle"), detailList([
        [t("targets.tableAlias"), `<code>${escapeHTML(target.alias)}</code>`],
        [t("targets.type"), escapeHTML(optionText("targetTypes", target.target_type))],
        [t("targets.tableAuth"), escapeHTML(optionText("authTypes", target.auth_type))],
        [t("targets.agentID"), escapeHTML(target.agent_id || "-")],
      ]).__raw).__raw}
      ${sectionBlock(t("targets.editTitle"), t("targets.detailSub"), `
        <form data-action="rename-target" data-target-id="${escapeHTML(target.id)}" class="stack">
          <label class="field"><span>${escapeHTML(t("targets.name"))}</span><input name="name" value="${escapeHTML(target.name || "")}" placeholder="${escapeHTML(t("targets.namePlaceholder"))}" /></label>
          <label class="field"><span>${escapeHTML(t("targets.aliasShort"))}</span><input name="alias" value="${escapeHTML(target.alias)}" placeholder="${escapeHTML(t("targets.aliasShortPlaceholder"))}" /></label>
          <label class="field"><span>${escapeHTML(t("targets.tagsShort"))}</span><input name="tags" value="${escapeHTML((target.tags || []).join(", "))}" placeholder="${escapeHTML(t("targets.tagsShortPlaceholder"))}" /></label>
          <button type="submit" class="primary">${escapeHTML(t("common.save"))}</button>
        </form>
      `).__raw}
    `,
  });
}

function targetTags(target) {
  const tags = target.tags || [];
  if (!tags.length) return `<span class="muted">-</span>`;
  return `<div class="tag-row">${tags.map((tag) => `<span>${escapeHTML(tag)}</span>`).join("")}</div>`;
}
