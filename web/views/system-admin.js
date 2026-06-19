import { filteredAdminUsers, state } from "../state.js";
import { emptyState, escapeHTML, raw } from "../components/html.js";
import { selectOptions } from "../components/forms.js";
import { cloudTable, drawer, modal, resourceHeader, resourceToolbar, rowButton, sectionBlock } from "../components/management.js";
import { optionText, t } from "../i18n.js";

export function renderSystemAdmin() {
  if (!state.user?.is_system_admin) {
    return sectionBlock(t("admin.title"), t("admin.required"), emptyState(t("admin.forbidden"), t("admin.forbiddenBody")).__raw);
  }
  return raw(`
    ${resourceHeader({
      title: t("admin.title"),
      subtitle: t("admin.globalSub"),
      stats: [
        { label: t("admin.usersTotal"), value: state.adminUsers.length },
        { label: t("admin.adminsTotal"), value: state.adminUsers.filter((user) => user.is_system_admin).length, tone: "warning" },
        { label: t("admin.orgsTotal"), value: state.adminOrgs.length, tone: "success" },
      ],
    }).__raw}
    ${sectionBlock(t("admin.identityTitle"), t("admin.identitySub"), identityCards()).__raw}
    <div class="grid two">
      ${sectionBlock(t("admin.accountTitle"), t("admin.accountSub"), accountCard()).__raw}
      ${sectionBlock(t("admin.orgTitle"), t("admin.orgSub"), orgCard()).__raw}
    </div>
    ${adminModals()}
    ${orgDrawer().__raw || ""}
  `);
}

function accountCard() {
  const adminCount = state.adminUsers.filter((user) => user.is_system_admin).length;
  const visibleCount = filteredAdminUsers().length;
  return `
    <button type="button" class="admin-list-card" data-click="open-modal" data-modal="admin-users">
      <span>
        <strong>${escapeHTML(t("admin.openAccountList"))}</strong>
        <small>${escapeHTML(t("admin.accountCardSub"))}</small>
      </span>
      <span class="admin-card-stats">
        <b>${escapeHTML(state.adminUsers.length)}</b>${escapeHTML(t("admin.usersTotal"))}
        <b>${escapeHTML(adminCount)}</b>${escapeHTML(t("admin.adminsTotal"))}
        <b>${escapeHTML(visibleCount)}</b>${escapeHTML(t("admin.visibleUsers"))}
      </span>
    </button>
  `;
}

function orgCard() {
  return `
    <button type="button" class="admin-list-card" data-click="open-modal" data-modal="admin-orgs">
      <span>
        <strong>${escapeHTML(t("admin.openOrgList"))}</strong>
        <small>${escapeHTML(t("admin.orgCardSub"))}</small>
      </span>
      <span class="admin-card-stats">
        <b>${escapeHTML(state.adminOrgs.length)}</b>${escapeHTML(t("admin.orgsTotal"))}
        <b>${escapeHTML(state.adminOrgs.filter((org) => org.slug).length)}</b>${escapeHTML(t("admin.sluggedOrgs"))}
      </span>
    </button>
  `;
}

function identityCards() {
  const dingtalk = state.adminSettings.dingtalk || {};
  const ldap = state.adminSettings.ldap || {};
  return `
    <div class="identity-grid">
      <button type="button" data-click="open-modal" data-modal="admin-dingtalk">
        <strong>${escapeHTML(t("admin.dingTalkSettings"))}</strong>
        <span>${escapeHTML(dingtalk.enabled ? t("common.enabled") : t("common.disabled"))}</span>
      </button>
      <button type="button" data-click="open-modal" data-modal="admin-ldap">
        <strong>${escapeHTML(t("admin.ldapSettings"))}</strong>
        <span>${escapeHTML(ldap.enabled ? t("common.enabled") : t("common.disabled"))}</span>
      </button>
    </div>
  `;
}

