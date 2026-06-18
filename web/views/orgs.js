import { state } from "../state.js";
import { escapeHTML, panel, raw, table } from "../components/html.js";

export function renderOrgs() {
  return raw(`
    <div class="grid two">
      ${panel("Create organization", "New organizations receive a default all-members user group.", `
        <form data-action="create-org" class="stack">
          <input name="name" aria-label="Organization name" autocomplete="off" placeholder="Organization name" required />
          <input name="slug" aria-label="Organization slug" autocomplete="off" placeholder="organization-slug" required />
          <button type="submit">Create organization</button>
        </form>
      `).__raw}
      ${panel("Join organization", "Use an invite code from an organization owner or admin.", `
        <form data-action="join-org" class="stack">
          <input name="code" aria-label="Invite code" autocomplete="off" placeholder="Invite code" required />
          <button type="submit">Join organization</button>
        </form>
      `).__raw}
    </div>
    ${panel("Your organizations", "Switch active scope from the sidebar.", table(["Name", "Slug", "Role"], state.orgs.map((org) => [
      `<strong>${escapeHTML(org.name)}</strong>${org.is_personal ? "<small>personal</small>" : ""}`,
      escapeHTML(org.slug),
      escapeHTML(org.role || "member"),
    ]))).__raw}
  `);
}
