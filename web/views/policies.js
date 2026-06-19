import { allTargetTags, state } from "../state.js";
import { badge, emptyState, escapeHTML, icon, panel, raw, table } from "../components/html.js";
import { selectOptions } from "../components/forms.js";
import { t } from "../i18n.js";

export function renderPolicies() {
  const tagChoices = allTargetTags().map((tag) => ({ id: tag, name: tag }));
  return raw(`
    ${panel(t("policies.title"), t("policies.sub"), `
      <form data-action="create-policy" class="stack">
        <div class="form-grid">
          <input name="name" aria-label="${escapeHTML(t("policies.name"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.name"))}" required />
          <select name="default_action" aria-label="${escapeHTML(t("policies.defaultAction"))}"><option value="allow">${escapeHTML(t("policies.defaultAllow"))}</option><option value="deny">${escapeHTML(t("policies.defaultDeny"))}</option></select>
          ${selectOptions("llm_config_id", t("policies.noLLM"), state.llms, "name")}
          ${selectOptions("llm_prompt_id", t("policies.defaultPrompt"), state.prompts, "title")}
        </div>
        <button type="submit">${icon("policies").__raw}${escapeHTML(t("policies.create"))}</button>
      </form>
      ${state.policies.length ? table([t("policies.tablePolicy"), t("policies.tableDefault"), t("policies.tableTags"), t("policies.tableGroups")], state.policies.map((policy) => [
        escapeHTML(policy.name),
        escapeHTML(policy.default_action),
        targetPolicyTags(policy),
        (policy.user_group_ids || []).length || t("common.allUsers"),
      ])) : emptyState(t("policies.emptyTitle"), t("policies.emptyBody")).__raw}
      <div class="grid four tight">
        <form data-action="add-rule" class="stack mini">
          ${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}
          <select name="rule_type" aria-label="${escapeHTML(t("policies.ruleType"))}"><option value="blacklist">${escapeHTML(t("common.blacklist"))}</option><option value="whitelist">${escapeHTML(t("common.whitelist"))}</option></select>
          <select name="pattern_type" aria-label="${escapeHTML(t("policies.patternType"))}"><option value="contains">${escapeHTML(t("common.contains"))}</option><option value="exact">${escapeHTML(t("common.exact"))}</option><option value="prefix">${escapeHTML(t("common.prefix"))}</option></select>
          <input name="pattern" aria-label="${escapeHTML(t("policies.pattern"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.patternPlaceholder"))}" required />
          <button type="submit">${escapeHTML(t("policies.addRule"))}</button>
        </form>
        <form data-action="bind-policy-target" class="stack mini">
          ${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}
          ${selectOptions("target_id", t("policies.target"), state.targets, "display_name")}
          <button type="submit">${escapeHTML(t("policies.bindTarget"))}</button>
        </form>
        <form data-action="bind-policy-tag" class="stack mini">
          ${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}
          ${selectOptions("tag", t("policies.targetTag"), tagChoices, "name")}
          <button type="submit">${escapeHTML(t("policies.bindTag"))}</button>
        </form>
        <form data-action="bind-policy-group" class="stack mini">
          ${selectOptions("policy_id", t("policies.policy"), state.policies, "name")}
          ${selectOptions("group_id", t("policies.userGroup"), state.groups, "name")}
          <button type="submit">${escapeHTML(t("policies.bindGroup"))}</button>
        </form>
      </div>
    `).__raw}
    <div class="grid two">
      ${panel(t("policies.llmTitle"), t("policies.llmSub"), `
        <form data-action="create-llm" class="stack">
          <input name="name" aria-label="${escapeHTML(t("policies.llmName"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.llmNamePlaceholder"))}" required />
          <input name="base_url" aria-label="${escapeHTML(t("policies.baseURL"))}" type="url" autocomplete="off" placeholder="${escapeHTML(t("policies.baseURLPlaceholder"))}" required />
          <input name="api_key" aria-label="${escapeHTML(t("policies.apiKey"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.apiKeyPlaceholder"))}" />
          <input name="model" aria-label="${escapeHTML(t("policies.model"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.modelPlaceholder"))}" required />
          <input name="timeout_seconds" aria-label="${escapeHTML(t("policies.timeout"))}" type="number" value="10" />
          <button type="submit">${escapeHTML(t("policies.saveLLM"))}</button>
        </form>
        <div class="chips">${state.llms.map((cfg) => `<span>${escapeHTML(cfg.name)} - ${escapeHTML(cfg.model)}</span>`).join("")}</div>
      `).__raw}
      ${panel(t("policies.promptsTitle"), t("policies.promptsSub"), `
        <form data-action="create-prompt" class="stack">
          <input name="title" aria-label="${escapeHTML(t("policies.promptTitle"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.promptTitle"))}" required />
          <textarea name="content" aria-label="${escapeHTML(t("policies.promptContent"))}" autocomplete="off" placeholder="${escapeHTML(t("policies.promptContent"))}" required></textarea>
          <button type="submit">${escapeHTML(t("policies.addPrompt"))}</button>
        </form>
        <div class="list-lines">
          ${state.prompts.map((prompt) => `<span>${escapeHTML(prompt.title)}${prompt.is_readonly ? ` - ${escapeHTML(t("common.readonly"))}` : ""}</span>`).join("")}
        </div>
      `).__raw}
    </div>
  `);
}

function targetPolicyTags(policy) {
  const tags = policy.target_tags || [];
  if (!tags.length) return "";
  return `<div class="tag-row">${tags.map((tag) => `<span>${escapeHTML(tag)}</span>`).join("")}</div>`;
}
