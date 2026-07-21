# Manual Review Remembered Choice Design

## Scope

Allow a reviewer to remember the current Allow or Deny choice for a configurable number of minutes. Later intercepted commands still open the normal review popup and keep their policy-configured countdown.

## Interaction

- Show a "Remember my choice" checkbox with a whole-minute input from 1 through 1440, defaulting to 10.
- Clicking Allow or Deny applies that action immediately. When the checkbox is checked, the same action becomes the remembered choice for that review stream.
- A later popup within the remembered period remains fully interactive. If nobody acts before its own countdown ends, it applies the remembered choice.
- A reviewer can override the remembered choice before the countdown ends. Keeping the checkbox checked refreshes the remembered choice and duration from that click; clearing it disables the remembered choice.
- Without an active remembered choice, timeout defaults to Deny.

## Server Behavior

The existing in-memory manual review hub stores one remembered decision per poller key: organization ID plus optional terminal session ID. The state contains Allow or Deny, configured minutes, and an absolute UTC expiry.

Each new review snapshots the active remembered decision as its timeout default but always keeps its own policy deadline. Existing pending reviews are not changed when another review stores a choice. Server restart clears remembered state.

## API

Keep the existing optional `auto_allow_minutes` transport field for compatibility. Omitted leaves remembered state unchanged, `0` clears it, and `1..1440` stores the current Allow or Deny decision. Review responses include `default_allow` so the popup can show the exact timeout result.

## Verification

- No remembered choice times out as Deny.
- Remembered Allow and Deny both keep the popup visible and apply only when that popup expires.
- Manual action before expiry wins and may replace or clear the remembered choice.
- Existing authorization, active-poller gating, and concurrent review behavior remain unchanged.
