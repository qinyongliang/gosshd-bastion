import { field } from "../components/forms.js";
import { html, icon, languageSwitch, raw, statusLine, themeSwitch } from "../components/html.js";
import { t } from "../i18n.js";

export function renderAuth(state) {
  const dingTalkEnabled = Boolean(state.providers?.dingtalk?.enabled);
  const mode = state.authMode === "register" ? "register" : "login";
  const isRegister = mode === "register";
  return html`
    <section class="auth-screen">
      <div class="brand-panel">
        <div class="brand-row"><div class="mark">g</div><span>${t("auth.brand")}</span></div>
        <h1>${t("auth.title")}</h1>
        <p>${t("auth.subtitle")}</p>
      </div>
      <div class="auth-card">
        <div class="auth-card-head">
          ${themeSwitch(state.theme)}
          ${languageSwitch(state.locale)}
          <span class="badge info">${t("language.auto")}</span>
        </div>
        <div class="tabs" role="tablist" aria-label="${t("auth.modeAria")}">
          <button type="button" role="tab" aria-selected="${isRegister}" class="${isRegister ? "active" : ""}" data-click="auth-mode" data-mode="register">${t("auth.register")}</button>
          <button type="button" role="tab" aria-selected="${!isRegister}" class="${!isRegister ? "active" : ""}" data-click="auth-mode" data-mode="login">${t("auth.login")}</button>
        </div>
        ${
          isRegister
            ? raw(`<form data-action="register" class="stack">
                ${field(t("auth.email"), "email", { type: "email", required: true, autocomplete: "email" }).__raw}
                ${field(t("auth.displayName"), "display_name", { required: true }).__raw}
                ${field(t("auth.password"), "password", { type: "password", required: true, autocomplete: "new-password" }).__raw}
                <button class="primary" type="submit">${icon("spark").__raw}${t("auth.createAccount")}</button>
              </form>`)
            : raw(`<form data-action="login" class="stack">
                ${field(t("auth.email"), "email", { type: "text", required: true, autocomplete: "username" }).__raw}
                ${field(t("auth.password"), "password", { type: "password", required: true, autocomplete: "current-password" }).__raw}
                <button class="primary" type="submit">${icon("key").__raw}${t("auth.signIn")}</button>
              </form>`)
        }
        <div class="sso-zone">
          <span>${t("auth.dingTalk")}</span>
          ${
            dingTalkEnabled
              ? raw(`<a class="button-link" href="/api/auth/dingtalk/start?redirect_after=/">${icon("spark").__raw}${t("auth.dingTalkContinue")}</a>`)
              : raw(`<button type="button" class="ghost" disabled>${t("auth.dingTalkDisabled")}</button>`)
          }
        </div>
        ${statusLine(state)}
      </div>
    </section>
  `;
}
