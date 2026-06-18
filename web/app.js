import { api } from "./api.js";
import { badge, emptyState, escapeHTML, field, formData, html, icon, raw } from "./components.js";

const app = document.querySelector("#app");
const state = {
  user: null,
  orgs: [],
  activeOrgID: "",
  keys: [],
  groups: [],
  targets: [],
  policies: [],
  audit: [],
  llms: [],
  prompts: [],
  targetTagFilters: [],
  notice: "",
  error: "",
  enrollment: null,
  invite: "",
};

boot();

async function boot() {
  bindEvents();
  await refresh();
}

function bindEvents() {
  app.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = event.target;
    const action = form.dataset.action;
    const data = formData(form);
    await run(async () => {
      if (action === "register") await api.register(data);
      if (action === "login") await api.login(data);
      if (action === "create-org") await api.createOrg(data);
      if (action === "join-org") await api.joinOrg(data.code);
      if (action === "create-key") await api.createKey({ name: data.name, authorized_key: data.authorized_key });
      if (action === "create-group") await api.createGroup(activeOrg().id, data);
      if (action === "create-target") await createTarget(data);
      if (action === "create-agent") state.enrollment = await api.enrollAgent({ ...ownerPayload(data), default_port: Number(data.default_port || 22) });
      if (action === "create-llm") await api.createLLMConfig({ ...ownerPayload(data), timeout_seconds: Number(data.timeout_seconds || 10) });
      if (action === "create-prompt") await api.createPrompt(ownerPayload(data));
      if (action === "create-policy") await createPolicy(data);
      if (action === "add-rule") await api.addRule(data.policy_id, {
        rule_type: data.rule_type,
        pattern_type: data.pattern_type,
        pattern: data.pattern,
      });
      if (action === "bind-policy-target") await api.bindTarget(data.policy_id, data.target_id);
      if (action === "bind-policy-group") await api.bindGroup(data.policy_id, data.group_id);
      form.reset();
      state.notice = "Saved";
      await refreshData();
      render();
    });
  });

  app.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-click]");
    if (!button) return;
    const action = button.dataset.click;
    await run(async () => {
      if (action === "logout") {
        await api.logout();
        Object.assign(state, { user: null, orgs: [], activeOrgID: "" });
      }
      if (action === "switch-org") {
        state.activeOrgID = button.dataset.id;
        await refreshData();
      }
      if (action === "leave-org") {
        await api.leaveOrg(activeOrg().id);
        state.activeOrgID = "";
        await refresh();
        return;
      }
      if (action === "invite") {
        const out = await api.invite(activeOrg().id, "member");
        state.invite = out.code;
      }
      if (action === "delete-key") {
        if (!window.confirm("Remove this public key?")) return;
        await api.deleteKey(button.dataset.id);
        await refreshData();
      }
      if (action === "copy") {
        await navigator.clipboard.writeText(button.dataset.value || "");
        state.notice = "Copied";
      }
      if (action === "toggle-target-tag") {
        const tag = button.dataset.tag || "";
        state.targetTagFilters = state.targetTagFilters.includes(tag)
          ? state.targetTagFilters.filter((item) => item !== tag)
          : [...state.targetTagFilters, tag];
      }
      render();
    });
  });
}

async function refresh() {
  try {
    const me = await api.me();
    state.user = me.user;
    state.orgs = me.organizations || [];
    if (!state.activeOrgID && state.orgs.length) state.activeOrgID = state.orgs[0].id;
    await refreshData();
  } catch {
    state.user = null;
  }
  render();
}

async function refreshData() {
  if (!state.user || !activeOrg()) return;
  const owner = owner();
  const [keys, groups, targets, policies, audit, llms, prompts] = await Promise.all([
    api.keys(),
    api.groups(activeOrg().id),
    api.targets(owner),
    api.policies(owner),
    api.audit(),
    api.llmConfigs(owner),
    api.prompts(owner),
  ]);
  state.keys = keys.keys || [];
  state.groups = groups.groups || [];
  state.targets = (targets.targets || []).map((target) => ({
    ...target,
    display_name: `${target.name || target.alias} (${target.alias})`,
  }));
  state.policies = policies.policies || [];
  state.audit = audit.logs || [];
  state.llms = llms.configs || [];
  state.prompts = prompts.prompts || [];
}

