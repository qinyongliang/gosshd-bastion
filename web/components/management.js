import { escapeHTML, icon, raw } from "./html.js";
import { t, tf } from "../i18n.js";

export function resourceHeader({ title, subtitle, stats = [], actions = "" }) {
  return raw(`
    <section class="resource-head">
      <div>
        <p class="eyebrow">${escapeHTML(t("management.console"))}</p>
        <h2>${escapeHTML(title)}</h2>
        <p>${escapeHTML(subtitle)}</p>
      </div>
      <div class="resource-actions">${actions}</div>
      ${stats.length ? `<div class="resource-stats">${stats.map((item) => statPill(item.label, item.value, item.tone)).join("")}</div>` : ""}
    </section>
  `);
}

export function resourceToolbar({ searchAction, query = "", searchPlaceholder = "", chips = "", actions = "" }) {
  return raw(`
    <section class="resource-toolbar" aria-label="${escapeHTML(t("management.toolbar"))}">
      <form data-action="${escapeHTML(searchAction)}" class="search-box">
        ${icon("search").__raw}
        <input name="query" value="${escapeHTML(query)}" autocomplete="off" placeholder="${escapeHTML(searchPlaceholder || t("management.search"))}" />
        <button type="submit">${escapeHTML(t("common.search"))}</button>
      </form>
      <div class="toolbar-chips">${chips}</div>
      <div class="toolbar-actions">${actions}</div>
    </section>
  `);
}

export function cloudTable(headers, rows, options = {}) {
  const empty = options.empty || "";
  const density = options.density || "comfortable";
  const body = rows.length
    ? rows.map((row) => `<tr>${row.map((cell) => `<td>${cell}</td>`).join("")}</tr>`).join("")
    : `<tr><td colspan="${headers.length}">${empty}</td></tr>`;
  return raw(`
    <div class="cloud-table ${escapeHTML(density)}">
      <table>
        <thead><tr>${headers.map((header) => `<th>${escapeHTML(header)}</th>`).join("")}</tr></thead>
        <tbody>${body}</tbody>
      </table>
    </div>
  `);
}

export function modal(state, name, { title, subtitle = "", body, size = "" }) {
  if (state.ui.modal !== name) return "";
  return raw(`
    <div class="overlay" data-click="close-overlays">
      <section class="modal ${escapeHTML(size)}" role="dialog" aria-modal="true" aria-label="${escapeHTML(title)}" onclick="event.stopPropagation()">
        <header class="surface-head">
          <div>
            <h2>${escapeHTML(title)}</h2>
            ${subtitle ? `<p>${escapeHTML(subtitle)}</p>` : ""}
          </div>
          <button type="button" class="icon-button" data-click="close-overlays" aria-label="${escapeHTML(t("common.close"))}">${icon("close").__raw}</button>
        </header>
        <div class="surface-body">${body}</div>
      </section>
    </div>
  `);
}

export function drawer(state, name, { title, subtitle = "", body, meta = "" }) {
  if (state.ui.drawer !== name) return "";
  return raw(`
    <div class="drawer-layer">
      <button type="button" class="drawer-scrim" data-click="close-overlays" aria-label="${escapeHTML(t("common.close"))}"></button>
      <aside class="drawer" aria-label="${escapeHTML(title)}">
        <header class="surface-head">
          <div>
            <h2>${escapeHTML(title)}</h2>
            ${subtitle ? `<p>${escapeHTML(subtitle)}</p>` : ""}
          </div>
          <button type="button" class="icon-button" data-click="close-overlays" aria-label="${escapeHTML(t("common.close"))}">${icon("close").__raw}</button>
        </header>
        ${meta ? `<div class="drawer-meta">${meta}</div>` : ""}
        <div class="surface-body">${body}</div>
      </aside>
    </div>
  `);
}

export function sectionBlock(title, subtitle, body) {
  return raw(`
    <section class="section-block">
      <header>
        <div>
          <h3>${escapeHTML(title)}</h3>
          <p>${escapeHTML(subtitle)}</p>
        </div>
      </header>
      ${body}
    </section>
  `);
}

export function detailList(items) {
  return raw(`<dl class="detail-list">${items.map(([label, value]) => `<div><dt>${escapeHTML(label)}</dt><dd>${value}</dd></div>`).join("")}</dl>`);
}

export function stepper(items, active = 0) {
  return raw(`<ol class="stepper">${items.map((item, index) => `
    <li class="${index === active ? "active" : ""}">
      <span>${index + 1}</span>
      <strong>${escapeHTML(item)}</strong>
    </li>
  `).join("")}</ol>`);
}

export function tabs(items) {
  return raw(`<div class="mode-tabs">${items.map((item) => `
    <button type="button" data-click="${escapeHTML(item.action)}" ${item.value ? `data-value="${escapeHTML(item.value)}"` : ""} class="${item.active ? "active" : ""}">
      ${escapeHTML(item.label)}
    </button>
  `).join("")}</div>`);
}

export function selectionSummary(count) {
  return count ? `<span class="selection-summary">${escapeHTML(tf("management.selected", { count }))}</span>` : `<span class="selection-summary muted">${escapeHTML(t("management.noSelection"))}</span>`;
}

export function rowButton(label, click, attrs = {}, tone = "") {
  const data = Object.entries(attrs)
    .map(([key, value]) => `data-${escapeHTML(key)}="${escapeHTML(value)}"`)
    .join(" ");
  return `<button type="button" class="small ${escapeHTML(tone)}" data-click="${escapeHTML(click)}" ${data}>${escapeHTML(label)}</button>`;
}

function statPill(label, value, tone = "neutral") {
  return `<span class="stat-pill ${escapeHTML(tone)}"><b>${escapeHTML(value)}</b>${escapeHTML(label)}</span>`;
}
