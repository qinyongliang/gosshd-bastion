import { allTargetTagDetails, filteredTargets, state, tagColorForName } from "../state.js";
import { emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { cloudTable, detailList, drawer, modal, resourceHeader, resourceToolbar, rowButton, sectionBlock, selectionSummary, stepper, tabs } from "../components/management.js";
import { tagInput } from "../components/tag-input.js";
import { optionText, t } from "../i18n.js";
import { TAG_COLORS, tagChip, tagColorClass, tagFilterButton } from "../tag-colors.js";
import { enrollmentDrawer } from "./agents.js";

export function renderTargets() {
  const tags = allTargetTagDetails();
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
      chips: tags.map((tag) => tagFilterButton(tag.name, tag.color, state.targetTagFilters.includes(tag.name))).join(""),
      actions: `
        ${selectionSummary(0)}
        <button type="button" data-click="clear-target-filters">${escapeHTML(t("targets.clearFilters"))}</button>
      `,
    }).__raw}
    ${targetTable(targets).__raw}
    ${createTargetModal().__raw || ""}
    ${enrollmentDrawer().__raw || ""}
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
    `<div class="row-actions">
      ${copySSHCommandButton(target)}
      ${rowButton(t("targets.openDetails"), "open-target-detail", { "target-id": target.id })}
    </div>`,
  ]), {
    empty: emptyState(t("targets.emptyTitle"), t("targets.emptyBody")).__raw,
  });
}

function copySSHCommandButton(target) {
  const command = sshCommand(target);
  return `<button type="button" class="small" data-click="copy" data-value="${escapeHTML(command)}" aria-label="${escapeHTML(t("targets.copySSHCommand"))}">${icon("copy").__raw}${escapeHTML(t("targets.copySSHCommand"))}</button>`;
}

function sshCommand(target) {
  const host = state.runtime?.ssh_host || window.location.hostname || "public-ip";
  const port = Number(state.runtime?.ssh_port || 22);
  const portArg = port && port !== 22 ? ` -p ${port}` : "";
  return `ssh${portArg} ${target.alias}@${host}`;
}

function createTargetModal() {
  const mode = state.ui.targetCreateMode || "direct";
  return modal(state, "create-target", {
    title: t("targets.createModalTitle"),
    subtitle: t("targets.createModalSub"),
    size: "wide",
    body: `
      ${tabs([
        { label: t("targets.tabServer"), action: "set-target-create-mode", value: "direct", active: mode === "direct" },
        { label: t("targets.tabPrivate"), action: "set-target-create-mode", value: "private", active: mode === "private" },
      ]).__raw}
      ${mode === "private" ? privateNodeForm() : sshServerWizard()}
    `,
  });
}

function sshServerWizard() {
  const step = Math.max(0, Math.min(Number(state.ui.targetCreateStep || 0), 3));
  const draft = targetDraft();
  const visibleFields = [
    ["name", "alias", "tags"],
    ["host", "port", "remote_username"],
    ["auth_type", "secret", "proxy_target_id"],
    [],
  ][step];
  return `
    ${stepper([t("targets.stepIdentity"), t("targets.stepEndpoint"), t("targets.stepSecurity"), t("targets.stepReview")], step).__raw}
    <form data-action="create-target" data-close-overlay="modal" class="modal-form target-wizard">
      ${hiddenDraftInputs(draft, visibleFields)}
      ${step === 0 ? identityStep(draft) : ""}
      ${step === 1 ? endpointStep(draft) : ""}
      ${step === 2 ? securityStep(draft) : ""}
      ${step === 3 ? reviewStep(draft) : ""}
      <footer class="modal-actions">
        <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
        ${step > 0 ? `<button type="button" data-click="target-create-step" data-step="${step - 1}">${escapeHTML(t("targets.back"))}</button>` : ""}
        ${step < 3 ? `<button type="button" class="primary" data-click="target-create-step" data-step="${step + 1}">${escapeHTML(t("targets.next"))}</button>` : `<button type="submit" class="primary">${icon("server").__raw}${escapeHTML(t("targets.add"))}</button>`}
      </footer>
    </form>
  `;
}

function privateNodeForm() {
  return `
    <form data-action="create-agent" class="modal-form private-node-form">
      <section class="wizard-card">
        <h3>${escapeHTML(t("targets.privateTitle"))}</h3>
        <p>${escapeHTML(t("targets.privateSub"))}</p>
        <div class="form-grid single">
          <label class="field"><span>${escapeHTML(t("targets.privateAlias"))}</span><input name="label" autocomplete="off" placeholder="${escapeHTML(t("targets.aliasPlaceholder"))}" required /></label>
        </div>
        <div class="guide-grid private-node-guide">
          <span><b>Linux</b>${escapeHTML(t("agents.linuxService"))}</span>
          <span><b>Windows</b>${escapeHTML(t("agents.windowsService"))}</span>
        </div>
      </section>
      <footer class="modal-actions">
        <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
        <button type="submit" class="primary">${icon("spark").__raw}${escapeHTML(t("targets.createPrivate"))}</button>
      </footer>
    </form>
  `;
}