function userTable() {
  const users = filteredAdminUsers();
  if (!users.length) return emptyState(t("admin.noUsers"), t("admin.noUsersBody")).__raw;
  return cloudTable([t("admin.email"), t("admin.provider"), t("admin.systemAdmin")], users.map((user) => [
    `<strong>${escapeHTML(user.display_name || user.email)}</strong><small>${escapeHTML(user.email)}</small>`,
    escapeHTML(optionText("providers", user.auth_provider || "local")),
    `<form data-action="admin-update-user" data-user-id="${escapeHTML(user.id)}" class="row-form"><select name="is_system_admin" aria-label="${escapeHTML(t("admin.systemAdmin"))}"><option value="false" ${!user.is_system_admin ? "selected" : ""}>${escapeHTML(t("common.user"))}</option><option value="true" ${user.is_system_admin ? "selected" : ""}>${escapeHTML(t("common.admin"))}</option></select><button type="submit">${escapeHTML(t("common.save"))}</button></form>`,
  ])).__raw;
}

function orgTable() {
  if (!state.adminOrgs.length) return emptyState(t("admin.noOrgLoaded"), t("admin.noOrgLoadedBody")).__raw;
  return cloudTable([t("admin.organization"), t("orgs.tableSlug"), t("management.operations")], state.adminOrgs.map((org) => [
    `<strong>${escapeHTML(org.name)}</strong><small>${escapeHTML(org.id)}</small>`,
    escapeHTML(org.slug || "-"),
    rowButton(t("admin.openOrg"), "open-admin-org", { "org-id": org.id }),
  ])).__raw;
}

function adminModals() {
  return `${usersModal().__raw || ""}${orgsModal().__raw || ""}${dingtalkModal().__raw || ""}${ldapModal().__raw || ""}`;
}

function usersModal() {
  return modal(state, "admin-users", {
    title: t("admin.accountTitle"),
    subtitle: t("admin.accountModalSub"),
    size: "wide",
    body: `
      ${resourceToolbar({
        searchAction: "set-admin-user-filter",
        query: state.adminUserQuery,
        searchPlaceholder: t("admin.accountSearch"),
        actions: `<button type="button" data-click="clear-admin-user-filter">${escapeHTML(t("targets.clearFilters"))}</button>`,
      }).__raw}
      ${userTable()}
    `,
  });
}

function orgsModal() {
  return modal(state, "admin-orgs", {
    title: t("admin.orgTitle"),
    subtitle: t("admin.orgModalSub"),
    size: "wide",
    body: orgTable(),
  });
}

function dingtalkModal() {
  const dingtalk = state.adminSettings.dingtalk || {};
  return modal(state, "admin-dingtalk", {
    title: t("admin.dingtalkModalTitle"),
    subtitle: t("admin.globalSub"),
    size: "wide",
    body: `
      <form data-action="admin-save-dingtalk" data-close-overlay="modal" class="modal-form">
        <label class="checkbox-row"><input type="checkbox" name="enabled" value="true" ${dingtalk.enabled ? "checked" : ""} /> ${escapeHTML(t("common.enabled"))}</label>
        <div class="form-grid">
          <label class="field"><span>${escapeHTML(t("admin.clientID"))}</span><input name="client_id" autocomplete="off" value="${escapeHTML(dingtalk.client_id || "")}" placeholder="${escapeHTML(t("admin.clientIDPlaceholder"))}" /></label>
          <label class="field"><span>${escapeHTML(t("admin.clientSecret"))}</span><input name="client_secret" autocomplete="off" placeholder="${escapeHTML(t("admin.clientSecretPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.authURL"))}</span><input name="auth_url" autocomplete="off" value="${escapeHTML(dingtalk.auth_url || "")}" placeholder="${escapeHTML(t("admin.authURLPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.tokenURL"))}</span><input name="token_url" autocomplete="off" value="${escapeHTML(dingtalk.token_url || "")}" placeholder="${escapeHTML(t("admin.tokenURLPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.userinfoURL"))}</span><input name="userinfo_url" autocomplete="off" value="${escapeHTML(dingtalk.userinfo_url || "")}" placeholder="${escapeHTML(t("admin.userinfoURLPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.redirectURL"))}</span><input name="redirect_url" autocomplete="off" value="${escapeHTML(dingtalk.redirect_url || "")}" placeholder="${escapeHTML(t("admin.redirectURLPlaceholder"))}" /></label>
          <label class="field"><span>${escapeHTML(t("admin.defaultOrg"))}</span>${selectOptions("default_org_id", t("admin.defaultOrg"), state.adminOrgs, "name")}</label>
          <label class="field"><span>${escapeHTML(t("admin.defaultRole"))}</span><select name="default_role"><option value="member">${escapeHTML(t("roles.member"))}</option><option value="admin">${escapeHTML(t("roles.admin"))}</option></select></label>
        </div>
        <footer class="modal-actions"><button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button><button type="submit" class="primary">${escapeHTML(t("admin.saveDingTalk"))}</button></footer>
      </form>
    `,
  });
}

