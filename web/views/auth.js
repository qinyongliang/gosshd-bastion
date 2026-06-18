import { field } from "../components/forms.js";
import { html, icon, raw, statusLine } from "../components/html.js";

export function renderAuth(state) {
  const dingTalkEnabled = Boolean(state.providers?.dingtalk?.enabled);
  return html`
    <section class="auth-screen">
      <div class="brand-panel">
        <div class="brand-row"><div class="mark">g</div><span>gosshd bastion</span></div>
        <h1>AI-ready SSH access with command policy in the path.</h1>
        <p>Organizations, user groups, SSH aliases, agent enrollment, command security groups, audit, and MCP automation live together.</p>
      </div>
      <div class="auth-card">
        <div class="tabs"><span>Register</span><span>Login</span></div>
        <form data-action="register" class="stack">
          ${field("Email", "email", { type: "email", required: true, autocomplete: "email" })}
          ${field("Display name", "display_name", { required: true })}
          ${field("Password", "password", { type: "password", required: true, autocomplete: "new-password" })}
          <button class="primary" type="submit">${icon("spark")}Create account</button>
        </form>
        <form data-action="login" class="stack compact">
          ${field("Email", "email", { type: "text", required: true, autocomplete: "username" })}
          ${field("Password", "password", { type: "password", required: true, autocomplete: "current-password" })}
          <button type="submit">${icon("key")}Sign in</button>
        </form>
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
