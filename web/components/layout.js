import { activeOrg, state } from "../state.js";
import { optionText, t, tf } from "../i18n.js";
import { badge, escapeHTML, hudLine, icon, languageSwitch, raw, statusLine, themeSwitch } from "./html.js";

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
    <section class="console">
      <aside class="sidebar">
        <div class="brand-row"><div class="mark">g</div><span>gosshd</span></div>
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
      <section class="workspace">
        <header class="topbar">
          <div>
            <p class="eyebrow">${escapeHTML(t("shell.eyebrow"))}</p>
            <h1>${escapeHTML(pageTitle())}</h1>
            <span class="context-line">${escapeHTML(org?.name || t("shell.noOrganization"))}</span>
          </div>
          <div class="top-actions">
            ${themeSwitch(state.theme).__raw}
            ${languageSwitch(state.locale).__raw}
            ${org?.is_personal ? badge(t("common.personal"), "info").__raw : badge(tf("shell.shared", { role: optionText("roles", org?.role || "member") }), "success").__raw}
            ${!org?.is_personal ? `<button data-click="invite">${escapeHTML(t("shell.invite"))}</button><button data-click="leave-org" class="danger">${escapeHTML(t("shell.leave"))}</button>` : ""}
          </div>
        </header>
        ${hudLine().__raw}
        ${statusLine(state).__raw || ""}
        ${state.invite ? invitePanel() : ""}
        ${content.__raw || content}
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
