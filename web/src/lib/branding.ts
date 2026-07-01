import type { Runtime } from "../types";
import defaultAppIcon from "../assets/system-icon.png";

export const DEFAULT_APP_NAME = "gosshd";
export const DEFAULT_APP_DESCRIPTION = "AI service bastion";
export const DEFAULT_APP_ICON = defaultAppIcon;

export type Branding = {
  app_name?: string;
  app_description?: string;
  app_icon?: string;
};

export function appName(branding?: Branding | Runtime) {
  return branding?.app_name?.trim() || DEFAULT_APP_NAME;
}

export function appDescription(branding?: Branding | Runtime) {
  return branding?.app_description?.trim() || DEFAULT_APP_DESCRIPTION;
}

export function appIcon(branding?: Branding | Runtime) {
  return branding?.app_icon?.trim() || DEFAULT_APP_ICON;
}

export function documentTitle(title: string, branding?: Branding | Runtime) {
  const name = appName(branding);
  const cleanTitle = title.trim();
  return cleanTitle ? `${cleanTitle} · ${name}` : name;
}

export function updateFavicon(branding?: Branding | Runtime) {
  const href = appIcon(branding);
  let link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
  if (!link) {
    link = document.createElement("link");
    link.rel = "icon";
    document.head.appendChild(link);
  }
  link.href = href;
}
