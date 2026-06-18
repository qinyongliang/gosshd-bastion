import { activeOrg, state } from "../state.js";
import { badge, escapeHTML, icon, raw, statusLine } from "./html.js";

const navItems = [
  ["dashboard", "Dashboard", "dashboard"],
  ["orgs", "Organizations", "org"],
  ["org-admin", "Members", "org"],
  ["keys", "Public keys", "key"],
  ["targets", "SSH services", "targets"],
  ["agents", "Agent SSH", "agents"],
  ["policies", "Command policy", "policies"],
  ["audit", "Audit", "audit"],
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
          ${state.user.is_system_admin ? badge("System admin", "info").__raw : ""}
        </div>
        <nav class="side-nav" aria-label="Primary">
          ${navItems.map(([route, label, iconName]) => navButton(route, label, iconName)).join("")}
          ${state.user.is_system_admin ? navButton("system-admin", "System admin", "settings") : ""}
        </nav>
        <div class="org-list">
          ${state.orgs.map((item) => `
            <button data-click="switch-org" data-id="${escapeHTML(item.id)}" class="${item.id === state.activeOrgID ? "active" : ""}">
              <span>${escapeHTML(item.name)}</span><small>${item.is_personal ? "personal" : escapeHTML(item.role || "org")}</small>
            </button>
          `).join("")}
        </div>
        <button data-click="logout" class="ghost">${icon("logout").__raw}Sign out</button>
      </aside>
      <section class="workspace">
        <header class="topbar">
          <div>
            <p class="eyebrow">AI service bastion</p>
            <h1>${escapeHTML(pageTitle())}</h1>
            <span class="context-line">${escapeHTML(org?.name || "No organization selected")}</span>
          </div>
          <div class="top-actions">
            ${org?.is_personal ? badge("Personal", "info").__raw : badge(`Shared ${org?.role || "member"}`, "success").__raw}
            ${!org?.is_personal ? '<button data-click="invite">Invite</button><button data-click="leave-org" class="danger">Leave</button>' : ""}
          </div>
        </header>
        ${statusLine(state).__raw || ""}
        ${state.invite ? invitePanel() : ""}
        ${content.__raw || content}
      </section>
    </section>
  `);
}

function navButton(route, label, iconName) {
  return `<button data-click="navigate" data-route="${route}" class="${state.route === route ? "active" : ""}">${icon(iconName).__raw}<span>${escapeHTML(label)}</span></button>`;
}

function pageTitle() {
  const labels = {
    dashboard: "Control plane",
    orgs: "Organizations",
    "org-admin": "Organization members",
    keys: "Public keys",
    targets: "SSH services",
    agents: "Agent SSH enrollment",
    policies: "Command security groups",
    audit: "Command audit",
    "system-admin": "System administration",
  };
  return labels[state.route] || "Control plane";
}

function invitePanel() {
  return `<div class="notice-card"><strong>Invite code</strong><code>${escapeHTML(state.invite)}</code><button data-click="copy" aria-label="Copy invite code" data-value="${escapeHTML(state.invite)}">${icon("copy").__raw}</button></div>`;
}
