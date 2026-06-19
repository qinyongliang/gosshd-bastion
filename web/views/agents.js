import { state } from "../state.js";
import { commandLine, emptyState, escapeHTML, icon, raw } from "../components/html.js";
import { drawer, modal, resourceHeader, sectionBlock, tabs } from "../components/management.js";
import { t } from "../i18n.js";

export function renderAgents() {
  return raw(`
    ${resourceHeader({
      title: t("agents.title"),
      subtitle: t("agents.sub"),
      actions: `<button type="button" class="primary" data-click="open-modal" data-modal="create-agent">${icon("spark").__raw}${escapeHTML(t("agents.enrollButton"))}</button>`,
      stats: [
        { label: t("agents.platformLinux"), value: "sh", tone: "success" },
        { label: t("agents.platformWindows"), value: "sc", tone: "info" },
        { label: t("management.lifecycle"), value: "system", tone: "warning" },
      ],
    }).__raw}
    <section class="resource-guide">
      <div>
        <h3>${escapeHTML(t("agents.startupTitle"))}</h3>
        <p>${escapeHTML(t("agents.startupBody"))}</p>
      </div>
      <div class="guide-grid">
        <span><b>Linux</b>${escapeHTML(t("agents.linuxService"))}</span>
        <span><b>Windows</b>${escapeHTML(t("agents.windowsService"))}</span>
        <span><b>Target</b>${escapeHTML(t("targets.addSub"))}</span>
      </div>
    </section>
    ${state.enrollment ? "" : emptyState(t("agents.generatedTitle"), t("agents.generatedSub")).__raw}
    ${createAgentModal().__raw || ""}
    ${enrollmentDrawer().__raw || ""}
  `);
}

function createAgentModal() {
  return modal(state, "create-agent", {
    title: t("agents.createModalTitle"),
    subtitle: t("agents.createModalSub"),
    body: `
      <form data-action="create-agent" class="modal-form">
        <div class="form-grid single">
          <label class="field"><span>${escapeHTML(t("agents.alias"))}</span><input name="label" autocomplete="off" placeholder="${escapeHTML(t("agents.aliasPlaceholder"))}" required /></label>
        </div>
        <footer class="modal-actions">
          <button type="button" data-click="close-overlays">${escapeHTML(t("common.cancel"))}</button>
          <button type="submit" class="primary">${icon("spark").__raw}${escapeHTML(t("agents.create"))}</button>
        </footer>
      </form>
    `,
  });
}

function enrollmentDrawer() {
  if (!state.enrollment) return "";
  const isWindows = state.ui.agentPlatform === "windows";
  return drawer(state, "agent-enrollment", {
    title: t("agents.generatedTitle"),
    subtitle: t("agents.generatedSub"),
    body: `
      ${sectionBlock(t("agents.guideTitle"), t("agents.guideSub"), tabs([
        { label: t("agents.platformLinux"), action: "set-agent-platform", value: "linux", active: !isWindows },
        { label: t("agents.platformWindows"), action: "set-agent-platform", value: "windows", active: isWindows },
      ]).__raw).__raw}
      ${isWindows ? windowsCommands() : linuxCommands()}
    `,
  });
}

function linuxCommands() {
  return `
    ${sectionBlock(t("agents.runOnceTitle"), t("agents.runOnceBody"), commandLine(t("agents.linuxShell"), state.enrollment.install_sh)).__raw}
    ${sectionBlock(t("agents.installTitle"), t("agents.installBody"), commandLine(t("agents.linuxService"), state.enrollment.service_sh)).__raw}
  `;
}

function windowsCommands() {
  return `
    ${sectionBlock(t("agents.runOnceTitle"), t("agents.runOnceBody"), commandLine(t("agents.windowsPowerShell"), state.enrollment.install_ps1)).__raw}
    ${sectionBlock(t("agents.installTitle"), t("agents.installBody"), commandLine(t("agents.windowsService"), state.enrollment.service_ps1)).__raw}
  `;
}