function identityStep(draft) {
  return `
    <section class="wizard-card">
      <h3>${escapeHTML(t("targets.stepIdentity"))}</h3>
      <p>${escapeHTML(t("targets.stepIdentitySub"))}</p>
      <div class="form-grid">
        <label class="field"><span>${escapeHTML(t("targets.serviceName"))}</span><input name="name" value="${escapeHTML(draft.name)}" autocomplete="off" placeholder="${escapeHTML(t("targets.serviceNamePlaceholder"))}" required /></label>
        <label class="field"><span>${escapeHTML(t("targets.alias"))}</span><input name="alias" value="${escapeHTML(draft.alias)}" autocomplete="off" placeholder="${escapeHTML(t("targets.aliasPlaceholder"))}" required /></label>
        ${tagInput({ label: t("targets.tags"), value: draft.tags, placeholder: t("targets.tagsPlaceholder"), className: "span-two" }).__raw}
      </div>
    </section>
  `;
}

function endpointStep(draft) {
  return `
    <section class="wizard-card">
      <h3>${escapeHTML(t("targets.stepEndpoint"))}</h3>
      <p>${escapeHTML(t("targets.stepEndpointSub"))}</p>
      <div class="form-grid">
        <label class="field span-two"><span>${escapeHTML(t("targets.host"))}</span><input name="host" value="${escapeHTML(draft.host)}" autocomplete="off" placeholder="${escapeHTML(t("targets.hostPlaceholder"))}" required /></label>
        <label class="field"><span>${escapeHTML(t("targets.port"))}</span><input name="port" type="number" value="${escapeHTML(draft.port)}" required /></label>
        <label class="field"><span>${escapeHTML(t("targets.remoteUsername"))}</span><input name="remote_username" value="${escapeHTML(draft.remote_username)}" autocomplete="off" placeholder="${escapeHTML(t("targets.remoteUsernamePlaceholder"))}" required /></label>
      </div>
    </section>
  `;
}

function securityStep(draft) {
  return `
    <section class="wizard-card">
      <h3>${escapeHTML(t("targets.stepSecurity"))}</h3>
      <p>${escapeHTML(t("targets.stepSecuritySub"))}</p>
      <div class="form-grid">
        <label class="field"><span>${escapeHTML(t("targets.authType"))}</span><select name="auth_type">${authOptions(draft.auth_type)}</select></label>
        <label class="field span-two"><span>${escapeHTML(t("targets.secret"))}</span><input name="secret" value="${escapeHTML(draft.secret)}" autocomplete="off" placeholder="${escapeHTML(t("targets.secretPlaceholder"))}" /></label>
      </div>
      <details class="advanced-panel">
        <summary>${escapeHTML(t("targets.advancedProxy"))}</summary>
        <p>${escapeHTML(t("targets.advancedProxySub"))}</p>
        <label class="field"><span>${escapeHTML(t("targets.proxyTarget"))}</span><select name="proxy_target_id">${proxyOptions(draft.proxy_target_id)}</select></label>
      </details>
    </section>
  `;
}

function reviewStep(draft) {
  return `
    <section class="wizard-card">
      <h3>${escapeHTML(t("targets.stepReview"))}</h3>
      <p>${escapeHTML(t("targets.stepReviewSub"))}</p>
      <div class="review-grid">
        <span><b>${escapeHTML(t("targets.serviceName"))}</b>${escapeHTML(draft.name || "-")}</span>
        <span><b>${escapeHTML(t("targets.alias"))}</b>${escapeHTML(draft.alias || "-")}</span>
        <span><b>${escapeHTML(t("targets.tableEndpoint"))}</b>${escapeHTML(`${draft.remote_username || "-"}@${draft.host || "-"}:${draft.port || "22"}`)}</span>
        <span><b>${escapeHTML(t("targets.proxyTarget"))}</b>${escapeHTML(proxyLabel(draft.proxy_target_id))}</span>
      </div>
    </section>
  `;
}

function targetDraft() {
  return {
    target_type: "direct",
    agent_id: "",
    name: "",
    alias: "",
    tags: "",
    host: "",
    port: "22",
    remote_username: "",
    auth_type: "password",
    secret: "",
    proxy_target_id: "",
    ...(state.ui.targetCreateDraft || {}),
  };
}

function hiddenDraftInputs(draft, visibleFields) {
  const visible = new Set(visibleFields);
  const fields = ["target_type", "agent_id", "name", "alias", "tags", "host", "port", "remote_username", "auth_type", "secret", "proxy_target_id"];
  return fields
    .filter((name) => !visible.has(name))
    .map((name) => `<input type="hidden" name="${escapeHTML(name)}" value="${escapeHTML(draft[name] || "")}" />`)
    .join("");
}

