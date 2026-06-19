import { canManageActiveOrg, canTransferActiveOrg, state } from "../state.js";
import { emptyState, escapeHTML, panel, raw, table } from "../components/html.js";
import { selectOptions } from "../components/forms.js";
import { optionText, t } from "../i18n.js";

export function renderOrgAdmin() {
  if (!canManageActiveOrg()) {
    return panel(t("members.title"), t("members.noAccessSub"), emptyState(t("members.noAccessTitle"), t("members.noAccessBody")).__raw);
  }
  return raw(`
    <div class="grid two">
      ${panel(t("members.addTitle"), t("members.addSub"), `
        <form data-action="add-org-member" class="stack">
          <input name="email" aria-label="${escapeHTML(t("members.email"))}" autocomplete="off" placeholder="user@example.com" />
          <input name="user_id" aria-label="${escapeHTML(t("members.userId"))}" autocomplete="off" placeholder="${escapeHTML(t("members.userId"))}" />
          <select name="role" aria-label="${escapeHTML(t("members.role"))}"><option value="member">${escapeHTML(t("roles.member"))}</option><option value="admin">${escapeHTML(t("roles.admin"))}</option></select>
          <button type="submit">${escapeHTML(t("members.addButton"))}</button>
        </form>
      `).__raw}
      ${panel(t("members.groupsTitle"), t("members.groupsSub"), `
        <div class="list-lines">${state.groups.map((group) => `<span>${escapeHTML(group.name)}${group.is_default ? ` - ${escapeHTML(t("members.defaultGroup"))}` : ""}</span>`).join("")}</div>
        <form data-action="create-group" class="stack compact">
          <input name="name" aria-label="${escapeHTML(t("members.groupName"))}" autocomplete="off" placeholder="${escapeHTML(t("members.groupName"))}" required />
          <input name="slug" aria-label="${escapeHTML(t("members.groupSlug"))}" autocomplete="off" placeholder="${escapeHTML(t("members.groupSlug"))}" required />
          <button type="submit">${escapeHTML(t("members.addGroup"))}</button>
        </form>
      `).__raw}
    </div>
    ${panel(t("members.membersTitle"), t("members.membersSub"), memberTable()).__raw}
    ${canTransferActiveOrg() ? panel(t("members.transferTitle"), t("members.transferSub"), `
      <form data-action="transfer-org-owner" class="inline-form">
        ${selectOptions("user_id", t("members.newOwner"), state.members, "email")}
        <button type="submit" class="danger">${escapeHTML(t("members.transferButton"))}</button>
      </form>
    `).__raw : ""}
  `);
}

function memberTable() {
  if (!state.members.length) return emptyState(t("members.emptyTitle"), t("members.emptyBody")).__raw;
  return table([t("members.tableUser"), t("members.tableRole"), t("members.tableSetRole"), t("members.tableRemove")], state.members.map((member) => [
    `<strong>${escapeHTML(member.display_name || member.email)}</strong><small>${escapeHTML(member.email)}</small>`,
    escapeHTML(optionText("roles", member.role)),
    `<form data-action="update-org-member" data-user-id="${escapeHTML(member.user_id)}" class="row-form"><select name="role" aria-label="${escapeHTML(t("members.role"))} ${escapeHTML(member.email)}"><option value="member" ${member.role === "member" ? "selected" : ""}>${escapeHTML(t("roles.member"))}</option><option value="admin" ${member.role === "admin" ? "selected" : ""}>${escapeHTML(t("roles.admin"))}</option></select><button type="submit">${escapeHTML(t("common.save"))}</button></form>`,
    member.role === "owner" ? "" : `<button data-click="remove-org-member" data-user-id="${escapeHTML(member.user_id)}" class="danger small">${escapeHTML(t("common.remove"))}</button>`,
  ]));
}
