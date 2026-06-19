import { state } from "../state.js";
import { commandLine, escapeHTML, icon, panel } from "../components/html.js";
import { t } from "../i18n.js";

export function renderAgents() {
  return panel(t("agents.title"), t("agents.sub"), `
    <form data-action="create-agent" class="stack">
      <input name="label" aria-label="${escapeHTML(t("agents.alias"))}" autocomplete="off" placeholder="${escapeHTML(t("agents.aliasPlaceholder"))}" required />
      <input name="default_host" aria-label="${escapeHTML(t("agents.host"))}" autocomplete="off" value="127.0.0.1" required />
      <input name="default_port" aria-label="${escapeHTML(t("agents.port"))}" type="number" value="22" required />
      <button type="submit">${icon("spark").__raw}${escapeHTML(t("agents.create"))}</button>
    </form>
    <div class="guide-block service">
      <strong>${escapeHTML(t("agents.startupTitle"))}</strong>
      <span>${escapeHTML(t("agents.startupBody"))}</span>
    </div>
    ${state.enrollment ? `
      <div class="guide-block">
        <strong>${escapeHTML(t("agents.runOnceTitle"))}</strong>
        <span>${escapeHTML(t("agents.runOnceBody"))}</span>
      </div>
      ${commandLine(t("agents.linuxShell"), state.enrollment.install_sh)}
      ${commandLine(t("agents.windowsPowerShell"), state.enrollment.install_ps1)}
      <div class="guide-block service">
        <strong>${escapeHTML(t("agents.installTitle"))}</strong>
        <span>${escapeHTML(t("agents.installBody"))}</span>
      </div>
      ${commandLine(t("agents.linuxService"), state.enrollment.service_sh)}
      ${commandLine(t("agents.windowsService"), state.enrollment.service_ps1)}
    ` : ""}
  `);
}