function authOptions(selected) {
  return ["password", "private_key"].map((value) => `<option value="${escapeHTML(value)}" ${selected === value ? "selected" : ""}>${escapeHTML(optionText("authTypes", value))}</option>`).join("");
}

function proxyOptions(selected, excludeID = "") {
  const options = [`<option value="">${escapeHTML(t("targets.noProxy"))}</option>`];
  for (const target of state.targets) {
    if (target.id === excludeID) continue;
    options.push(`<option value="${escapeHTML(target.id)}" ${selected === target.id ? "selected" : ""}>${escapeHTML(target.name || target.alias)} (${escapeHTML(target.alias)})</option>`);
  }
  return options.join("");
}

function proxyLabel(id) {
  if (!id) return t("targets.noProxy");
  const target = state.targets.find((item) => item.id === id);
  return target ? `${target.name || target.alias} (${target.alias})` : id;
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
      ${sectionBlock(t("targets.editTitle"), t("targets.editSub"), `
        <form data-action="rename-target" data-target-id="${escapeHTML(target.id)}" class="stack">
          <div class="form-grid">
            <label class="field"><span>${escapeHTML(t("targets.name"))}</span><input name="name" value="${escapeHTML(target.name || "")}" placeholder="${escapeHTML(t("targets.namePlaceholder"))}" /></label>
            <label class="field"><span>${escapeHTML(t("targets.aliasShort"))}</span><input name="alias" value="${escapeHTML(target.alias)}" placeholder="${escapeHTML(t("targets.aliasShortPlaceholder"))}" /></label>
            ${tagInput({ label: t("targets.tagsShort"), value: target.tags || [], placeholder: t("targets.tagsShortPlaceholder"), className: "span-two" }).__raw}
          </div>
          <div class="form-grid">
            <label class="field span-two"><span>${escapeHTML(t("targets.host"))}</span><input name="host" value="${escapeHTML(target.host)}" autocomplete="off" placeholder="${escapeHTML(t("targets.hostPlaceholder"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.port"))}</span><input name="port" type="number" min="1" max="65535" value="${escapeHTML(target.port)}" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.remoteUsername"))}</span><input name="remote_username" value="${escapeHTML(target.remote_username)}" autocomplete="off" placeholder="${escapeHTML(t("targets.remoteUsernamePlaceholder"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("targets.authType"))}</span><select name="auth_type">${authOptions(target.auth_type)}</select></label>
            <label class="field span-two"><span>${escapeHTML(t("targets.secret"))}</span><input name="secret" autocomplete="off" placeholder="${escapeHTML(t("targets.secretKeepPlaceholder"))}" /></label>
          </div>
          <details class="advanced-panel">
            <summary>${escapeHTML(t("targets.advancedProxy"))}</summary>
            <p>${escapeHTML(t("targets.advancedProxySub"))}</p>
            <label class="field"><span>${escapeHTML(t("targets.proxyTarget"))}</span><select name="proxy_target_id">${proxyOptions(target.proxy_target_id, target.id)}</select></label>
          </details>
          <button type="submit" class="primary">${escapeHTML(t("common.save"))}</button>
        </form>
      `).__raw}
      ${sectionBlock(t("targets.tagColorTitle"), t("targets.tagColorSub"), targetTagColorEditor(target)).__raw}
    `,
  });
}

function targetTags(target) {
  const tags = target.tags || [];
  if (!tags.length) return `<span class="muted">-</span>`;
  return `<div class="tag-row">${tags.map((tag) => tagChip(tag, targetTagColor(target, tag))).join("")}</div>`;
}

function targetTagColorEditor(target) {
  const tags = target.tags || [];
  if (!tags.length) return `<p class="muted">${escapeHTML(t("targets.noTagsForColor"))}</p>`;
  return `<div class="tag-color-editor">${tags.map((tag) => {
    const current = targetTagColor(target, tag);
    return `
      <section class="tag-color-row">
        ${tagChip(tag, current)}
        <div class="tag-color-swatches">${TAG_COLORS.map((color) => tagColorSwatch(tag, color, current)).join("")}</div>
      </section>
    `;
  }).join("")}</div>`;
}

function tagColorSwatch(tag, color, current) {
  const label = `${t("targets.setTagColor")} ${tag} ${optionText("tagColors", color)}`;
  return `<button type="button" class="tag-swatch ${tagColorClass(color, tag)} ${current === color ? "active" : ""}" data-click="set-target-tag-color" data-tag="${escapeHTML(tag)}" data-color="${escapeHTML(color)}" aria-label="${escapeHTML(label)}" title="${escapeHTML(label)}"><i></i></button>`;
}

function targetTagColor(target, tag) {
  return target.tag_colors?.[tag] || tagColorForName(tag);
}
