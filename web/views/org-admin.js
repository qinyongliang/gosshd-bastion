import { canManageActiveOrg, canTransferActiveOrg, filteredMembers, state } from "../state.js";
import { emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { modal, resourceHeader, resourceToolbar, rowButton, sectionBlock, cloudTable } from "../components/management.js";
import { optionText, t } from "../i18n.js";

export function renderOrgAdmin() {
  if (!canManageActiveOrg()) {
    return sectionBlock(t("members.title"), t("members.noAccessSub"), emptyState(t("members.noAccessTitle"), t("members.noAccessBody")).__raw);
  }
  const members = filteredMembers();
  return raw(`
    ${resourceHeader({
      title: t("members.membersTitle"),
      subtitle: t("members.membersSub"),
      actions: headerActions(),
      stats: [
        { label: t("members.total"), value: state.members.length },
        { label: t("members.adminsTotal"), value: state.members.filter((member) => member.role === "owner" || member.role === "admin").length, tone: "warning" },
        { label: t("members.groupsTotal"), value: state.groups.length, tone: "success" },
        { label: t("members.visibleTotal"), value: members.length, tone: "info" },
      ],
    }).__raw}
    ${sectionBlock(t("members.groupsTitle"), t("members.groupsSub"), groupStrip()).__raw}
    ${sectionBlock(t("members.membersTitle"), t("members.listSub"), `
      ${resourceToolbar({
        searchAction: "set-member-filter",
        query: state.memberQuery,
        searchPlaceholder: t("members.searchPlaceholder"),
        chips: sortChips(),
        actions: `<button type="button" data-click="clear-member-filter">${escapeHTML(t("targets.clearFilters"))}</button>`,
      }).__raw}
      ${memberTable(members)}
    `).__raw}
    ${memberModals()}
  `);
}

function headerActions() {
  return `
    <button type="button" class="primary" data-click="open-modal" data-modal="add-member">${icon("plus").__raw}${escapeHTML(t("members.addButton"))}</button>
    <button type="button" data-click="open-modal" data-modal="member-groups">${icon("org").__raw}${escapeHTML(t("members.groupsTitle"))}</button>
    ${canTransferActiveOrg() ? `<button type="button" data-click="open-transfer-owner">${escapeHTML(t("members.transferButton"))}</button>` : ""}
  `;
}

function groupStrip() {
  if (!state.groups.length) return emptyState(t("members.emptyGroupsTitle"), t("members.emptyGroupsBody")).__raw;
  return `<div class="member-group-strip">${state.groups.map((group) => `
    <span>
      <strong>${escapeHTML(group.name)}</strong>
      <small>${escapeHTML(group.is_default ? t("members.defaultGroup") : group.slug)}</small>
    </span>
  `).join("")}</div>`;
}

function sortChips() {
  return [
    ["role", t("members.sortRole")],
    ["name", t("members.sortName")],
    ["joined_desc", t("members.sortNewest")],
    ["joined_asc", t("members.sortOldest")],
  ].map(([value, label]) => `
    <button type="button" data-click="set-member-sort" data-value="${escapeHTML(value)}" class="${state.memberSort === value ? "active" : ""}">
      ${escapeHTML(label)}
    </button>
  `).join("");
}

function memberTable(members) {
  if (!members.length) return emptyState(t("members.emptyTitle"), t("members.emptyBody")).__raw;
  return cloudTable(
    [t("members.tableUser"), t("members.tableRole"), t("members.tableJoined"), t("management.operations")],
    members.map((member) => [
      `<strong>${escapeHTML(member.display_name || member.email)}</strong><small>${escapeHTML(member.email)}</small><small>${escapeHTML(member.user_id)}</small>`,
      escapeHTML(optionText("roles", member.role)),
      escapeHTML(formatDate(member.created_at)),
      memberActions(member),
    ]),
  ).__raw;
}

function memberActions(member) {
  if (member.role === "owner") return `<span class="muted">${escapeHTML(t("members.ownerLocked"))}</span>`;
  return `<div class="row-actions">
    ${rowButton(t("members.changeRole"), "open-member-role", { "user-id": member.user_id })}
    ${canTransferActiveOrg() ? rowButton(t("members.transferShort"), "open-transfer-owner", { "user-id": member.user_id }, "danger") : ""}
    <button type="button" data-click="remove-org-member" data-user-id="${escapeHTML(member.user_id)}" class="danger small">${escapeHTML(t("common.remove"))}</button>
  </div>`;
}

function memberModals() {
  return [
    addMemberModal(),
    groupsModal(),
    roleModal(),
    transferModal(),
  ].map((item) => item.__raw || item || "").join("");
}

function addMemberModal() {
  return modal(state, "add-member", {
    title: t("members.addTitle"),
    subtitle: t("members.addSub"),
    body: `
      <form data-action="add-org-member" data-close-overlay="modal" class="modal-form">
        <div class="form-grid">
          <label class="field span-two"><span>${escapeHTML(t("members.email"))}</span><input name="email" autocomplete="off" placeholder="user@example.com" /></label>
          <label class="field"><span>${escapeHTML(t("members.userId"))}</span><input name="user_id" autocomplete="off" placeholder="${escapeHTML(t("members.userId"))}" /></label>
          <label class="field"><span>${escapeHTML(t("members.role"))}</span>${roleSelect("role", "member")}</label>
        </div>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${icon("plus").__raw}${escapeHTML(t("members.addButton"))}</button>
        </footer>
      </form>
    `,
  });
}

function groupsModal() {
  return modal(state, "member-groups", {
    title: t("members.groupsTitle"),
    subtitle: t("members.groupModalSub"),
    size: "wide",
    body: `
      <div class="member-group-strip modal-strip">
        ${state.groups.map((group) => `
          <span>
            <strong>${escapeHTML(group.name)}</strong>
            <small>${escapeHTML(group.is_default ? t("members.defaultGroup") : group.slug)}</small>
          </span>
        `).join("") || emptyState(t("members.emptyGroupsTitle"), t("members.emptyGroupsBody")).__raw}
      </div>
      <form data-action="create-group" data-close-overlay="modal" class="modal-form">
        <div class="form-grid">
          <label class="field span-two"><span>${escapeHTML(t("members.groupName"))}</span><input name="name" autocomplete="off" placeholder="${escapeHTML(t("members.groupName"))}" required /></label>
          <label class="field span-two"><span>${escapeHTML(t("members.groupSlug"))}</span><input name="slug" autocomplete="off" placeholder="${escapeHTML(t("members.groupSlug"))}" required /></label>
        </div>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${escapeHTML(t("members.addGroup"))}</button>
        </footer>
      </form>
    `,
  });
}

function roleModal() {
  const member = state.members.find((item) => item.user_id === state.ui.memberUserID);
  if (!member || member.role === "owner") return "";
  return modal(state, "member-role", {
    title: t("members.roleModalTitle"),
    subtitle: member.display_name || member.email,
    body: `
      <form data-action="update-org-member" data-user-id="${escapeHTML(member.user_id)}" data-close-overlay="modal" class="modal-form">
        <div class="member-user-summary">
          <strong>${escapeHTML(member.display_name || member.email)}</strong>
          <span>${escapeHTML(member.email)}</span>
          <small>${escapeHTML(t("members.tableJoined"))} ${escapeHTML(formatDate(member.created_at))}</small>
        </div>
        <label class="field"><span>${escapeHTML(t("members.role"))}</span>${roleSelect("role", member.role)}</label>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${escapeHTML(t("common.save"))}</button>
        </footer>
      </form>
    `,
  });
}

function transferModal() {
  if (!canTransferActiveOrg()) return "";
  const selected = state.members.find((item) => item.user_id === state.ui.memberTransferUserID && item.role !== "owner");
  const candidates = state.members.filter((item) => item.role !== "owner");
  return modal(state, "transfer-owner", {
    title: t("members.transferTitle"),
    subtitle: t("members.transferSub"),
    body: candidates.length ? `
      <form data-action="transfer-org-owner" data-close-overlay="modal" class="modal-form">
        ${selected ? `
          <input type="hidden" name="user_id" value="${escapeHTML(selected.user_id)}" />
          <div class="member-user-summary">
            <strong>${escapeHTML(selected.display_name || selected.email)}</strong>
            <span>${escapeHTML(selected.email)}</span>
            <small>${escapeHTML(t("members.selectedTransfer"))}</small>
          </div>
        ` : `
          <label class="field"><span>${escapeHTML(t("members.newOwner"))}</span>${memberSelect(candidates)}</label>
        `}
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary danger">${escapeHTML(t("members.transferButton"))}</button>
        </footer>
      </form>
    ` : emptyState(t("members.noTransferCandidates"), t("members.noTransferCandidatesBody")).__raw,
  });
}

function roleSelect(name, selected) {
  return `<select name="${escapeHTML(name)}" aria-label="${escapeHTML(t("members.role"))}">
    <option value="member" ${selected === "member" ? "selected" : ""}>${escapeHTML(t("roles.member"))}</option>
    <option value="admin" ${selected === "admin" ? "selected" : ""}>${escapeHTML(t("roles.admin"))}</option>
  </select>`;
}

function memberSelect(members) {
  return `<select name="user_id" aria-label="${escapeHTML(t("members.newOwner"))}" required>
    <option value="">${escapeHTML(t("members.newOwner"))}</option>
    ${members.map((member) => `<option value="${escapeHTML(member.user_id)}">${escapeHTML(member.display_name || member.email)} - ${escapeHTML(member.email)}</option>`).join("")}
  </select>`;
}

function formatDate(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(state.locale === "zh-CN" ? "zh-CN" : "en-US", { dateStyle: "medium", timeStyle: "short" });
}
