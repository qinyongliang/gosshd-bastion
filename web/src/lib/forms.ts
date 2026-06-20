import type { FormEvent } from "react";
import { dateLocale } from "../i18n";
import type { Member } from "../types";

type Translate = (key: string, fallback?: string) => string;

export function formSubmit(event: FormEvent<HTMLFormElement>, next: (data: Record<string, string>) => void) {
  event.preventDefault();
  next(formValues(event.currentTarget));
}

export function formValues(form: HTMLFormElement) {
  const data: Record<string, string> = {};
  for (const [key, value] of new FormData(form).entries()) data[key] = String(value);
  for (const element of Array.from(form.elements)) {
    if (element instanceof HTMLInputElement && element.type === "checkbox") data[element.name] = element.checked ? "on" : "";
  }
  return data;
}

export function policyPayload(body: Record<string, string>): Record<string, unknown> {
  return {
    name: body.name,
    default_action: body.default_action || "deny",
    llm_config_id: body.llm_config_id || "",
    llm_prompt_id: body.llm_prompt_id || "",
    ip_allowlist: body.ip_allowlist || "",
    allow_interactive: body.allow_interactive === "on",
    allow_port_forward: body.allow_port_forward === "on",
    allow_upload: body.allow_upload === "on",
    allow_download: body.allow_download === "on",
  };
}

export function sortMembers(members: Member[], query: string, sort: "role" | "name" | "newest") {
  const filtered = members.filter((item) => [item.display_name, item.email, item.role].join(" ").toLowerCase().includes(query.toLowerCase()));
  return [...filtered].sort((a, b) => {
    if (sort === "newest") return String(b.created_at || "").localeCompare(String(a.created_at || ""));
    if (sort === "name") return (a.display_name || a.email).localeCompare(b.display_name || b.email);
    const weight = { owner: 0, admin: 1, member: 2 };
    return weight[a.role] - weight[b.role];
  });
}

export function roleText(role?: string, t?: Translate) {
  if (role === "owner") return t ? t("roleOwner") : "Owner";
  if (role === "admin") return t ? t("roleAdmin") : "Administrator";
  return t ? t("roleMember") : "Member";
}

export function formatDate(value?: string) {
  if (!value) return "";
  try {
    return new Intl.DateTimeFormat(dateLocale(document.documentElement.lang === "en" ? "en" : "zh-CN"), { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
  } catch {
    return value;
  }
}

export function pageTitle(t?: Translate) {
  const path = window.location.pathname.replace(/^\/+/, "") || "dashboard";
  const titles: Record<string, string> = {
    dashboard: "dashboard",
    orgs: "orgs",
    "org-admin": "members",
    keys: "keys",
    targets: "services",
    policies: "commandPolicy",
    audit: "auditPageTitle",
    "system-admin": "settings",
  };
  const key = titles[path] || "dashboard";
  return t ? t(key) : key;
}

export function localizeError(error: unknown, t: (key: string) => string) {
  const message = error instanceof Error ? error.message : String(error);
  if (message === "invalid credentials") return t("invalidCredentials");
  return message;
}
