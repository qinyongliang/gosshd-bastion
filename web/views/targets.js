import { allTargetTags, filteredTargets, state } from "../state.js";
import { emptyState, escapeHTML, icon, panel, raw, table } from "../components/html.js";

export function renderTargets() {
  const tags = allTargetTags();
  const targets = filteredTargets();
  return raw(`
    ${panel("Add SSH service", "Direct and agent-enrolled clients use the same renameable target model.", `
      <form data-action="create-target" class="stack">
        <div class="form-grid">
          <input name="name" aria-label="Service name" autocomplete="off" placeholder="service name" required />
          <input name="alias" aria-label="Target alias" autocomplete="off" placeholder="alias, e.g. test2" required />
          <input name="tags" aria-label="Target tags" autocomplete="off" placeholder="tags, comma separated" />
          <select name="target_type" aria-label="Target type"><option value="direct">direct</option><option value="agent">agent</option></select>
          <input name="host" aria-label="Target host" autocomplete="off" placeholder="host" required />
          <input name="port" aria-label="Target port" type="number" value="22" required />
          <input name="remote_username" aria-label="Remote username" autocomplete="off" placeholder="remote user" required />
          <select name="auth_type" aria-label="Authentication type"><option value="password">password</option><option value="private_key">private key</option></select>
          <input name="secret" aria-label="Target secret" autocomplete="off" placeholder="password or private key" />
          <input name="agent_id" aria-label="Agent id" autocomplete="off" placeholder="agent id for agent targets" />
        </div>
        <button type="submit">${icon("server").__raw}Add service</button>
      </form>
    `).__raw}
    ${tags.length ? `<div class="filter-chips">${tags.map((tag) => `
      <button type="button" data-click="toggle-target-tag" data-tag="${escapeHTML(tag)}" class="${state.targetTagFilters.includes(tag) ? "active" : ""}">${escapeHTML(tag)}</button>
    `).join("")}</div>` : ""}
    ${panel("SSH services", "Use ssh alias@public-ip after adding a public key. Agent services can be renamed here too.", targets.length ? table(["Service", "Alias", "Endpoint", "Auth", "Tags", "Rename"], targets.map((target) => [
      `<strong>${escapeHTML(target.name || target.alias)}</strong><small>${escapeHTML(target.target_type)}</small>`,
      escapeHTML(target.alias),
      escapeHTML(`${target.remote_username}@${target.host}:${target.port}`),
      escapeHTML(target.auth_type),
      targetTags(target),
      `<form data-action="rename-target" data-target-id="${escapeHTML(target.id)}" class="row-form"><input name="name" aria-label="Name" value="${escapeHTML(target.name || "")}" placeholder="name" /><input name="alias" aria-label="Alias" value="${escapeHTML(target.alias)}" placeholder="alias" /><input name="tags" aria-label="Tags" value="${escapeHTML((target.tags || []).join(", "))}" placeholder="tags" /><button type="submit">Save</button></form>`,
    ])) : emptyState("No SSH services", "Add a direct target or enroll an agent.").__raw).__raw}
  `);
}

function targetTags(target) {
  const tags = target.tags || [];
  if (!tags.length) return "";
  return `<div class="tag-row">${tags.map((tag) => `<span>${escapeHTML(tag)}</span>`).join("")}</div>`;
}
