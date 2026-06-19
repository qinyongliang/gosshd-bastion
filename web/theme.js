const STORAGE_KEY = "gosshd_theme";
const DEFAULT_THEME = "light";
const LIGHT_THEME = "light";

let currentTheme = resolveTheme();

export function getTheme() {
  return currentTheme;
}

export function getThemeStorageKey() {
  return STORAGE_KEY;
}

export function resolveTheme() {
  const stored = normalizeTheme(readStoredTheme());
  if (stored) return stored;
  return DEFAULT_THEME;
}

export function setTheme(theme, options = {}) {
  currentTheme = normalizeTheme(theme) || DEFAULT_THEME;
  if (options.persist !== false) writeStoredTheme(currentTheme);
  applyDocumentTheme();
  return currentTheme;
}

export function applyDocumentTheme() {
  document.documentElement.dataset.theme = currentTheme;
  document.documentElement.style.colorScheme = currentTheme === LIGHT_THEME ? "light" : "dark";
}

export function normalizeTheme(value) {
  const text = String(value || "").trim().toLowerCase();
  if (text === "light" || text === "white") return LIGHT_THEME;
  if (text === "dark" || text === "black") return "dark";
  return "";
}

function readStoredTheme() {
  try {
    return window.localStorage.getItem(STORAGE_KEY) || "";
  } catch {
    return "";
  }
}

function writeStoredTheme(theme) {
  try {
    window.localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // localStorage can be unavailable; the current DOM still reflects the selected theme.
  }
}
