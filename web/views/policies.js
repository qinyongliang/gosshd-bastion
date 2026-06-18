import { allTargetTags, state } from "../state.js";
import { badge, emptyState, escapeHTML, icon, panel, raw, table } from "../components/html.js";
import { selectOptions } from "../components/forms.js";

export function renderPolicies() {
  const tagChoices = allTargetTags().map((tag) => ({ id: tag, name: tag }));
  return raw(`
    ${panel("Command security groups", "Bind policies to SSH services, target tags, and user groups. Unmatched commands can be routed to an LLM reviewer.", `
      <form data-action="create-policy" class="stack">
        <div class="form-grid">
          <input name="name" aria-label="Policy name" autocomplete="off" placeholder="Policy name" required />
          <select name="default_action" aria-label="Default policy action"><option value="allow">default allow</option><option value="deny">default deny</option></select>
          ${selectOptions("llm_config_id", "No LLM", state.llms, "name")}
          ${selectOptions("llm_prompt_id", "Default prompt", state.prompts, "title")}
        </div>
        <button type="submit">${icon("policies").__raw}Create policy</button>
      </form>
      ${state.policies.length ? table(["Policy", "Default", "Tags", "Groups"], state.policies.map((policy) => [
        escapeHTML(policy.name),
        escapeHTML(policy.default_action),
        targetPolicyTags(policy),
        (policy.user_group_ids || []).length || "all users",
      ])) : emptyState("No policies", "Create a policy and bind it below.").__raw}
      <div class="grid four tight">
        <form data-action="add-rule" class="stack mini">
          ${selectOptions("policy_id", "Policy", state.policies, "name")}
          <select name="rule_type" aria-label="Rule type"><option value="blacklist">blacklist</option><option value="whitelist">whitelist</option></select>
          <select name="pattern_type" aria-label="Pattern type"><option value="contains">contains</option><option value="exact">exact</option><option value="prefix">prefix</option></select>
          <input name="pattern" aria-label="Command pattern" autocomplete="off" placeholder="pattern" required />
          <button type="submit">Add rule</button>
        </form>
        <form data-action="bind-policy-target" class="stack mini">
          ${selectOptions("policy_id", "Policy", state.policies, "name")}
          ${selectOptions("target_id", "Target", state.targets, "display_name")}
          <button type="submit">Bind target</button>
        </form>
        <form data-action="bind-policy-tag" class="stack mini">
          ${selectOptions("policy_id", "Policy", state.policies, "name")}
          ${selectOptions("tag", "Target tag", tagChoices, "name")}
          <button type="submit">Bind tag</button>
        </form>
        <form data-action="bind-policy-group" class="stack mini">
          ${selectOptions("policy_id", "Policy", state.policies, "name")}
          ${selectOptions("group_id", "User group", state.groups, "name")}
          <button type="submit">Bind group</button>
        </form>
      </div>
    `).__raw}
    <div class="grid two">
      ${panel("LLM configs", "Set base URL, key, model, and timeout for realtime command review.", `
        <form data-action="create-llm" class="stack">
          <input name="name" aria-label="LLM config name" autocomplete="off" placeholder="Reviewer" required />
          <input name="base_url" aria-label="LLM base URL" type="url" autocomplete="off" placeholder="https://api.example/v1" required />
          <input name="api_key" aria-label="LLM API key" autocomplete="off" placeholder="API key" />
          <input name="model" aria-label="LLM model" autocomplete="off" placeholder="model" required />
          <input name="timeout_seconds" aria-label="LLM timeout seconds" type="number" value="10" />
          <button type="submit">Save LLM</button>
        </form>
        <div class="chips">${state.llms.map((cfg) => `<span>${escapeHTML(cfg.name)} - ${escapeHTML(cfg.model)}</span>`).join("")}</div>
      `).__raw}
      ${panel("Prompt resources", "Readonly defaults plus reusable policy prompts.", `
        <form data-action="create-prompt" class="stack">
          <input name="title" aria-label="Prompt title" autocomplete="off" placeholder="Prompt title" required />
          <textarea name="content" aria-label="Prompt content" autocomplete="off" placeholder="Prompt content" required></textarea>
          <button type="submit">Add prompt</button>
        </form>
        <div class="list-lines">
          ${state.prompts.map((prompt) => `<span>${escapeHTML(prompt.title)}${prompt.is_readonly ? " - readonly" : ""}</span>`).join("")}
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