async function run(fn) {
  state.error = "";
  state.notice = "";
  try {
    await fn();
  } catch (error) {
    state.error = error.message;
    render();
  }
}

function render() {
  app.innerHTML = state.user ? renderConsole() : renderAuth();
}

function renderAuth() {
  return html`
    <section class="auth-screen">
      <div class="brand-panel">
        <div class="brand-row"><div class="mark">g</div><span>gosshd bastion</span></div>
        <h1>Access every SSH service through one clean control plane.</h1>
        <p>Organizations, keys, agent enrollment, command policy, audit, and MCP automation live together.</p>
      </div>
      <div class="auth-card">
        <div class="tabs"><span>Register</span><span>Login</span></div>
        <form data-action="register" class="stack">
          ${field("Email", "email", { type: "email", required: true, autocomplete: "email" })}
          ${field("Display name", "display_name", { required: true })}
          ${field("Password", "password", { type: "password", required: true, autocomplete: "new-password" })}
          <button class="primary" type="submit">${icon("spark")}Create account</button>
        </form>
        <form data-action="login" class="stack compact">
          ${field("Email", "email", { type: "email", required: true, autocomplete: "email" })}
          ${field("Password", "password", { type: "password", required: true, autocomplete: "current-password" })}
          <button type="submit">${icon("key")}Sign in</button>
        </form>
        ${statusLine()}
      </div>
    </section>
  `;
}

function renderConsole() {
  const org = activeOrg();
  return html`
    <section class="console">
      <aside class="sidebar">
        <div class="brand-row"><div class="mark">g</div><span>gosshd</span></div>
        <div class="user-block">
          <strong>${state.user.display_name}</strong>
          <span>${state.user.email}</span>
        </div>
        <div class="org-list">
          ${raw(state.orgs.map((item) => `
            <button data-click="switch-org" data-id="${item.id}" class="${item.id === state.activeOrgID ? "active" : ""}">
              <span>${escapeHTML(item.name)}</span>${item.is_personal ? '<small>personal</small>' : '<small>org</small>'}
            </button>
          `).join(""))}
        </div>
        <button data-click="logout" class="ghost">Sign out</button>
      </aside>
      <section class="workspace">
        <header class="topbar">
          <div>
            <p class="eyebrow">Bastion control plane</p>
            <h1>${org?.name || "No organization"}</h1>
          </div>
          <div class="top-actions">
            ${org?.is_personal ? badge("Personal", "info") : badge("Shared organization", "success")}
            ${!org?.is_personal ? raw('<button data-click="invite">Invite</button><button data-click="leave-org" class="danger">Leave</button>') : ""}
          </div>
        </header>
        ${statusLine()}
        ${state.invite ? invitePanel() : ""}
        <div class="metrics">
          ${metric("Targets", state.targets.length, "server")}
          ${metric("Policies", state.policies.length, "shield")}
          ${metric("Keys", state.keys.length, "key")}
          ${metric("Audit rows", state.audit.length, "log")}
        </div>
        <div class="grid two">
          ${orgPanel()}
          ${keysPanel()}
        </div>
        <div class="grid two">
          ${targetsPanel()}
          ${agentPanel()}
        </div>
        <div class="grid two">
          ${llmPanel()}
          ${promptPanel()}
        </div>
        ${policyPanel()}
        ${auditPanel()}
      </section>
    </section>
  `;
}

