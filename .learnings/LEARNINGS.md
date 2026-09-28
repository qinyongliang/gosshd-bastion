# Learnings

Corrections, insights, and knowledge gaps captured during development.

**Categories**: correction | insight | knowledge_gap | best_practice

---

## [LRN-20260807-001] correction

**Logged**: 2026-08-07T00:00:00+08:00
**Priority**: high
**Status**: resolved
**Area**: frontend

### Summary
Organization invite creation belongs in the Members page and may only invite ordinary members.

### Details
The first implementation placed the action in Organizations and allowed owners to issue admin invitations. The intended contract is that organization owners and admins create member-only codes from Members.

### Suggested Action
Enforce member-only invites in HTTP and MCP, and keep the creation UI in Members.

### Metadata
- Source: user_feedback
- Related Files: web/src/pages/MembersPage.tsx, internal/server/api_orgs.go, internal/server/mcp.go
- Tags: organization, invite, permissions

### Resolution
- **Resolved**: 2026-08-07T00:00:00+08:00
- **Notes**: Moved the UI and added server-side role enforcement tests.

---
