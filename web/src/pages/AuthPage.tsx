import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { KeyRound, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../api";
import { BrandMark, Field, Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { appDescription, appName, documentTitle, updateFavicon, type Branding } from "../lib/branding";
import { formSubmit, localizeError } from "../lib/forms";
import { useTheme } from "../theme";

export function AuthPage({ dingTalkEnabled, registrationEnabled, branding }: { dingTalkEnabled: boolean; registrationEnabled: boolean; branding?: Branding }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [error, setError] = useState("");
  const mutation = useMutation({
    mutationFn: (data: Record<string, string>) => mode === "login" ? api.login(data) : api.register(data),
    onSuccess: async () => {
      setError("");
      await queryClient.invalidateQueries();
    },
    onError: (err) => setError(localizeError(err, t)),
  });
  const name = appName(branding);
  const description = appDescription(branding);
  useEffect(() => {
    updateFavicon(branding);
    document.title = documentTitle(t("login"), branding);
  }, [branding, t]);

  return (
    <section className="auth-screen">
      <div className="brand-panel">
        <div className="auth-signal-map" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
        <div className="brand-row"><BrandMark branding={branding} /><span>{name}</span></div>
        <h1>{name}</h1>
        <p>{description}</p>
      </div>
      <div className="auth-card">
        <div className="auth-card-scan" aria-hidden="true" />
        <div className="auth-card-head">
          <Segmented value={theme} items={[["dark", t("themeDark")], ["light", t("themeLight")]]} onChange={(value) => setTheme(value as "light" | "dark")} />
          <Segmented value={locale} items={[["en", "EN"], ["zh-CN", t("languageChinese")]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          <span className="badge info">Auto</span>
        </div>
        <div className="tabs" role="tablist" aria-label="Auth mode">
          {registrationEnabled && <button type="button" role="tab" aria-selected={mode === "register"} className={clsx(mode === "register" && "active")} onClick={() => setMode("register")}>{t("register")}</button>}
          <button type="button" role="tab" aria-selected={mode === "login"} className={clsx(mode === "login" && "active")} onClick={() => setMode("login")}>{t("login")}</button>
        </div>
        <form className="stack" onSubmit={(event) => formSubmit(event, (data) => mutation.mutate(data))}>
          <Field label={t("commonEmail")} name="email" type={mode === "login" ? "text" : "email"} required />
          {mode === "register" && <Field label={t("displayName")} name="display_name" required />}
          <Field label={t("authPassword")} name="password" type="password" required />
          <button className="primary" type="submit" disabled={mutation.isPending}>
            {mode === "login" ? <KeyRound /> : <Plus />}
            {mode === "login" ? t("authSignIn") : t("authCreateAccount")}
          </button>
        </form>
        <div className="sso-zone">
          <span>DingTalk</span>
          {dingTalkEnabled ? <a className="button-link" href="/api/auth/dingtalk/start?redirect_after=/">{t("authProviderContinue")}</a> : <button type="button" className="ghost" disabled>{t("authProviderDisabled")}</button>}
        </div>
        {error && <div className="status error">{error}</div>}
      </div>
    </section>
  );
}