function orgPanel() {
  return panel("Organizations", "Create or join shared organizations.", `
    <form data-action="create-org" class="inline-form">
      <input name="name" aria-label="Organization name" autocomplete="off" placeholder="Organization name…" required />
      <input name="slug" aria-label="Organization slug" autocomplete="off" placeholder="slug…" required />
      <button type="submit">${icon("plus")}Create</button>
    </form>
    <form data-action="join-org" class="inline-form">
      <input name="code" aria-label="Invite code" autocomplete="off" placeholder="Invite code…" required />
      <button type="submit">Join</button>
    </form>
      <div class="list-lines">
      ${state.groups.map((group) => `<span>${escapeHTML(group.name)}${group.is_default ? " · default" : ""}</span>`).join("")}
    </div>
    <form data-action="create-group" class="inline-form">
      <input name="name" aria-label="User group name" autocomplete="off" placeholder="User group…" required />
      <input name="slug" aria-label="User group slug" autocomplete="off" placeholder="group-slug…" required />
      <button type="submit">Add group</button>
    </form>
  `);
}

function keysPanel() {
  return panel("Public keys", "Keys identify users at SSH login.", `
    <form data-action="create-key" class="stack">
      <input name="name" aria-label="Public key name" autocomplete="off" placeholder="Laptop…" required />
      <textarea name="authorized_key" aria-label="Authorized public key" autocomplete="off" spellcheck="false" placeholder="ssh-ed25519 AAAA…" required></textarea>
      <button type="submit">${icon("key")}Add key</button>
    </form>
    ${state.keys.length ? table(["Name", "Fingerprint", ""], state.keys.map((key) => [
      escapeHTML(key.name),
      escapeHTML(key.fingerprint),
        `<button data-click="delete-key" data-id="${escapeHTML(key.id)}" class="danger small">Remove</button>`,
    ])) : emptyState("No keys", "Add a public key before using SSH aliases.").__raw}
  `);
}

function targetsPanel() {
  const tags = allTargetTags();
  const targets = filteredTargets();
  return panel("SSH services", "Direct and agent-enrolled services share the same target model.", `
    <form data-action="create-target" class="stack">
      <div class="form-grid">
        <input name="name" aria-label="Service name" autocomplete="off" placeholder="service name…" required />
        <input name="alias" aria-label="Target alias" autocomplete="off" placeholder="alias, e.g. test2…" required />
        <input name="tags" aria-label="Target tags" autocomplete="off" placeholder="tags, comma separated…" />
        <select name="target_type" aria-label="Target type"><option value="direct">direct</option><option value="agent">agent</option></select>
        <input name="host" aria-label="Target host" autocomplete="off" placeholder="host…" required />
        <input name="port" aria-label="Target port" type="number" value="22" required />
        <input name="remote_username" aria-label="Remote username" autocomplete="off" placeholder="remote user…" required />
        <select name="auth_type" aria-label="Authentication type"><option value="password">password</option><option value="private_key">private key</option></select>
        <input name="secret" aria-label="Target secret" autocomplete="off" placeholder="password or private key…" />
        <input name="agent_id" aria-label="Agent id" autocomplete="off" placeholder="agent id for agent targets…" />
      </div>
      <button type="submit">${icon("server")}Add service</button>
    </form>
    ${tags.length ? `<div class="filter-chips">${tags.map((tag) => `
      <button type="button" data-click="toggle-target-tag" data-tag="${escapeHTML(tag)}" class="${state.targetTagFilters.includes(tag) ? "active" : ""}">${escapeHTML(tag)}</button>
    `).join("")}</div>` : ""}
    ${targets.length ? table(["Service", "Type", "Endpoint", "Auth", "Tags"], targets.map((target) => [
      `<strong>${escapeHTML(target.name || target.alias)}</strong><small>${escapeHTML(target.alias)}</small>`,
      escapeHTML(target.target_type),
      escapeHTML(`${target.remote_username}@${target.host}:${target.port}`),
      escapeHTML(target.auth_type),
      targetTags(target),
    ])) : emptyState("No SSH services", "Add a direct target or enroll an agent.").__raw}
  `);
}

