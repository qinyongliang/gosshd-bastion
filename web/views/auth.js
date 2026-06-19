import { field } from "../components/forms.js";
import { html, icon, raw, statusLine } from "../components/html.js";

export function renderAuth(state) {
  const dingTalkEnabled = Boolean(state.providers?.dingtalk?.enabled);
  const mode = state.authMode === "register" ? "register" : "login";
  const isRegister = mode === "register";
  return html`
    <section class="auth-screen">
      <div class="brand-panel">
        <div class="brand-row"><div class="mark">g</div><span>gosshd bastion</span></div>
        <h1>AI-ready SSH access with command policy in the path.</h1>
        <p>Organizations, user groups, SSH aliases, agent enrollment, command security groups, audit, and MCP automation live together.</p>
      </div>
      <div class="auth-card">
        <div class="tabs" role="tablist" aria-label="Authentication mode">
          <button type="button" role="tab" aria-selected="${isRegister}" class="${isRegister ? "active" : ""}" data-click="auth-mode" data-mode="register">Register</button>
          <button type="button" role="tab" aria-selected="${!isRegister}" class="${!isRegister ? "active" : ""}" data-click="auth-mode" data-mode="login">Login</button>
        </div>
        ${
          isRegister
            ? raw(`<form data-action="register" class="stack">
                ${field("Email", "email", { type: "email", required: true, autocomplete: "email" }).__raw}
                ${field("Display name", "display_name", { required: true }).__raw}
                ${field("Password", "password", { type: "password", required: true, autocomplete: "new-password" }).__raw}
                <button class="primary" type="submit">${icon("spark").__raw}Create account</button>
              </form>`)
            : raw(`<form data-action="login" class="stack">
                ${field("Email", "email", { type: "text", required: true, autocomplete: "username" }).__raw}
                ${field("Password", "password", { type: "password", required: true, autocomplete: "current-password" }).__raw}
                <button class="primary" type="submit">${icon("key").__raw}Sign in</button>
              </form>`)
        }
        <div class="sso-zone">
          <span>DingTalk login</span>
          ${
            dingTalkEnabled
              ? raw(`<a class="button-link" href="/api/auth/dingtalk/start?redirect_after=/">${icon("spark").__raw}Continue with DingTalk</a>`)
              : raw(`<button type="button" class="ghost" disabled>DingTalk login is not configured</button>`)
          }
        </div>
        ${statusLine(state)}
      </div>
    </section>
  `;
}
