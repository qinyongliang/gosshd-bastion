import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { KeyRound, Plus } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { Field, Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { formSubmit, localizeError } from "../lib/forms";
import { useTheme } from "../theme";

export function AuthPage({ dingTalkEnabled }: { dingTalkEnabled: boolean }) {
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

  return (
    <section className="auth-screen">
      <div className="brand-panel">
        <div className="brand-row"><div className="mark">g</div><span>gosshd</span></div>
        <h1>AI 服务堡垒机</h1>
        <p>为自动化任务和运维人员提供 SSH 别名访问、命令安全组和完整审计。</p>
      </div>
      <div className="auth-card">
        <div className="auth-card-head">
          <Segmented value={theme} items={[["dark", "黑", "Black"], ["light", "白", "White"]]} onChange={(value) => setTheme(value as "light" | "dark")} />
          <Segmented value={locale} items={[["en", "EN", "EN"], ["zh-CN", "中文", "中文"]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          <span className="badge info">Auto</span>
        </div>
        <div className="tabs" role="tablist" aria-label="Auth mode">
          <button type="button" role="tab" aria-selected={mode === "register"} className={clsx(mode === "register" && "active")} onClick={() => setMode("register")}>{t("register")}</button>
          <button type="button" role="tab" aria-selected={mode === "login"} className={clsx(mode === "login" && "active")} onClick={() => setMode("login")}>{t("login")}</button>
        </div>
        <form className="stack" onSubmit={(event) => formSubmit(event, (data) => mutation.mutate(data))}>
          <Field label="Email" name="email" type={mode === "login" ? "text" : "email"} required />
          {mode === "register" && <Field label="Display name" name="display_name" required />}
          <Field label="Password" name="password" type="password" required />
          <button className="primary" type="submit" disabled={mutation.isPending}>
            {mode === "login" ? <KeyRound /> : <Plus />}
            {mode === "login" ? "Sign in" : "Create account"}
          </button>
        </form>
        <div className="sso-zone">
          <span>DingTalk</span>
          {dingTalkEnabled ? <a className="button-link" href="/api/auth/dingtalk/start?redirect_after=/">Continue</a> : <button type="button" className="ghost" disabled>Disabled</button>}
        </div>
        {error && <div className="status error">{error}</div>}
      </div>
    </section>
  );
}