function ldapModal() {
  const ldap = state.adminSettings.ldap || {};
  return modal(state, "admin-ldap", {
    title: t("admin.ldapModalTitle"),
    subtitle: t("admin.globalSub"),
    size: "wide",
    body: `
      <form data-action="admin-save-ldap" data-close-overlay="modal" class="modal-form">
        <label class="checkbox-row"><input type="checkbox" name="enabled" value="true" ${ldap.enabled ? "checked" : ""} /> ${escapeHTML(t("common.enabled"))}</label>
        <div class="form-grid">
          <label class="field span-two"><span>${escapeHTML(t("admin.ldapServer"))}</span><input name="server_url" autocomplete="off" value="${escapeHTML(ldap.server_url || "")}" placeholder="${escapeHTML(t("admin.ldapServerPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.bindDN"))}</span><input name="bind_dn" autocomplete="off" value="${escapeHTML(ldap.bind_dn || "")}" placeholder="${escapeHTML(t("admin.bindDNPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.bindPassword"))}</span><input name="bind_password" autocomplete="off" placeholder="${escapeHTML(t("admin.bindPasswordPlaceholder"))}" /></label>
          <label class="field span-two"><span>${escapeHTML(t("admin.baseDN"))}</span><input name="base_dn" autocomplete="off" value="${escapeHTML(ldap.base_dn || "")}" placeholder="${escapeHTML(t("admin.baseDNPlaceholder"))}" /></label>
          <label class="field"><span>${escapeHTML(t("admin.userFilter"))}</span><input name="user_filter" autocomplete="off" value="${escapeHTML(ldap.user_filter || "")}" placeholder="(uid={username})" /></label>
          <label class="field"><span>${escapeHTML(t("admin.emailAttr"))}</span><input name="email_attr" autocomplete="off" value="${escapeHTML(ldap.email_attr || "")}" placeholder="${escapeHTML(t("admin.emailAttrPlaceholder"))}" /></label>
          <label class="field"><span>${escapeHTML(t("admin.nameAttr"))}</span><input name="name_attr" autocomplete="off" value="${escapeHTML(ldap.name_attr || "")}" placeholder="${escapeHTML(t("admin.nameAttrPlaceholder"))}" /></label>
        </div>
        <footer class="modal-actions"><button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button><button type="submit" class="primary">${escapeHTML(t("admin.saveLDAP"))}</button></footer>
      </form>
    `,
  });
}

function orgDrawer() {
  const org = state.adminOrgs.find((item) => item.id === state.selectedAdminOrgID);
  if (!org) return "";
  return drawer(state, "admin-org", {
    title: org.name,
    subtitle: t("admin.orgDrawerTitle"),
    body: state.adminMembers.length ? cloudTable([t("admin.tableUser"), t("admin.tableRole"), t("admin.tableSetRole"), t("admin.tableTransfer")], state.adminMembers.map((member) => [
      `<strong>${escapeHTML(member.display_name || member.email)}</strong><small>${escapeHTML(member.email)}</small>`,
      escapeHTML(optionText("roles", member.role)),
      `<form data-action="admin-update-org-member" data-user-id="${escapeHTML(member.user_id)}" class="row-form"><select name="role" aria-label="${escapeHTML(t("members.role"))}"><option value="member" ${member.role === "member" ? "selected" : ""}>${escapeHTML(t("roles.member"))}</option><option value="admin" ${member.role === "admin" ? "selected" : ""}>${escapeHTML(t("roles.admin"))}</option></select><button type="submit">${escapeHTML(t("common.save"))}</button></form>`,
      `<button data-click="admin-transfer-org-owner" data-user-id="${escapeHTML(member.user_id)}" class="danger small">${escapeHTML(t("admin.makeOwner"))}</button>`,
    ])).__raw : emptyState(t("admin.noOrgLoaded"), t("admin.noOrgLoadedBody")).__raw,
  });
}
