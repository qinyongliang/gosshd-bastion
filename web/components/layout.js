import { activeOrg, state } from "../state.js";
import { optionText, t, tf } from "../i18n.js";
import { badge, escapeHTML, hudLine, icon, languageSwitch, raw, statusLine, themeSwitch } from "./html.js";
import { modal } from "./management.js";

const navItems = [
  ["dashboard", "nav.dashboard", "dashboard"],
  ["orgs", "nav.orgs", "org"],
  ["org-admin", "nav.members", "org"],
  ["keys", "nav.keys", "key"],
  ["targets", "nav.targets", "targets"],
  ["policies", "nav.policies", "policies"],
  ["audit", "nav.audit", "audit"],
];

export function renderShell(content) {
  const org = activeOrg();
  return raw(`
    <section class="console ${state.ui.sidebarOpen ? "sidebar-open" : ""}">
      <aside class="sidebar">
        <div class="brand-row"><div class="mark">g</div><span>gosshd</span><button type="button" class="mobile-sidebar-close icon-button" data-click="close-sidebar" aria-label="${escapeHTML(t("common.close"))}">${icon("close").__raw}</button></div>
        <div class="user-block">
          <strong>${escapeHTML(state.user.display_name || state.user.email)}</strong>
          <span>${escapeHTML(state.user.email)}</span>
          ${state.user.is_system_admin ? badge(t("shell.systemAdmin"), "info").__raw : ""}
        </div>
        <nav class="side-nav" aria-label="Primary">
          ${navItems.map(([route, label, iconName]) => navButton(route, label, iconName)).join("")}
          ${state.user.is_system_admin ? navButton("system-admin", "nav.admin", "settings") : ""}
        </nav>
        <div class="org-list">
          ${state.orgs.map((item) => `
            <button data-click="switch-org" data-id="${escapeHTML(item.id)}" class="${item.id === state.activeOrgID ? "active" : ""}">
              <span>${escapeHTML(item.name)}</span><small>${item.is_personal ? t("common.personal") : escapeHTML(optionText("roles", item.role || "member"))}</small>
            </button>
          `).join("")}
        </div>
        <button data-click="logout" class="ghost">${icon("logout").__raw}${escapeHTML(t("shell.signOut"))}</button>
      </aside>
      <button type="button" class="sidebar-scrim" data-click="close-sidebar" aria-label="${escapeHTML(t("common.close"))}"></button>
      <section class="workspace">
        <header class="topbar">
          <div>
            <p class="eyebrow">${escapeHTML(t("shell.eyebrow"))}</p>
            <h1>${escapeHTML(pageTitle())}</h1>
            <span class="context-line">${escapeHTML(org?.name || t("shell.noOrganization"))}</span>
          </div>
          <button type="button" class="mobile-menu-button icon-button" data-click="open-sidebar" aria-label="${escapeHTML(t("common.menu"))}">${icon("menu").__raw}</button>
          <div class="top-actions">
            ${themeSwitch(state.theme).__raw}
            ${languageSwitch(state.locale).__raw}
            <button type="button" data-click="open-modal" data-modal="personal-settings">${icon("settings").__raw}${escapeHTML(t("shell.personalSettings"))}</button>
            ${org?.is_personal ? badge(t("common.personal"), "info").__raw : badge(tf("shell.shared", { role: optionText("roles", org?.role || "member") }), "success").__raw}
            ${!org?.is_personal ? `<button data-click="invite">${escapeHTML(t("shell.invite"))}</button><button data-click="leave-org" class="danger">${escapeHTML(t("shell.leave"))}</button>` : ""}
          </div>
        </header>
        ${hudLine().__raw}
        ${statusLine(state).__raw || ""}
        ${state.invite ? invitePanel() : ""}
        ${content.__raw || content}
        ${personalModals()}
      </section>
    </section>
  `);
}

function navButton(route, label, iconName) {
  return `<button data-click="navigate" data-route="${route}" class="${state.route === route ? "active" : ""}">${icon(iconName).__raw}<span>${escapeHTML(t(label))}</span></button>`;
}

function pageTitle() {
  const labels = {
    dashboard: "page.dashboard",
    orgs: "page.orgs",
    "org-admin": "page.org-admin",
    keys: "page.keys",
    targets: "page.targets",
    policies: "page.policies",
    audit: "page.audit",
    "system-admin": "page.system-admin",
  };
  return t(labels[state.route] || "page.dashboard");
}

function invitePanel() {
  return `<div class="notice-card"><strong>${escapeHTML(t("shell.inviteCode"))}</strong><code>${escapeHTML(state.invite)}</code><button data-click="copy" aria-label="${escapeHTML(t("common.copy"))}" data-value="${escapeHTML(state.invite)}">${icon("copy").__raw}</button></div>`;
}

function personalModals() {
  return `${personalSettingsModal().__raw || ""}${changePasswordModal().__raw || ""}`;
}

function personalSettingsModal() {
  const isLocal = state.user.auth_provider === "local";
  return modal(state, "personal-settings", {
    title: t("profile.title"),
    subtitle: t("profile.subtitle"),
    body: `
      <div class="profile-summary">
        <span><strong>${escapeHTML(state.user.display_name || state.user.email)}</strong><small>${escapeHTML(state.user.email)}</small></span>
        <span><b>${escapeHTML(t("profile.loginSource"))}</b>${escapeHTML(optionText("providers", state.user.auth_provider || "local"))}</span>
      </div>
      <section class="section-block embedded">
        <header>
          <div>
            <h3>${escapeHTML(t("profile.securityTitle"))}</h3>
            <p>${escapeHTML(t("profile.securitySub"))}</p>
          </div>
        </header>
        ${isLocal
          ? `<button type="button" class="primary" data-click="open-modal" data-modal="change-password">${icon("key").__raw}${escapeHTML(t("profile.changePassword"))}</button>`
          : `<div class="notice-card compact"><strong>${escapeHTML(t("profile.externalPasswordTitle"))}</strong><span>${escapeHTML(t("profile.externalPasswordBody"))}</span></div>`}
      </section>
    `,
  });
}

function changePasswordModal() {
  if (state.user.auth_provider !== "local") return "";
  return modal(state, "change-password", {
    title: t("profile.changePassword"),
    subtitle: t("profile.changePasswordSub"),
    body: `
      <form data-action="change-own-password" data-close-overlay="modal" class="modal-form">
        <label class="field"><span>${escapeHTML(t("profile.currentPassword"))}</span><input name="current_password" type="password" autocomplete="current-password" required /></label>
        <label class="field"><span>${escapeHTML(t("profile.newPassword"))}</span><input name="new_password" type="password" autocomplete="new-password" required minlength="8" placeholder="${escapeHTML(t("profile.newPasswordPlaceholder"))}" /></label>
        <label class="field"><span>${escapeHTML(t("profile.confirmPassword"))}</span><input name="confirm_password" type="password" autocomplete="new-password" required minlength="8" /></label>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${icon("key").__raw}${escapeHTML(t("profile.savePassword"))}</button>
        </footer>
      </form>
    `,
  });
}
