# Manual Review Remembered Choice Design

## Scope

Allow a reviewer to remember the current Allow or Deny choice for a configurable number of minutes and a snapshot of selected target servers. The choice is shared for the intercepted SSH user across browser windows. Later intercepted commands still open the normal review popup and keep their policy-configured countdown.

Allow an organization administrator to create the same temporary Allow choice proactively from the audit list without waiting for a client command to be intercepted.

## Interaction

- Show a "Remember my choice" checkbox with a whole-minute input from 1 through 1440, defaulting to 10.
- Show a collapsed "More" control while remembering is enabled. It expands one shared target picker with sections for target folders, tags, command safety groups, and individual targets.
- With no active remembered choice, all targets are selected by default. Individual target rows show only the target name.
- Folder, tag, and safety-group controls are bulk selectors over target IDs. Clearing one selector removes targets covered only by that selector within the same section; a target still covered by another selected selector in that section remains selected. Checking an individual target directly adds or removes its ID.
- Only target IDs are saved. Group selector state is not saved; another window derives checked, unchecked, and partial group states from the server-provided target IDs.
- Clicking Allow or Deny applies that action immediately. When the checkbox is checked, the same action, duration, and target IDs become the remembered choice for the intercepted SSH user.
- A later popup within the remembered period remains fully interactive. If nobody acts before its own countdown ends, it applies the remembered choice.
- A reviewer can override the remembered choice before the countdown ends. Keeping the checkbox checked refreshes the remembered choice and duration from that click; clearing it disables the remembered choice.
- Without an active remembered choice, timeout defaults to Deny.
- Add a "Temporary authorization" button to the audit-list header for organization administrators. Its modal reuses the same duration and target picker, requires an SSH user, and stores an Allow choice immediately without creating or replaying a command.

## Server Behavior

The existing in-memory manual review hub stores one remembered decision per organization ID plus intercepted SSH user ID. The state contains Allow or Deny, configured minutes, an absolute UTC expiry, and a set of target IDs. Browser session IDs and reviewer IDs are not part of the key.

Each new review snapshots the active remembered decision only when its target ID is in the saved target set, but always keeps its own policy deadline. Existing pending reviews are not changed when another review stores or changes a choice. Server restart clears remembered state.

The server creates and times a review even when no browser is polling for reviews. Closing the web page does not cancel the pending review or its remembered choice. Every window renders the remembered default, absolute expiry, and target IDs returned by the server; browser storage is not used. If no page is opened, the server still applies the timeout decision at the deadline.

## API

Keep the existing optional `auto_allow_minutes` transport field and add `auto_allow_target_ids`. Omitted minutes leave remembered state unchanged, `0` clears it, and `1..1440` requires at least one target ID and stores the current Allow or Deny decision. Review responses include `default_allow`, `auto_allow_expires_at`, and `auto_allow_target_ids` from the server-owned command snapshot.

Add `GET /api/manual-review-choice?organization_id=...&user_id=...` for the proactive modal to load the SSH user's active state. Add `PUT /api/manual-review-choice` with `organization_id`, `user_id`, `auto_allow_minutes`, and `auto_allow_target_ids` to store a proactive Allow choice.

Both endpoints require organization-admin access. The server verifies that the SSH user belongs to the organization, every target belongs to the organization, the duration is from 1 through 1440 minutes, and at least one target is selected.

## Verification

- A review with no remembered choice times out as Deny.
- Remembered Allow and Deny both keep the popup visible and apply only when that popup expires on a selected target.
- Remembered state is isolated by SSH user and target ID, but is identical across browser windows and reviewer administrators.
- Manual action before expiry wins and may replace or clear the remembered choice.
- A review created with no active browser poller still resolves at its server deadline.
- Cross-organization users and targets are rejected by the server.
- Folder, tag, and safety-group overlap changes the final target-ID set without persisting group selectors.
- Proactive temporary authorization stores Allow without creating or replaying an audit command.
- Existing authorization and concurrent review behavior remain unchanged.
