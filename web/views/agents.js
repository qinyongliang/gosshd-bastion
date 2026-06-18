import { state } from "../state.js";
import { commandLine, icon, panel } from "../components/html.js";

export function renderAgents() {
  return panel("Agent SSH enrollment", "Create an enrollment and install a private-side agent as a startup service when needed.", `
    <form data-action="create-agent" class="stack">
      <input name="label" aria-label="Agent service alias" autocomplete="off" placeholder="service alias" required />
      <input name="default_host" aria-label="Agent default host" autocomplete="off" value="127.0.0.1" required />
      <input name="default_port" aria-label="Agent default SSH port" type="number" value="22" required />
      <button type="submit">${icon("spark").__raw}Create enrollment</button>
    </form>
    <div class="guide-block service">
      <strong>Startup install</strong>
      <span>Run the install command with sh on Linux to register a systemd service with systemctl. On Windows, run the PowerShell install command to register the agent with sc.exe.</span>
    </div>
    ${state.enrollment ? `
      <div class="guide-block">
        <strong>Run once</strong>
        <span>Starts the agent in the current terminal session.</span>
      </div>
      ${commandLine("Linux/macOS shell", state.enrollment.install_sh)}
      ${commandLine("Windows PowerShell", state.enrollment.install_ps1)}
      <div class="guide-block service">
        <strong>Install as startup service</strong>
        <span>Linux uses systemctl. Windows uses sc.exe. The agent registers as a normal SSH service and can be renamed in SSH services.</span>
      </div>
      ${commandLine("Linux systemctl service", state.enrollment.service_sh)}
      ${commandLine("Windows sc.exe service", state.enrollment.service_ps1)}
    ` : ""}
  `);
}
