import { state } from "../state.js";
import { emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { cloudTable, modal, resourceHeader, rowButton, sectionBlock } from "../components/management.js";
import { optionText, t } from "../i18n.js";

export function renderOrgs() {
  return raw(`
    ${resourceHeader({
      title: t("orgs.listTitle"),
      subtitle: t("orgs.listSub"),
      actions: `
        <button type="button" class="primary" data-click="open-modal" data-modal="create-org">${icon("plus").__raw}${escapeHTML(t("orgs.createButton"))}</button>
        <button type="button" data-click="open-modal" data-modal="join-org">${escapeHTML(t("orgs.joinButton"))}</button>
      `,
      stats: [
        { label: t("orgs.total"), value: state.orgs.length },
        { label: t("orgs.sharedTotal"), value: state.orgs.filter((org) => !org.is_personal).length, tone: "success" },
        { label: t("orgs.personalTotal"), value: state.orgs.filter((org) => org.is_personal).length, tone: "info" },
      ],
    }).__raw}
    ${sectionBlock(t("orgs.listTitle"), t("orgs.listSub"), orgTable()).__raw}
    ${createOrgModal().__raw || ""}
    ${joinOrgModal().__raw || ""}
  `);
}

function orgTable() {
  if (!state.orgs.length) return emptyState(t("orgs.emptyTitle"), t("orgs.emptyBody")).__raw;
  return cloudTable(
    [t("orgs.tableName"), t("orgs.tableType"), t("orgs.tableRole"), t("management.operations")],
    state.orgs.map((org) => [
      `<strong>${escapeHTML(org.name)}</strong>`,
      escapeHTML(org.is_personal ? t("common.personal") : t("orgs.sharedType")),
      escapeHTML(optionText("roles", org.role || "member")),
      rowButton(t("orgs.switchButton"), "switch-org", { id: org.id }, state.activeOrgID === org.id ? "primary" : ""),
    ]),
  ).__raw;
}

function createOrgModal() {
  return modal(state, "create-org", {
    title: t("orgs.createTitle"),
    subtitle: t("orgs.createSub"),
    body: `
      <form data-action="create-org" data-close-overlay="modal" class="modal-form">
        <div class="form-grid">
          <label class="field span-two"><span>${escapeHTML(t("orgs.name"))}</span><input name="name" autocomplete="off" placeholder="${escapeHTML(t("orgs.name"))}" required /></label>
          <label class="field span-two"><span>${escapeHTML(t("orgs.slugLabel"))}</span><input name="slug" autocomplete="off" placeholder="${escapeHTML(t("orgs.slug"))}" required /></label>
        </div>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${escapeHTML(t("orgs.createButton"))}</button>
        </footer>
      </form>
    `,
  });
}

function joinOrgModal() {
  return modal(state, "join-org", {
    title: t("orgs.joinTitle"),
    subtitle: t("orgs.joinSub"),
    body: `
      <form data-action="join-org" data-close-overlay="modal" class="modal-form">
        <label class="field"><span>${escapeHTML(t("orgs.inviteCode"))}</span><input name="code" autocomplete="off" placeholder="${escapeHTML(t("orgs.inviteCode"))}" required /></label>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${escapeHTML(t("orgs.joinButton"))}</button>
        </footer>
      </form>
    `,
  });
}
