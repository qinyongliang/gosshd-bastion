# Tunnel IP Statistics Implementation Plan

**Goal:** Rename the traffic dialog to Statistics and add source IP traffic/connection lists, single-IP filtering, sorting and custom time ranges.

**Architecture:** Keep true tunnel-wide concurrency counters and record parallel counters per source IP in five-minute buckets. Rebuild only the two audit traffic tables through migration 0003, discarding their old rows. Merge persisted and live counters under the existing metrics lock so reads cannot double-count a flush.

**Tech Stack:** Go, SQLite, React, TypeScript, Ant Design, Playwright.

### Storage and collection

- [x] Add `source_ip` to traffic bucket/totals primary keys; use an empty IP for tunnel-wide counters and `unknown` for unavailable source addresses.
- [x] Record real ingress addresses in Agent stream requests, relay/P2P forwarding and temporary SSH forwards. Normalize IPv4/IPv6 and remove ports.
- [x] Add store tests for source isolation, time boundaries, persistence and destructive traffic-only migration; extend real relay/P2P/SSH integration tests to assert source IP counts and bytes.

### API

- [x] Return `sources` and scoped `active_connections` alongside `buckets`; validate optional `source_ip`, keep organization-admin permissions and the 31-day limit.
- [x] Aggregate source rows with sums for bytes/opened connections and maxima for per-IP concurrency. Preserve tunnel-wide peak concurrency using its own counters.
- [x] Cover filtered persisted/live snapshots before and after flushing, missing sources and invalid parameters. Expose matching statistics in the tunnel MCP tool.

### Interface

- [x] Rename the action and title to 统计 / Statistics. Retain chart/path controls and add source IP search, click-to-filter rows and reset.
- [x] Add a paginated source list with directional/total bytes, opened connections and peak concurrency; traffic and connection sorting controls.
- [x] Keep 24-hour/7-day/30-day presets and add start/end datetime fields with a custom range apply action. Explain five-minute precision and the 31-day limit.
- [x] Verify both locales, IP filtering, sorting, custom ranges, transport filters and mobile overflow in the existing browser test.

### Validation

```powershell
go test ./internal/store ./internal/server ./internal/agent ./internal/protocol ./internal/tunnel
pnpm build
go test ./internal/server -run TestTunnelTrafficUIE2EWithBrowser -count=1
git diff --check
```

Verified: production frontend build; full server/store/protocol/tunnel tests; source IP API/MCP tests; real relay, P2P, local and remote SSH source attribution; Chinese/English browser tests at desktop/mobile widths. Agent tests pass when excluding the pre-existing `TestWindowsShellUsesConPTYForInteractiveInput`, which expects English ping output on this Chinese Windows installation.

Rollout: update the server and ingress Agents together. Audit migration 0003 discards only old traffic buckets/totals once. Main configuration and command audit logs are unaffected. Older ingress Agents cannot report source addresses and appear as `unknown`. Time statistics retain five-minute precision and a maximum range of 31 days.