function agentPanel() {
  return panel("Agent enrollment", "Install a private-side agent as a normal renameable SSH service.", `
    <form data-action="create-agent" class="stack">
      <input name="label" aria-label="Agent service alias" autocomplete="off" placeholder="service alias…" required />
      <input name="default_host" aria-label="Agent default host" autocomplete="off" value="127.0.0.1" required />
      <input name="default_port" aria-label="Agent default SSH port" type="number" value="22" required />
      <button type="submit">${icon("spark")}Create enrollment</button>
    </form>
    ${state.enrollment ? `
      <div class="guide-block">
        <strong>Run once</strong>
        <span>Starts the agent in the current terminal session.</span>
      </div>
      ${commandLine("Linux/macOS shell", state.enrollment.install_sh)}
      ${commandLine("Windows PowerShell", state.enrollment.install_ps1)}
      <div class="guide-block service">
        <strong>Install as startup service</strong>
        <span>Linux registers a systemd service with systemctl. Windows registers a service with sc.exe.</span>
      </div>
      ${commandLine("Linux systemctl service", state.enrollment.service_sh)}
      ${commandLine("Windows sc.exe service", state.enrollment.service_ps1)}
    ` : ""}
  `);
}

function llmPanel() {
  return panel("LLM configs", "Provider settings are owner-level resources.", `
    <form data-action="create-llm" class="stack">
      <input name="name" aria-label="LLM config name" autocomplete="off" placeholder="Reviewer…" required />
      <input name="base_url" aria-label="LLM base URL" type="url" autocomplete="off" placeholder="https://api.openai.com/v1…" required />
      <input name="api_key" aria-label="LLM API key" autocomplete="off" placeholder="API key…" />
      <input name="model" aria-label="LLM model" autocomplete="off" placeholder="model…" required />
      <input name="timeout_seconds" aria-label="LLM timeout seconds" type="number" value="10" />
      <button type="submit">Save LLM</button>
    </form>
    <div class="chips">${state.llms.map((cfg) => `<span>${escapeHTML(cfg.name)} · ${escapeHTML(cfg.model)}</span>`).join("")}</div>
  `);
}

function promptPanel() {
  return panel("Prompt resources", "Readonly defaults plus reusable policy prompts.", `
    <form data-action="create-prompt" class="stack">
      <input name="title" aria-label="Prompt title" autocomplete="off" placeholder="Prompt title…" required />
      <textarea name="content" aria-label="Prompt content" autocomplete="off" placeholder="Prompt content…" required></textarea>
      <button type="submit">Add prompt</button>
    </form>
    <div class="list-lines">
      ${state.prompts.map((prompt) => `<span>${escapeHTML(prompt.title)}${prompt.is_readonly ? " · readonly" : ""}</span>`).join("")}
    </div>
  `);
}

function policyPanel() {
  return panel("Command security groups", "Bind policies to targets and user groups. Blacklists, whitelists, and LLM review can be combined.", `
    <form data-action="create-policy" class="stack">
      <div class="form-grid">
        <input name="name" aria-label="Policy name" autocomplete="off" placeholder="Policy name…" required />
        <select name="default_action" aria-label="Default policy action"><option value="allow">default allow</option><option value="deny">default deny</option></select>
        ${selectOptions("llm_config_id", "No LLM", state.llms, "name")}
        ${selectOptions("llm_prompt_id", "Default prompt", state.prompts, "title")}
      </div>
      <button type="submit">${icon("shield")}Create policy</button>
    </form>
    ${state.policies.length ? table(["Policy", "Default", "Groups"], state.policies.map((policy) => [
      escapeHTML(policy.name),
      escapeHTML(policy.default_action),
      (policy.user_group_ids || []).length || "all users",
    ])) : emptyState("No policies", "Create a policy and bind it below.").__raw}
    <div class="grid three tight">
      <form data-action="add-rule" class="stack mini">
        ${selectOptions("policy_id", "Policy", state.policies, "name")}
        <select name="rule_type" aria-label="Rule type"><option value="blacklist">blacklist</option><option value="whitelist">whitelist</option></select>
        <select name="pattern_type" aria-label="Pattern type"><option value="contains">contains</option><option value="exact">exact</option><option value="prefix">prefix</option></select>
        <input name="pattern" aria-label="Command pattern" autocomplete="off" placeholder="pattern…" required />
        <button type="submit">Add rule</button>
      </form>
      <form data-action="bind-policy-target" class="stack mini">
        ${selectOptions("policy_id", "Policy", state.policies, "name")}
        ${selectOptions("target_id", "Target", state.targets, "display_name")}
        <button type="submit">Bind target</button>
      </form>
      <form data-action="bind-policy-group" class="stack mini">
        ${selectOptions("policy_id", "Policy", state.policies, "name")}
        ${selectOptions("group_id", "User group", state.groups, "name")}
        <button type="submit">Bind group</button>
      </form>
    </div>
  `);
}

