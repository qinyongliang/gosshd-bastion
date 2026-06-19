import { state } from "../state.js";
import { emptyState, escapeHTML, icon, panel, raw, table } from "../components/html.js";
import { t } from "../i18n.js";

export function renderKeys() {
  return panel(t("keys.title"), t("keys.sub"), `
    <form data-action="create-key" class="stack">
      <input name="name" aria-label="${escapeHTML(t("keys.name"))}" autocomplete="off" placeholder="${escapeHTML(t("keys.namePlaceholder"))}" required />
      <textarea name="authorized_key" aria-label="${escapeHTML(t("keys.authorized"))}" autocomplete="off" spellcheck="false" placeholder="${escapeHTML(t("keys.authorizedPlaceholder"))}" required></textarea>
      <button type="submit">${icon("key").__raw}${escapeHTML(t("keys.add"))}</button>
    </form>
    ${state.keys.length ? table([t("keys.tableName"), t("keys.fingerprint"), ""], state.keys.map((key) => [
      escapeHTML(key.name),
      escapeHTML(key.fingerprint),
      `<button data-click="delete-key" data-id="${escapeHTML(key.id)}" class="danger small">${escapeHTML(t("common.remove"))}</button>`,
    ])) : emptyState(t("keys.emptyTitle"), t("keys.emptyBody")).__raw}
  `);
}
