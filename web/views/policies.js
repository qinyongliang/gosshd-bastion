import { allTargetTagDetails, filteredPolicies, state, tagColorForName } from "../state.js";
import { badge, emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { selectOptions } from "../components/forms.js";
import { cloudTable, detailList, drawer, modal, resourceHeader, resourceToolbar, rowButton, sectionBlock, selectionSummary } from "../components/management.js";
import { t } from "../i18n.js";
import { tagChip } from "../tag-colors.js";

export function renderPolicies() {
  const policies = filteredPolicies();
  return raw(`
    ${resourceHeader({
      title: t("policies.title"),
      subtitle: t("policies.sub"),
      actions: `
        <button type="button" class="primary" data-click="open-modal" data-modal="create-policy">${icon("plus").__raw}${escapeHTML(t("policies.create"))}</button>
        <button type="button" data-click="open-modal" data-modal="create-rule">${escapeHTML(t("policies.addRule"))}</button>
        <button type="button" data-click="open-modal" data-modal="create-llm">${escapeHTML(t("policies.configureLLM"))}</button>
      `,
      stats: [
        { label: t("nav.policies"), value: state.policies.length },
        { label: t("policies.ruleCount"), value: state.policies.reduce((sum, policy) => sum + (policy.rules || []).length, 0), tone: "warning" },
        { label: t("policies.boundTargets"), value: state.policies.reduce((sum, policy) => sum + (policy.target_ids || []).length + (policy.target_tags || []).length, 0), tone: "success" },
        { label: t("policies.llmCount"), value: state.llms.length, tone: "info" },
      ],
    }).__raw}
    ${resourceToolbar({
      searchAction: "set-policy-filter",
      query: state.policyQuery,
      searchPlaceholder: t("policies.searchPlaceholder"),
      chips: allTargetTagDetails().map((tag) => tagChip(tag.name, tag.color)).join(""),
      actions: `${selectionSummary(0)}<button type="button" data-click="clear-policy-filter">${escapeHTML(t("targets.clearFilters"))}</button>`,
    }).__raw}
    ${policyTable(policies).__raw}
    <div class="ops-grid">
      <button type="button" data-click="open-modal" data-modal="bind-policy-target">${escapeHTML(t("policies.bindTargetTitle"))}</button>
      <button type="button" data-click="open-modal" data-modal="bind-policy-tag">${escapeHTML(t("policies.bindTagTitle"))}</button>
      <button type="button" data-click="open-modal" data-modal="bind-policy-group">${escapeHTML(t("policies.bindGroupTitle"))}</button>
      <button type="button" data-click="open-modal" data-modal="create-prompt">${escapeHTML(t("policies.addPromptResource"))}</button>
    </div>
    ${policyModals()}
    ${policyDrawer().__raw || ""}
  `);
}

function policyTable(policies) {
  return cloudTable([
    "",
    t("policies.tablePolicy"),
    t("policies.tableDefault"),
    t("policies.ruleCount"),
    t("policies.tableTags"),
    t("policies.tableGroups"),
    t("management.operations"),
  ], policies.map((policy) => [
    `<input type="checkbox" aria-label="${escapeHTML(t("policies.tablePolicy"))}" />`,
    `<strong>${escapeHTML(policy.name)}</strong><small>${escapeHTML(policy.llm_config_id ? t("dashboard.streamLLM") : t("policies.noLLM"))}</small>`,
    badge(policy.default_action, policy.default_action === "allow" ? "success" : "danger").__raw,
    String((policy.rules || []).length),
    targetPolicyTags(policy),
    String((policy.user_group_ids || []).length || t("common.allUsers")),
    `<div class="row-actions">${rowButton(t("common.details"), "open-policy-detail", { "policy-id": policy.id })}</div>`,
  ]), {
    empty: emptyState(t("policies.emptyTitle"), t("policies.emptyBody")).__raw,
  });
}

function policyModals() {
  const tagChoices = allTargetTagDetails().map((tag) => ({ id: tag.name, name: tag.name }));
  return [
    modal(state, "create-policy", {
      title: t("policies.createModalTitle"),
      subtitle: t("policies.createModalSub"),
      body: `
        <form data-action="create-policy" data-close-overlay="modal" class="modal-form">
          <div class="form-grid">
            <label class="field"><span>${escapeHTML(t("policies.name"))}</span><input name="name" autocomplete="off" placeholder="${escapeHTML(t("policies.name"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("policies.defaultAction"))}</span><select name="default_action"><option value="allow">${escapeHTML(t("policies.defaultAllow"))}</option><option value="deny">${escapeHTML(t("policies.defaultDeny"))}</option></select></label>
            <label class="field"><span>${escapeHTML(t("dashboard.streamLLM"))}</span>${selectOptions("llm_config_id", t("policies.noLLM"), state.llms, "name")}</label>
            <label class="field"><span>${escapeHTML(t("policies.defaultPrompt"))}</span>${selectOptions("llm_prompt_id", t("policies.defaultPrompt"), state.prompts, "title")}</label>
          </div>
          <footer class="modal-actions"><button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button><button type="submit" class="primary">${escapeHTML(t("policies.create"))}</button></footer>
        </form>
      `,
    }).__raw,
    modal(state, "create-rule", {
      title: t("policies.addRuleTitle"),
      body: relationForm("add-rule", `
        ${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}
        <select name="rule_type" aria-label="${escapeHTML(t("policies.ruleType"))}"><option value="blacklist">${escapeHTML(t("common.blacklist"))}</option><option value="whitelist">${escapeHTML(t("common.whitelist"))}</option></select>
        <select name="pattern_type" aria-label="${escapeHTML(t("policies.patternType"))}"><option value="contains">${escapeHTML(t("common.contains"))}</option><option value="exact">${escapeHTML(t("common.exact"))}</option><option value="prefix">${escapeHTML(t("common.prefix"))}</option></select>
        <input name="pattern" aria-label="${escapeHTML(t("policies.pattern"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.patternPlaceholder"))}" required />
      `, t("policies.addRule")),
    }).__raw,
    modal(state, "bind-policy-target", {
      title: t("policies.bindTargetTitle"),
      body: relationForm("bind-policy-target", `${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}${selectOptions("target_id", t("policies.target"), state.targets, "display_name")}`, t("policies.bindTarget")),
    }).__raw,
    modal(state, "bind-policy-tag", {
      title: t("policies.bindTagTitle"),
      body: relationForm("bind-policy-tag", `${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}${selectOptions("tag", t("policies.targetTag"), tagChoices, "name")}`, t("policies.bindTag")),
    }).__raw,
    modal(state, "bind-policy-group", {
      title: t("policies.bindGroupTitle"),
      body: relationForm("bind-policy-group", `${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}${selectOptions("group_id", t("policies.userGroup"), state.groups, "name")}`, t("policies.bindGroup")),
    }).__raw,
    modal(state, "create-llm", {
      title: t("policies.configureLLM"),
      subtitle: t("policies.llmSub"),
      body: `
        <form data-action="create-llm" data-close-overlay="modal" class="modal-form">
          <div class="form-grid">
            <label class="field"><span>${escapeHTML(t("policies.llmName"))}</span><input name="name" autocomplete="off" placeholder="${escapeHTML(t("policies.llmNamePlaceholder"))}" required /></label>
            <label class="field span-two"><span>${escapeHTML(t("policies.baseURL"))}</span><input name="base_url" type="url" autocomplete="off" placeholder="${escapeHTML(t("policies.baseURLPlaceholder"))}" required /></label>
            <label class="field"><span>${escapeHTML(t("policies.model"))}</span><input name="model" autocomplete="off" placeholder="${escapeHTML(t("policies.modelPlaceholder"))}" required /></label>
            <label class="field span-two"><span>${escapeHTML(t("policies.apiKey"))}</span><input name="api_key" autocomplete="off" placeholder="${escapeHTML(t("policies.apiKeyPlaceholder"))}" /></label>
            <label class="field"><span>${escapeHTML(t("policies.timeout"))}</span><input name="timeout_seconds" type="number" value="10" /></label>
          </div>
          <footer class="modal-actions"><button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button><button type="submit" class="primary">${escapeHTML(t("policies.saveLLM"))}</button></footer>
        </form>
      `,
    }).__raw,
    modal(state, "create-prompt", {
      title: t("policies.addPromptResource"),
      subtitle: t("policies.promptsSub"),
      body: `
        <form data-action="create-prompt" data-close-overlay="modal" class="modal-form">
          <label class="field"><span>${escapeHTML(t("policies.promptTitle"))}</span><input name="title" autocomplete="off" placeholder="${escapeHTML(t("policies.promptTitle"))}" required /></label>
          <label class="field"><span>${escapeHTML(t("policies.promptContent"))}</span><textarea name="content" autocomplete="off" placeholder="${escapeHTML(t("policies.promptContent"))}" required></textarea></label>
          <footer class="modal-actions"><button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button><button type="submit" class="primary">${escapeHTML(t("policies.addPrompt"))}</button></footer>
        </form>
      `,
    }).__raw,
  ].join("");
}

function relationForm(action, fields, label) {
  return `
    <form data-action="${escapeHTML(action)}" data-close-overlay="modal" class="modal-form">
      <div class="stack">${fields}</div>
      <footer class="modal-actions"><button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button><button type="submit" class="primary">${escapeHTML(label)}</button></footer>
    </form>
  `;
}

function policyDrawer() {
  const policy = state.policies.find((item) => item.id === state.ui.policyID);
  if (!policy) return "";
  return drawer(state, "policy-detail", {
    title: policy.name,
    subtitle: t("policies.detailSub"),
    meta: targetPolicyTags(policy),
    body: `
      ${sectionBlock(t("policies.detailTitle"), t("management.lifecycle"), detailList([
        [t("policies.defaultAction"), badge(policy.default_action, policy.default_action === "allow" ? "success" : "danger").__raw],
        [t("policies.ruleCount"), String((policy.rules || []).length)],
        [t("policies.boundTargets"), String((policy.target_ids || []).length)],
        [t("policies.llmTitle"), escapeHTML(policy.llm_config_id || t("policies.noLLM"))],
      ]).__raw).__raw}
      ${sectionBlock(t("policies.addRuleTitle"), t("management.relationships"), `
        <div class="rule-list">
          ${(policy.rules || []).length ? policy.rules.map((rule) => `<span><b>${escapeHTML(rule.rule_type)}</b>${escapeHTML(rule.pattern_type)}:${escapeHTML(rule.pattern)}</span>`).join("") : `<span class="muted">${escapeHTML(t("policies.emptyBody"))}</span>`}
        </div>
      `).__raw}
    `,
  });
}

function targetPolicyTags(policy) {
  const tags = policy.target_tags || [];
  if (!tags.length) return `<span class="muted">-</span>`;
  return `<div class="tag-row">${tags.map((tag) => tagChip(tag, tagColorForName(tag))).join("")}</div>`;
}