function auditPanel() {
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

function panel(title, subtitle, body) {
  return raw(`<section class="panel"><div class="panel-head"><h2>${escapeHTML(title)}</h2><p>${escapeHTML(subtitle)}</p></div>${body}</section>`);
}

function commandLine(label, value) {
  return `<div class="command-box">
    <span>${escapeHTML(label)}</span>
    <code>${escapeHTML(value || "")}</code>
    <button data-click="copy" aria-label="Copy ${escapeHTML(label)} command" data-value="${escapeHTML(value || "")}">${icon("copy").__raw}</button>
  </div>`;
}

function metric(label, value, iconName) {
  return raw(`<div class="metric">${icon(iconName).__raw}<span>${label}</span><strong>${value}</strong></div>`);
}

function table(headers, rows) {
  return `<div class="table-wrap"><table><thead><tr>${headers.map((h) => `<th>${escapeHTML(h)}</th>`).join("")}</tr></thead><tbody>${rows
    .map((row) => `<tr>${row.map((cell) => `<td>${cell}</td>`).join("")}</tr>`)
    .join("")}</tbody></table></div>`;
}

function selectOptions(name, label, items, textKey) {
  return `<select name="${name}" aria-label="${escapeHTML(label)}"><option value="">${escapeHTML(label)}</option>${items
    .map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item[textKey])}</option>`)
    .join("")}</select>`;
}

function invitePanel() {
  return raw(`<div class="notice-card"><strong>Invite code</strong><code>${escapeHTML(state.invite)}</code><button data-click="copy" aria-label="Copy invite code" data-value="${escapeHTML(state.invite)}">${icon("copy").__raw}</button></div>`);
}

function statusLine() {
  if (state.error) return raw(`<div class="status error" aria-live="polite">${escapeHTML(state.error)}</div>`);
  if (state.notice) return raw(`<div class="status ok" aria-live="polite">${escapeHTML(state.notice)}</div>`);
  return "";
}

function owner() {
  return { owner_type: "organization", owner_id: activeOrg()?.id };
}

function ownerPayload(data) {
  return { ...data, owner_type: "organization", owner_id: activeOrg().id };
}

function activeOrg() {
  return state.orgs.find((org) => org.id === state.activeOrgID) || state.orgs[0];
}

async function createTarget(data) {
  const tags = splitTags(data.tags);
  await api.createTarget({
    ...ownerPayload(data),
    port: Number(data.port || 22),
    tags,
  });
}

async function createPolicy(data) {
  await api.createPolicy(ownerPayload(data));
}

function splitTags(raw) {
  return String(raw || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function allTargetTags() {
  return [...new Set(state.targets.flatMap((target) => target.tags || []))].sort((a, b) => a.localeCompare(b));
}

function filteredTargets() {
  if (!state.targetTagFilters.length) return state.targets;
  return state.targets.filter((target) => state.targetTagFilters.every((tag) => (target.tags || []).includes(tag)));
}

function targetTags(target) {
  const tags = target.tags || [];
  if (!tags.length) return "";
  return `<div class="tag-row">${tags.map((tag) => `<span>${escapeHTML(tag)}</span>`).join("")}</div>`;
}
