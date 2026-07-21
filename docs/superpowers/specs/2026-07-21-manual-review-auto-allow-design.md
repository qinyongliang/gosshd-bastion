# Manual Review Auto-Allow Design

## Scope

Add an optional automatic-allow deadline to the existing manual command review popup. The setting applies to the current review stream: organization-wide reviews use the organization stream, while web-terminal reviews use that terminal session stream.

The state is intentionally kept in the existing in-memory manual review hub. It survives browser refreshes and other reviewers opening the same stream, but it resets when the server restarts.

## Interaction

- Add a checked/unchecked option labeled "Allow automatically when time expires" above the existing Allow and Deny actions.
- Place a numeric minutes input beside the option, defaulting to 10 minutes. Accept whole minutes from 1 through 1440.
- The option starts inactive and unchecked.
- Checking it and clicking Allow approves the current command and starts the deadline.
- While active, later intercepted commands still open the full review popup. The option remains checked, the minutes input retains its configured value, and the header shows the shared remaining time as `MM:SS` or `HH:MM:SS`.
- Clicking Allow again with the same minutes approves only the current command and keeps the existing deadline.
- Changing the minutes and clicking Allow approves the current command and starts a new deadline from that click. All pending reviews in the same stream move to the new deadline.
- Unchecking the option and clicking Allow approves the current command and disables automatic allowance for the stream.
- Deny only denies the current command; it does not change the automatic-allow setting.
- Do not show additional explanatory text beneath the control.

## Server Behavior

The manual review hub stores one automatic-allow state per existing poller key: organization ID plus optional terminal session ID. The state contains the configured whole minutes and absolute UTC deadline.

Each manual review request retains its normal policy deadline. When an automatic-allow state is active, the request instead waits until the shared automatic deadline and is marked to allow on expiry. When that deadline arrives, every still-pending review in the stream resolves as allowed. New reviews after the state expires use the normal policy timeout and deny behavior.

Renewing the deadline updates all pending reviews in the stream and reschedules their expiry. Disabling the setting returns pending reviews to their normal policy deadlines; reviews whose normal deadline has already passed resolve as denied immediately.

The hub owns review expiry so rescheduled deadlines cannot be missed by stale timers in individual SSH request handlers. Manual Allow, manual Deny, cancellation, and authorization checks continue through the existing decision endpoint and review channel.

## API

Manual review responses expose the active automatic-allow deadline and configured minutes for their stream.

The decision endpoint accepts an optional automatic-allow update:

- omitted: leave the stream setting unchanged;
- `0`: disable it;
- `1..1440`: set or renew it from the current decision time.

The frontend omits the value when the checkbox remains checked and the minutes have not changed. It sends `0` when the active checkbox is cleared, and sends the entered minutes when enabling or renewing.

## Failure Handling

- Reject invalid minute values at the API boundary.
- If an automatic deadline expires while a manual decision is racing it, the hub lock permits only one decision to remove and resolve the request.
- Existing authentication and organization authorization remain unchanged.
- No database migration, new dependency, background worker, or persistent preference is introduced.

## Verification

- Hub tests cover enabling, unchanged approvals, renewal, disabling, shared pending-request deadlines, automatic allowance at expiry, and normal denial after expiry.
- API tests cover response fields, valid updates, invalid minutes, and authorization.
- Frontend checks cover the default unchecked 10-minute input, active checked state, second-precision remaining time, unchanged Allow payloads, renewed Allow payloads, and Deny leaving the setting unchanged.
- Run focused Go tests, the frontend type check, and the production frontend build.
