import { state } from "../state.js";
import { emptyState, escapeHTML, icon, panel, raw, table } from "../components/html.js";

export function renderKeys() {
  return panel("Public keys", "Keys identify users at SSH login before the bastion resolves target aliases.", `
    <form data-action="create-key" class="stack">
      <input name="name" aria-label="Public key name" autocomplete="off" placeholder="Laptop" required />
      <textarea name="authorized_key" aria-label="Authorized public key" autocomplete="off" spellcheck="false" placeholder="ssh-ed25519 AAAA..." required></textarea>
      <button type="submit">${icon("key").__raw}Add key</button>
    </form>
    ${state.keys.length ? table(["Name", "Fingerprint", ""], state.keys.map((key) => [
      escapeHTML(key.name),
      escapeHTML(key.fingerprint),
      `<button data-click="delete-key" data-id="${escapeHTML(key.id)}" class="danger small">Remove</button>`,
    ])) : emptyState("No keys", "Add a public key before using SSH aliases.").__raw}
  `);
}
