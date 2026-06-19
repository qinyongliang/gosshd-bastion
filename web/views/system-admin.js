import { state } from "../state.js";
import { emptyState, escapeHTML, panel, raw, table } from "../components/html.js";
import { selectOptions } from "../components/forms.js";
import { optionText, t } from "../i18n.js";

export function renderSystemAdmin() {
  if (!state.user?.is_system_admin) {
    return panel(t("admin.title"), t("admin.required"), emptyState(t("admin.forbidden"), t("admin.forbiddenBody")).__raw);
  }
  const dingtalk = state.adminSettings.dingtalk || {};
  const ldap = state.adminSettings.ldap || {};
  return raw(`
    <div class="grid two">
      ${panel(t("admin.globalTitle"), t("admin.globalSub"), `
        <form data-action="admin-save-dingtalk" class="stack">
          <strong>${escapeHTML(t("admin.dingTalkSettings"))}</strong>
          <label class="checkbox-row"><input type="checkbox" name="enabled" value="true" ${dingtalk.enabled ? "checked" : ""} /> ${escapeHTML(t("common.enabled"))}</label>
          <input name="client_id" aria-label="${escapeHTML(t("admin.clientID"))}" autocomplete="off" value="${escapeHTML(dingtalk.client_id || "")}" placeholder="${escapeHTML(t("admin.clientIDPlaceholder"))}" />
          <input name="client_secret" aria-label="${escapeHTML(t("admin.clientSecret"))}" autocomplete="off" placeholder="${escapeHTML(t("admin.clientSecretPlaceholder"))}" />
          <input name="auth_url" aria-label="${escapeHTML(t("admin.authURL"))}" autocomplete="off" value="${escapeHTML(dingtalk.auth_url || "")}" placeholder="${escapeHTML(t("admin.authURLPlaceholder"))}" />
          <input name="token_url" aria-label="${escapeHTML(t("admin.tokenURL"))}" autocomplete="off" value="${escapeHTML(dingtalk.token_url || "")}" placeholder="${escapeHTML(t("admin.tokenURLPlaceholder"))}" />
          <input name="userinfo_url" aria-label="${escapeHTML(t("admin.userinfoURL"))}" autocomplete="off" value="${escapeHTML(dingtalk.userinfo_url || "")}" placeholder="${escapeHTML(t("admin.userinfoURLPlaceholder"))}" />
          <input name="redirect_url" aria-label="${escapeHTML(t("admin.redirectURL"))}" autocomplete="off" value="${escapeHTML(dingtalk.redirect_url || "")}" placeholder="${escapeHTML(t("admin.redirectURLPlaceholder"))}" />
          ${selectOptions("default_org_id", t("admin.defaultOrg"), state.adminOrgs, "name")}
          <select name="default_role" aria-label="${escapeHTML(t("admin.defaultRole"))}"><option value="member">${escapeHTML(t("roles.member"))}</option><option value="admin">${escapeHTML(t("roles.admin"))}</option></select>
          <button type="submit">${escapeHTML(t("admin.saveDingTalk"))}</button>
        </form>
        <form data-action="admin-save-ldap" class="stack compact">
          <strong>${escapeHTML(t("admin.ldapSettings"))}</strong>
          <label class="checkbox-row"><input type="checkbox" name="enabled" value="true" ${ldap.enabled ? "checked" : ""} /> ${escapeHTML(t("common.enabled"))}</label>
          <input name="server_url" aria-label="${escapeHTML(t("admin.ldapServer"))}" autocomplete="off" value="${escapeHTML(ldap.server_url || "")}" placeholder="${escapeHTML(t("admin.ldapServerPlaceholder"))}" />
          <input name="bind_dn" aria-label="${escapeHTML(t("admin.bindDN"))}" autocomplete="off" value="${escapeHTML(ldap.bind_dn || "")}" placeholder="${escapeHTML(t("admin.bindDNPlaceholder"))}" />
          <input name="bind_password" aria-label="${escapeHTML(t("admin.bindPassword"))}" autocomplete="off" placeholder="${escapeHTML(t("admin.bindPasswordPlaceholder"))}" />
          <input name="base_dn" aria-label="${escapeHTML(t("admin.baseDN"))}" autocomplete="off" value="${escapeHTML(ldap.base_dn || "")}" placeholder="${escapeHTML(t("admin.baseDNPlaceholder"))}" />
          <input name="user_filter" aria-label="${escapeHTML(t("admin.userFilter"))}" autocomplete="off" value="${escapeHTML(ldap.user_filter || "")}" placeholder="(uid={username})" />
          <input name="email_attr" aria-label="${escapeHTML(t("admin.emailAttr"))}" autocomplete="off" value="${escapeHTML(ldap.email_attr || "")}" placeholder="${escapeHTML(t("admin.emailAttrPlaceholder"))}" />
          <input name="name_attr" aria-label="${escapeHTML(t("admin.nameAttr"))}" autocomplete="off" value="${escapeHTML(ldap.name_attr || "")}" placeholder="${escapeHTML(t("admin.nameAttrPlaceholder"))}" />
          <button type="submit">${escapeHTML(t("admin.saveLDAP"))}</button>
        </form>
      `).__raw}
      ${panel(t("admin.accountTitle"), t("admin.accountSub"), userTable()).__raw}
    </div>
    ${panel(t("admin.orgTitle"), t("admin.orgSub"), orgManagement()).__raw}
  `);
}

