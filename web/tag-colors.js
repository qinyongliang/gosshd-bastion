import { escapeHTML } from "./components/html.js";

export const TAG_COLORS = ["gray", "red", "orange", "yellow", "green", "blue", "purple"];

export function normalizeTagColor(color) {
  const value = String(color || "").trim().toLowerCase();
  return TAG_COLORS.includes(value) ? value : "";
}

export function fallbackTagColor(name) {
  const text = String(name || "");
  let hash = 2166136261;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return TAG_COLORS[Math.abs(hash) % TAG_COLORS.length];
}

export function tagColorClass(color, name) {
  return `tag-color-${normalizeTagColor(color) || fallbackTagColor(name)}`;
}

export function tagChip(name, color) {
  return `<span class="tag-chip ${tagColorClass(color, name)}" data-tag="${escapeHTML(name)}"><i></i>${escapeHTML(name)}</span>`;
}

export function tagFilterButton(name, color, active) {
  return `<button type="button" data-click="toggle-target-tag" data-tag="${escapeHTML(name)}" class="tag-chip ${tagColorClass(color, name)} ${active ? "active" : ""}"><i></i>${escapeHTML(name)}</button>`;
}

