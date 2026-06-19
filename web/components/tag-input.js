import { escapeHTML, icon, raw } from "./html.js";
import { allTargetTagDetails, tagColorForName } from "../state.js";
import { t } from "../i18n.js";
import { tagChip, tagColorClass } from "../tag-colors.js";

export function tagInput({ label, name = "tags", value = "", placeholder = "", className = "" }) {
  const tags = selectedTags(value);
  const knownTags = allTargetTagDetails();
  return raw(`
    <div class="field tag-field ${escapeHTML(className)}">
      <span>${escapeHTML(label)}</span>
      <div class="tag-input" data-tag-input>
        <input type="hidden" data-tag-hidden name="${escapeHTML(name)}" value="${escapeHTML(tags.join(", "))}" />
        <div class="tag-input-box" data-tag-box>
          <div class="tag-input-values" data-tag-values>${tags.map((tag) => tagInputChip(tag)).join("")}</div>
          <input class="tag-input-text" data-tag-input-text aria-label="${escapeHTML(label)}" autocomplete="off" placeholder="${escapeHTML(placeholder)}" />
        </div>
        <div class="tag-input-menu" data-tag-menu role="listbox">
          ${knownTags.map((tag) => tagSuggestion(tag.name, tag.color)).join("")}
          <button type="button" class="tag-suggestion create" data-click="tag-input-create" data-tag-create-option hidden>
            ${icon("plus").__raw}<span>${escapeHTML(t("targets.createTag"))}</span><b data-tag-create-label></b>
          </button>
        </div>
      </div>
    </div>
  `);
}

export function tagInputChip(tag) {
  const color = tagColorForName(tag);
  return `
    <span class="tag-chip tag-input-chip ${tagColorClass(color, tag)}" data-tag-chip="${escapeHTML(tag)}">
      <i></i>
      <span>${escapeHTML(tag)}</span>
      <button type="button" class="tag-remove" data-click="tag-input-remove" data-tag="${escapeHTML(tag)}" aria-label="${escapeHTML(`${t("targets.removeTag")} ${tag}`)}">${icon("close").__raw}</button>
    </span>
  `;
}

export function selectedTags(value) {
  const rawTags = Array.isArray(value) ? value : String(value || "").split(",");
  const seen = new Set();
  return rawTags
    .map((tag) => String(tag || "").trim())
    .filter((tag) => {
      if (!tag || seen.has(tag)) return false;
      seen.add(tag);
      return true;
    });
}

function tagSuggestion(name, color) {
  return `<button type="button" class="tag-suggestion" data-click="tag-input-select" data-tag="${escapeHTML(name)}" role="option">${tagChip(name, color)}</button>`;
}

