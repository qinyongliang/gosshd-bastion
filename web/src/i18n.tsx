import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { ReactNode, useEffect, useMemo, useState } from "react";
import { I18nextProvider, initReactI18next, useTranslation } from "react-i18next";

export type Locale = "en" | "zh-CN";

const storageKey = "gosshd_locale";

const en = {
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
};

const zh = {
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
};

if (!i18next.isInitialized) {
  void i18next
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      resources: {
        en: { translation: en },
        zh: { translation: zh },
        "zh-CN": { translation: zh },
      },
      supportedLngs: ["en", "zh", "zh-CN"],
      fallbackLng: "zh-CN",
      nonExplicitSupportedLngs: true,
      detection: {
        order: ["localStorage", "navigator"],
        lookupLocalStorage: storageKey,
        caches: ["localStorage"],
      },
      interpolation: {
        escapeValue: false,
      },
      returnEmptyString: false,
    });
}

type I18nValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: keyof typeof en | string, fallback?: string) => string;
};

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => currentLocale());

  useEffect(() => {
    document.documentElement.lang = locale;
    const handleLanguageChanged = () => setLocaleState(currentLocale());
    i18next.on("languageChanged", handleLanguageChanged);
    return () => i18next.off("languageChanged", handleLanguageChanged);
  }, [locale]);

  return <I18nextProvider i18n={i18next}>{children}</I18nextProvider>;
}

export function useI18n() {
  const { t: translate, i18n } = useTranslation();
  const locale = currentLocale();
  return useMemo<I18nValue>(() => ({
    locale,
    setLocale(next) {
      window.localStorage.setItem(storageKey, next);
      document.documentElement.lang = next;
      void i18n.changeLanguage(next);
    },
    t(key, fallback = "") {
      return translate(key, { defaultValue: fallback || key });
    },
  }), [i18n, locale, translate]);
}

export function dateLocale(locale: Locale) {
  return locale === "zh-CN" ? "zh-CN" : "en-US";
}

function currentLocale(): Locale {
  return normalizeLocale(i18next.resolvedLanguage || i18next.language || window.localStorage.getItem(storageKey)) || "zh-CN";
}

function normalizeLocale(value: unknown): Locale | "" {
  const text = String(value || "").toLowerCase();
  if (text.startsWith("zh")) return "zh-CN";
  if (text.startsWith("en")) return "en";
  return "";
}
