import { createContext, ReactNode, useContext, useMemo, useState } from "react";

type Locale = "en" | "zh-CN";

const storageKey = "gosshd_locale";

const messages = {
  en: {
    add: "Add",
    admin: "System admin",
    audit: "Audit",
    cancel: "Cancel",
    close: "Close",
    commandPolicy: "Command policy",
    copied: "Copied",
    dashboard: "Dashboard",
    deny: "deny",
    details: "Details",
    keys: "Public keys",
    login: "Login",
    logout: "Sign out",
    members: "Members",
    orgs: "Organizations",
    privateNode: "Private node",
    register: "Register",
    save: "Save",
    search: "Search",
    services: "SSH services",
    settings: "System admin",
    invalidCredentials: "Email or password is incorrect. Please check and try again.",
  },
  "zh-CN": {
    add: "添加",
    admin: "系统管理员",
    audit: "审计",
    cancel: "取消",
    close: "关闭",
    commandPolicy: "命令安全组",
    copied: "已复制",
    dashboard: "控制台",
    deny: "拒绝",
    details: "详情",
    keys: "公钥",
    login: "登录",
    logout: "退出登录",
    members: "成员",
    orgs: "组织",
    privateNode: "私有节点",
    register: "注册",
    save: "保存",
    search: "搜索",
    services: "SSH 服务",
    settings: "系统管理",
    invalidCredentials: "邮箱或密码不正确，请检查后重新输入。",
  },
} satisfies Record<Locale, Record<string, string>>;

type I18nValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: keyof typeof messages.en | string, fallback?: string) => string;
};

const I18nContext = createContext<I18nValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, updateLocale] = useState<Locale>(() => resolveLocale());
  const value = useMemo<I18nValue>(() => ({
    locale,
    setLocale(next) {
      window.localStorage.setItem(storageKey, next);
      document.documentElement.lang = next;
      updateLocale(next);
    },
    t(key, fallback = "") {
      return messages[locale][key as keyof typeof messages.en] || messages["zh-CN"][key as keyof typeof messages.en] || fallback || key;
    },
  }), [locale]);

  document.documentElement.lang = locale;
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const context = useContext(I18nContext);
  if (!context) throw new Error("useI18n must be used within I18nProvider");
  return context;
}

export function dateLocale(locale: Locale) {
  return locale === "zh-CN" ? "zh-CN" : "en-US";
}

function resolveLocale(): Locale {
  const stored = normalizeLocale(window.localStorage.getItem(storageKey));
  if (stored) return stored;
  const languages = Array.isArray(window.navigator.languages) ? window.navigator.languages : [window.navigator.language];
  for (const item of languages) {
    const normalized = normalizeLocale(item);
    if (normalized) return normalized;
  }
  return "zh-CN";
}

function normalizeLocale(value: unknown): Locale | "" {
  const text = String(value || "").toLowerCase();
  if (text.startsWith("zh")) return "zh-CN";
  if (text.startsWith("en")) return "en";
  return "";
}
