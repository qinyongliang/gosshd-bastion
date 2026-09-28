# Feature Requests

Capabilities requested by the user.

---

## [FEAT-20260807-001] organization_invite_codes

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: high
**Status**: resolved
**Area**: frontend

### Requested Capability
Allow organization managers to create invite codes with an optional expiration time; codes do not expire by default.

### User Context
The UI currently lets users join with a code but provides no way to create one.

### Complexity Estimate
simple

### Suggested Implementation
Expose the existing invite endpoint in the organization list and make its expiration optional.

### Metadata
- Frequency: first_time
- Related Features: organization membership

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Added invite creation UI, optional expiration, permanent-by-default behavior, and API tests.

---