function userTable() {
  if (!state.adminUsers.length) return emptyState(t("admin.noUsers"), t("admin.noUsersBody")).__raw;
  return table([t("admin.email"), t("admin.provider"), t("admin.systemAdmin")], state.adminUsers.map((user) => [
    `<strong>${escapeHTML(user.display_name || user.email)}</strong><small>${escapeHTML(user.email)}</small>`,
    escapeHTML(optionText("providers", user.auth_provider || "local")),
    `<form data-action="admin-update-user" data-user-id="${escapeHTML(user.id)}" class="row-form"><select name="is_system_admin" aria-label="${escapeHTML(t("admin.systemAdmin"))}"><option value="false" ${!user.is_system_admin ? "selected" : ""}>${escapeHTML(t("common.user"))}</option><option value="true" ${user.is_system_admin ? "selected" : ""}>${escapeHTML(t("common.admin"))}</option></select><button type="submit">${escapeHTML(t("common.save"))}</button></form>`,
  ]));
}

function orgManagement() {
  return `
    <form data-action="admin-select-org" class="inline-form">
      ${selectOptions("org_id", t("admin.organization"), state.adminOrgs, "name")}
      <button type="submit">${escapeHTML(t("admin.loadMembers"))}</button>
    </form>
    ${state.adminMembers.length ? table([t("admin.tableUser"), t("admin.tableRole"), t("admin.tableSetRole"), t("admin.tableTransfer")], state.adminMembers.map((member) => [
      `<strong>${escapeHTML(member.display_name || member.email)}</strong><small>${escapeHTML(member.email)}</small>`,
      escapeHTML(optionText("roles", member.role)),
      `<form data-action="admin-update-org-member" data-user-id="${escapeHTML(member.user_id)}" class="row-form"><select name="role" aria-label="${escapeHTML(t("members.role"))}"><option value="member" ${member.role === "member" ? "selected" : ""}>${escapeHTML(t("roles.member"))}</option><option value="admin" ${member.role === "admin" ? "selected" : ""}>${escapeHTML(t("roles.admin"))}</option></select><button type="submit">${escapeHTML(t("common.save"))}</button></form>`,
      `<button data-click="admin-transfer-org-owner" data-user-id="${escapeHTML(member.user_id)}" class="danger small">${escapeHTML(t("admin.makeOwner"))}</button>`,
    ])) : emptyState(t("admin.noOrgLoaded"), t("admin.noOrgLoadedBody")).__raw}
  `;
}
