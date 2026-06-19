import { state } from "../state.js";
import { emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { modal, resourceHeader } from "../components/management.js";
import { t } from "../i18n.js";

export function renderKeys() {
  return raw(`
    ${resourceHeader({
      title: t("keys.title"),
      subtitle: t("keys.sub"),
      actions: `<button type="button" class="primary" data-click="open-modal" data-modal="create-key">${icon("key").__raw}${escapeHTML(t("keys.add"))}</button>`,
      stats: [
        { label: t("keys.total"), value: state.keys.length },
      ],
    }).__raw}
    <section class="panel key-panel">
      <header class="key-panel-head">
        <div>
          <h2>${escapeHTML(t("keys.listTitle"))}</h2>
          <p>${escapeHTML(t("keys.listSub"))}</p>
        </div>
      </header>
      ${state.keys.length ? keyList() : emptyState(t("keys.emptyTitle"), t("keys.emptyBody")).__raw}
    </section>
    ${createKeyModal().__raw || ""}
  `);
}

function createKeyModal() {
  return modal(state, "create-key", {
    title: t("keys.createTitle"),
    subtitle: t("keys.createSub"),
    body: `
      <form data-action="create-key" data-close-overlay="modal" class="modal-form key-form">
        <label class="field"><span>${escapeHTML(t("keys.name"))}</span><input name="name" autocomplete="off" placeholder="${escapeHTML(t("keys.namePlaceholder"))}" required /></label>
        <label class="field"><span>${escapeHTML(t("keys.authorized"))}</span><textarea name="authorized_key" autocomplete="off" spellcheck="false" placeholder="${escapeHTML(t("keys.authorizedPlaceholder"))}" required></textarea></label>
        <p class="field-help">${escapeHTML(t("keys.formHelp"))}</p>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${icon("key").__raw}${escapeHTML(t("keys.add"))}</button>
        </footer>
      </form>
    `,
  });
}

function keyList() {
  return `<div class="key-list">${state.keys.map((key) => `
    <article class="key-card">
      <div class="key-icon">${icon("key").__raw}</div>
      <div class="key-main">
        <div class="key-title-row">
          <strong>${escapeHTML(key.name)}</strong>
          <span>${escapeHTML(t("keys.sshKey"))}</span>
        </div>
        <code>${escapeHTML(key.fingerprint)}</code>
        <small>${escapeHTML(t("keys.addedAt"))} ${escapeHTML(formatDate(key.created_at))}</small>
      </div>
      <button data-click="delete-key" data-id="${escapeHTML(key.id)}" class="danger small">${escapeHTML(t("common.remove"))}</button>
    </article>
  `).join("")}</div>`;
}

function formatDate(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(state.locale === "zh-CN" ? "zh-CN" : "en-US", { dateStyle: "medium", timeStyle: "short" });
}
