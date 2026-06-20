import { t } from "../i18n.js";

export function html(strings, ...values) {
  return strings.reduce((out, part, index) => out + part + escapeHTML(values[index] ?? ""), "");
}

export function raw(value) {
  return { __raw: String(value ?? "") };
}

export function escapeHTML(value) {
  if (value && value.__raw) return value.__raw;
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

export function statusLine(state) {
  if (state.error) return raw(`<div class="status error" aria-live="polite">${escapeHTML(state.error)}</div>`);
  if (state.notice) return raw(`<div class="status ok" aria-live="polite">${escapeHTML(state.notice)}</div>`);
  return "";
}

export function badge(text, tone = "neutral") {
  return raw(`<span class="badge ${tone}">${escapeHTML(text)}</span>`);
}

export function emptyState(title, body) {
  return raw(`
    <div class="empty-state">
      <div class="empty-orbit"></div>
      <strong>${escapeHTML(title)}</strong>
      <span>${escapeHTML(body)}</span>
    </div>
  `);
}

export function icon(name) {
  const paths = {
    agents: "M12 2v4m0 12v4M4.93 4.93l2.83 2.83m8.48 8.48 2.83 2.83M2 12h4m12 0h4M4.93 19.07l2.83-2.83m8.48-8.48 2.83-2.83",
    audit: "M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01",
    chevron: "m9 18 6-6-6-6",
    close: "M18 6 6 18M6 6l12 12",
    copy: "M8 8h10v12H8V8Zm-4 8V4h10",
    dashboard: "M3 13h8V3H3v10Zm10 8h8V3h-8v18ZM3 21h8v-6H3v6Z",
    key: "M21 2l-2 2m-7.5 7.5a5 5 0 1 1-7.07 7.07 5 5 0 0 1 7.07-7.07Zm0 0L15 8m0 0 2 2 4-4-2-2-4 4Z",
    logout: "M10 17l5-5-5-5M15 12H3M21 3v18",
    menu: "M4 6h16M4 12h16M4 18h16",
    org: "M16 11c1.66 0 3-1.57 3-3.5S17.66 4 16 4s-3 1.57-3 3.5S14.34 11 16 11ZM8 11c1.66 0 3-1.57 3-3.5S9.66 4 8 4 5 5.57 5 7.5 6.34 11 8 11Zm0 2c-2.67 0-5 1.34-5 3v2h10v-2c0-1.66-2.33-3-5-3Zm8 0c-.7 0-1.36.09-1.96.25 1.18.84 1.96 1.93 1.96 3.25V18h5v-2c0-1.66-2.33-3-5-3Z",
    plus: "M12 5v14M5 12h14",
    policies: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z",
    search: "M11 19a8 8 0 1 1 5.66-13.66A8 8 0 0 1 11 19Zm6-2 4 4",
    server: "M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v4H4V6Zm0 8h16v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-4Zm4-6h.01M8 16h.01",
    settings: "M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5ZM19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06A1.65 1.65 0 0 0 15 19.4a1.65 1.65 0 0 0-1 .6 1.65 1.65 0 0 0-.4 1.06V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 8.6 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-.6-1 1.65 1.65 0 0 0-1.06-.4H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 8.6a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-.6A1.65 1.65 0 0 0 10.4 3V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15.4 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.26.38.6.72 1 1 .3.2.68.4 1.06.4H21a2 2 0 1 1 0 4h-.09A1.65 1.65 0 0 0 19.4 15Z",
    spark: "M12 2l1.8 6.2L20 10l-6.2 1.8L12 18l-1.8-6.2L4 10l6.2-1.8L12 2Z",
    targets: "M4 4h16v6H4V4Zm0 10h16v6H4v-6Zm4-7h.01M8 17h.01",
  };
  return raw(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[name] || paths.spark}"/></svg>`);
}

export function table(headers, rows) {
  return `<div class="table-wrap"><table><thead><tr>${headers.map((h) => `<th>${escapeHTML(h)}</th>`).join("")}</tr></thead><tbody>${rows
    .map((row) => `<tr>${row.map((cell) => `<td>${cell}</td>`).join("")}</tr>`)
    .join("")}</tbody></table></div>`;
}

export function panel(title, subtitle, body, extraClass = "") {
  return raw(`<section class="panel ${escapeHTML(extraClass)}"><div class="panel-head"><div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(subtitle)}</p></div></div>${body}</section>`);
}

export function metric(label, value, iconName) {
  return raw(`<div class="metric">${icon(iconName).__raw}<span>${escapeHTML(label)}</span><strong>${escapeHTML(value)}</strong></div>`);
}

export function commandLine(label, value) {
  return `<div class="command-box">
    <span>${escapeHTML(label)}</span>
    <code>${escapeHTML(value || "")}</code>
    <button data-click="copy" aria-label="${escapeHTML(t("common.copy"))} ${escapeHTML(label)} command" data-value="${escapeHTML(value || "")}">${icon("copy").__raw}</button>
  </div>`;
}

export function languageSwitch(locale) {
  const current = locale || "en";
  return raw(`<div class="language-switch" role="group" aria-label="${escapeHTML(t("language.aria"))}">
    <button type="button" data-click="set-locale" data-locale="en" class="${current === "en" ? "active" : ""}">${escapeHTML(t("language.english"))}</button>
    <button type="button" data-click="set-locale" data-locale="zh-CN" class="${current === "zh-CN" ? "active" : ""}">${escapeHTML(t("language.chinese"))}</button>
  </div>`);
}

export function themeSwitch(theme) {
  const current = theme || "dark";
  return raw(`<div class="theme-switch" role="group" aria-label="${escapeHTML(t("theme.aria"))}">
    <button type="button" data-click="set-theme" data-theme="dark" class="${current === "dark" ? "active" : ""}">${escapeHTML(t("theme.dark"))}</button>
    <button type="button" data-click="set-theme" data-theme="light" class="${current === "light" ? "active" : ""}">${escapeHTML(t("theme.light"))}</button>
  </div>`);
}

export function hudLine() {
  return raw(`<div class="hud-line">
    <span class="hud-pill"><i class="hud-dot"></i>SSH ingress online</span>
    <span class="hud-pill">policy latency 38ms</span>
    <span class="hud-pill">LLM guard armed</span>
  </div>`);
}

export function accessSummaryGrid() {
  const rows = [
    ["dashboard.flowPublicKey", "dashboard.flowSSHLogin"],
    ["dashboard.flowBastion", "dashboard.flowPolicy"],
    ["dashboard.flowSSHService", "dashboard.flowEgress"],
    ["dashboard.flowAudit", "dashboard.flowAuditSub"],
  ];
  return raw(`<div class="access-summary-grid">${rows.map(([title, body], index) => `
    <section class="access-summary-card">
      <span>${String(index + 1).padStart(2, "0")}</span>
      <strong>${escapeHTML(t(title))}</strong>
      <small>${escapeHTML(t(body))}</small>
    </section>
  `).join("")}</div>`);
}

export function streamList() {
  const rows = [
    ["dashboard.streamIdentity", "dashboard.streamIdentityValue"],
    ["dashboard.streamRoute", "dashboard.streamRouteValue"],
    ["dashboard.streamPolicy", "dashboard.streamPolicyValue"],
    ["dashboard.streamLLM", "dashboard.streamLLMValue"],
    ["dashboard.streamAudit", "dashboard.streamAuditValue"],
  ];
  return raw(`<div class="stream-list">${rows.map(([label, value]) => `
    <span><b>${escapeHTML(t(label))}</b>${escapeHTML(t(value))}</span>
  `).join("")}</div>`);
}
