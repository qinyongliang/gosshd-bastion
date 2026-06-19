import { state } from "../state.js";
import { escapeHTML, panel, raw, table } from "../components/html.js";
import { optionText, t } from "../i18n.js";

export function renderOrgs() {
  return raw(`
    <div class="grid two">
      ${panel(t("orgs.createTitle"), t("orgs.createSub"), `
        <form data-action="create-org" class="stack">
          <input name="name" aria-label="${escapeHTML(t("orgs.name"))}" autocomplete="off" placeholder="${escapeHTML(t("orgs.name"))}" required />
          <input name="slug" aria-label="${escapeHTML(t("orgs.slugLabel"))}" autocomplete="off" placeholder="${escapeHTML(t("orgs.slug"))}" required />
          <button type="submit">${escapeHTML(t("orgs.createButton"))}</button>
        </form>
      `).__raw}
      ${panel(t("orgs.joinTitle"), t("orgs.joinSub"), `
        <form data-action="join-org" class="stack">
          <input name="code" aria-label="${escapeHTML(t("orgs.inviteCode"))}" autocomplete="off" placeholder="${escapeHTML(t("orgs.inviteCode"))}" required />
          <button type="submit">${escapeHTML(t("orgs.joinButton"))}</button>
        </form>
      `).__raw}
    </div>
    ${panel(t("orgs.listTitle"), t("orgs.listSub"), table([t("orgs.tableName"), t("orgs.tableSlug"), t("orgs.tableRole")], state.orgs.map((org) => [
      `<strong>${escapeHTML(org.name)}</strong>${org.is_personal ? `<small>${escapeHTML(t("common.personal"))}</small>` : ""}`,
      escapeHTML(org.slug),
      escapeHTML(optionText("roles", org.role || "member")),
    ]))).__raw}
  `);
}
