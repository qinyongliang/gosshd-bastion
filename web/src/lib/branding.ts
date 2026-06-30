import type { Runtime } from "../types";

export const DEFAULT_APP_NAME = "gosshd";
export const DEFAULT_APP_DESCRIPTION = "AI service bastion";

export type Branding = {
  app_name?: string;
  app_description?: string;
};

export function appName(branding?: Branding | Runtime) {
  return branding?.app_name?.trim() || DEFAULT_APP_NAME;
}

export function appDescription(branding?: Branding | Runtime) {
  return branding?.app_description?.trim() || DEFAULT_APP_DESCRIPTION;
}

export function documentTitle(title: string, branding?: Branding | Runtime) {
  const name = appName(branding);
  const cleanTitle = title.trim();
  return cleanTitle ? `${cleanTitle} · ${name}` : name;
}
