import { canManageActiveOrg, canTransferActiveOrg, state } from "../state.js";
import { emptyState, escapeHTML, panel, raw, table } from "../components/html.js";
import { selectOptions } from "../components/forms.js";

export function renderOrgAdmin() {
  if (!canManageActiveOrg()) {
    return panel("Organization members", "Only organization owners, organization admins, and system admins can manage members.", emptyState("No management access", "Ask an organization owner to promote your account.").__raw);
  }
  return raw(`
    <div class="grid two">
      ${panel("Add member", "Add by email or user id and assign an organization role.", `
        <form data-action="add-org-member" class="stack">
          <input name="email" aria-label="Member email" autocomplete="off" placeholder="user@example.com" />
          <input name="user_id" aria-label="Member user id" autocomplete="off" placeholder="or user id" />
          <select name="role" aria-label="Role"><option value="member">member</option><option value="admin">admin</option></select>
          <button type="submit">Add member</button>
        </form>
      `).__raw}
      ${panel("User groups", "Create user groups for command security group binding.", `
        <div class="list-lines">${state.groups.map((group) => `<span>${escapeHTML(group.name)}${group.is_default ? " - default all-members" : ""}</span>`).join("")}</div>
        <form data-action="create-group" class="stack compact">
          <input name="name" aria-label="User group name" autocomplete="off" placeholder="User group name" required />
          <input name="slug" aria-label="User group slug" autocomplete="off" placeholder="group-slug" required />
          <button type="submit">Add group</button>
        </form>
      `).__raw}
    </div>
    ${panel("Members", "Promote, demote, remove, or transfer ownership.", memberTable()).__raw}
    ${canTransferActiveOrg() ? panel("Transfer owner", "The target must already be a member. The previous owner becomes admin.", `
      <form data-action="transfer-org-owner" class="inline-form">
        ${selectOptions("user_id", "New owner", state.members, "email")}
        <button type="submit" class="danger">Transfer owner</button>
      </form>
    `).__raw : ""}
  `);
}

function memberTable() {
  if (!state.members.length) return emptyState("No members loaded", "Refresh after adding the first member.").__raw;
  return table(["User", "Role", "Set role", "Remove"], state.members.map((member) => [
    `<strong>${escapeHTML(member.display_name || member.email)}</strong><small>${escapeHTML(member.email)}</small>`,
    escapeHTML(member.role),
    `<form data-action="update-org-member" data-user-id="${escapeHTML(member.user_id)}" class="row-form"><select name="role" aria-label="Role for ${escapeHTML(member.email)}"><option value="member" ${member.role === "member" ? "selected" : ""}>member</option><option value="admin" ${member.role === "admin" ? "selected" : ""}>admin</option></select><button type="submit">Save</button></form>`,
    member.role === "owner" ? "" : `<button data-click="remove-org-member" data-user-id="${escapeHTML(member.user_id)}" class="danger small">Remove</button>`,
  ]));
}
