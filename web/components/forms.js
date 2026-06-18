import { escapeHTML, raw } from "./html.js";

export function field(label, name, attrs = {}) {
  const tag = attrs.multiline ? "textarea" : "input";
  const value = attrs.value ?? "";
  const type = attrs.type || "text";
  const placeholder = attrs.placeholder || "";
  const required = attrs.required ? "required" : "";
  const autocomplete = attrs.autocomplete ? `autocomplete="${escapeHTML(attrs.autocomplete)}"` : "";
  const spellcheck = attrs.spellcheck === false ? `spellcheck="false"` : "";
  const control =
    tag === "textarea"
      ? `<textarea name="${escapeHTML(name)}" placeholder="${escapeHTML(placeholder)}" ${required} ${spellcheck}>${escapeHTML(value)}</textarea>`
      : `<input name="${escapeHTML(name)}" type="${escapeHTML(type)}" value="${escapeHTML(value)}" placeholder="${escapeHTML(placeholder)}" ${required} ${autocomplete} />`;
  return raw(`<label class="field"><span>${escapeHTML(label)}</span>${control}</label>`);
}

export function selectField(label, name, options, selected = "") {
  const choices = options
    .map((option) => {
      const value = typeof option === "string" ? option : option.value;
      const text = typeof option === "string" ? option : option.label;
      return `<option value="${escapeHTML(value)}" ${value === selected ? "selected" : ""}>${escapeHTML(text)}</option>`;
    })
    .join("");
  return raw(`<label class="field"><span>${escapeHTML(label)}</span><select name="${escapeHTML(name)}">${choices}</select></label>`);
}

export function selectOptions(name, label, items, textKey = "name") {
  return `<select name="${escapeHTML(name)}" aria-label="${escapeHTML(label)}"><option value="">${escapeHTML(label)}</option>${items
    .map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item[textKey])}</option>`)
    .join("")}</select>`;
}

export function formData(form) {
  return Object.fromEntries(new FormData(form).entries());
}
