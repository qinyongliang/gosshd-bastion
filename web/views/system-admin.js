import { state } from "../state.js";
import { emptyState, escapeHTML, panel, raw, table } from "../components/html.js";
import { selectOptions } from "../components/forms.js";

export function renderSystemAdmin() {
  if (!state.user?.is_system_admin) {
    return panel("System admin", "System admin access is required.", emptyState("Forbidden", "Sign in as the default admin account or another system admin.").__raw);
  }
  const dingtalk = state.adminSettings.dingtalk || {};
  const ldap = state.adminSettings.ldap || {};
  return raw(`
    <div class="grid two">
      ${panel("Global settings", "Configure DingTalk login and LDAP settings for the whole system.", `
        <form data-action="admin-save-dingtalk" class="stack">
          <strong>DingTalk settings</strong>
          <label class="checkbox-row"><input type="checkbox" name="enabled" value="true" ${dingtalk.enabled ? "checked" : ""} /> Enabled</label>
          <input name="client_id" aria-label="DingTalk client id" autocomplete="off" value="${escapeHTML(dingtalk.client_id || "")}" placeholder="client id" />
          <input name="client_secret" aria-label="DingTalk client secret" autocomplete="off" placeholder="client secret" />
          <input name="auth_url" aria-label="DingTalk auth url" autocomplete="off" value="${escapeHTML(dingtalk.auth_url || "")}" placeholder="auth url" />
          <input name="token_url" aria-label="DingTalk token url" autocomplete="off" value="${escapeHTML(dingtalk.token_url || "")}" placeholder="token url" />
          <input name="userinfo_url" aria-label="DingTalk userinfo url" autocomplete="off" value="${escapeHTML(dingtalk.userinfo_url || "")}" placeholder="userinfo url" />
          <input name="redirect_url" aria-label="DingTalk redirect url" autocomplete="off" value="${escapeHTML(dingtalk.redirect_url || "")}" placeholder="redirect url" />
          ${selectOptions("default_org_id", "Default organization", state.adminOrgs, "name")}
          <select name="default_role" aria-label="Default role"><option value="member">member</option><option value="admin">admin</option></select>
          <button type="submit">Save DingTalk settings</button>
        </form>
        <form data-action="admin-save-ldap" class="stack compact">
          <strong>LDAP settings</strong>
          <label class="checkbox-row"><input type="checkbox" name="enabled" value="true" ${ldap.enabled ? "checked" : ""} /> Enabled</label>
          <input name="server_url" aria-label="LDAP server url" autocomplete="off" value="${escapeHTML(ldap.server_url || "")}" placeholder="ldap://ldap.example" />
          <input name="bind_dn" aria-label="LDAP bind dn" autocomplete="off" value="${escapeHTML(ldap.bind_dn || "")}" placeholder="bind dn" />
          <input name="bind_password" aria-label="LDAP bind password" autocomplete="off" placeholder="bind password" />
          <input name="base_dn" aria-label="LDAP base dn" autocomplete="off" value="${escapeHTML(ldap.base_dn || "")}" placeholder="base dn" />
          <input name="user_filter" aria-label="LDAP user filter" autocomplete="off" value="${escapeHTML(ldap.user_filter || "")}" placeholder="(uid={username})" />
          <input name="email_attr" aria-label="LDAP email attribute" autocomplete="off" value="${escapeHTML(ldap.email_attr || "")}" placeholder="mail" />
          <input name="name_attr" aria-label="LDAP name attribute" autocomplete="off" value="${escapeHTML(ldap.name_attr || "")}" placeholder="cn" />
          <button type="submit">Save LDAP settings</button>
        </form>
      `).__raw}
      ${panel("Account management", "Promote or demote system administrators.", userTable()).__raw}
    </div>
    ${panel("Organization management", "Inspect organizations and repair or transfer ownership.", orgManagement()).__raw}
  `);
}

function userTable() {
  if (!state.adminUsers.length) return emptyState("No users", "Users appear after registration or external login.").__raw;
  return table(["Email", "Provider", "System admin"], state.adminUsers.map((user) => [
    `<strong>${escapeHTML(user.display_name || user.email)}</strong><small>${escapeHTML(user.email)}</small>`,
    escapeHTML(user.auth_provider || "local"),
    `<form data-action="admin-update-user" data-user-id="${escapeHTML(user.id)}" class="row-form"><select name="is_system_admin" aria-label="System admin"><option value="false" ${!user.is_system_admin ? "selected" : ""}>user</option><option value="true" ${user.is_system_admin ? "selected" : ""}>admin</option></select><button type="submit">Save</button></form>`,
  ]));
}

function orgManagement() {
  return `
    <form data-action="admin-select-org" class="inline-form">
      ${selectOptions("org_id", "Organization", state.adminOrgs, "name")}
      <button type="submit">Load members</button>
    </form>
    ${state.adminMembers.length ? table(["User", "Role", "Set role", "Transfer"], state.adminMembers.map((member) => [
      `<strong>${escapeHTML(member.display_name || member.email)}</strong><small>${escapeHTML(member.email)}</small>`,
      escapeHTML(member.role),
      `<form data-action="admin-update-org-member" data-user-id="${escapeHTML(member.user_id)}" class="row-form"><select name="role" aria-label="Member role"><option value="member" ${member.role === "member" ? "selected" : ""}>member</option><option value="admin" ${member.role === "admin" ? "selected" : ""}>admin</option></select><button type="submit">Save</button></form>`,
      `<button data-click="admin-transfer-org-owner" data-user-id="${escapeHTML(member.user_id)}" class="danger small">Make owner</button>`,
    ])) : emptyState("No organization loaded", "Choose an organization to view members.").__raw}
  `;
}
