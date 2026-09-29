# Security and User Group Target Binding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the reported security issues and implement organization user-group bindings to batches of SSH targets with enforced access control.

**Architecture:** Add application-level AES-GCM encryption for stored SSH and LLM secrets, using the configured or generated server key while preserving legacy plaintext reads for migration. Add a many-to-many organization user-group/SSH-target relation with replacement and individual binding APIs; targets with bindings are visible and usable only by members of at least one bound group. Apply the same check to HTTP, SSH, MCP, and system-probe paths.

**Tech Stack:** Go 1.26, modernc SQLite migrations, net/http, React/TypeScript, React Query, existing repository and policy services.

---

### Task 1: Add secret encryption at rest

**Files:**
- Create: `internal/store/secret_box.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/repository.go`
- Modify: `internal/server/app.go`
- Modify: `internal/server/config.go`
- Test: `internal/store/store_test.go`

- [ ] Add AES-256-GCM SecretBox keyed by SHA-256 of configured key material; prefix ciphertext with `gosshd:v1:`, leave legacy non-prefixed values readable, and encrypt new writes.
- [ ] Pass key material into Store.Open through a variadic argument so existing repository tests without a key remain compatible.
- [ ] Make target, SSH credential, and LLM config repository writes encrypt and scan methods decrypt.
- [ ] Load `SecretKey`, `SecretKeyPath`, or `GOSSHD_SECRET_KEY`; when absent generate a 32-byte key and write a 0600 `.secret-key` file beside the database.
- [ ] Add tests proving ciphertext differs from plaintext in SQLite and round-trips through repository reads.

### Task 2: Enforce target access policy on system probes and Agent ownership

**Files:**
- Modify: `internal/server/api_connect.go`
- Modify: `internal/server/api_targets.go`
- Modify: `internal/server/ssh_bastion.go`
- Modify: `internal/server/mcp.go`
- Test: `internal/server/api_test.go`

- [ ] Add `RequestSystem` and require `EvaluateAccess` before system probe execution.
- [ ] Validate AgentID owner organization on target create/update and enforce ownership again before opening Agent streams.
- [ ] Reuse target group access checks from Task 3 in HTTP target lookup, SSH alias resolution, MCP listing, and system endpoints.
- [ ] Add tests showing denied system access returns 403 and an Agent from another organization cannot be attached.

### Task 3: Implement user-group to SSH-target batches

**Files:**
- Create: `internal/store/migrations/0003_user_group_targets.sql`
- Modify: `internal/store/models.go`
- Modify: `internal/store/repository.go`
- Modify: `internal/server/api.go`
- Modify: `internal/server/api_groups.go`
- Modify: `internal/server/api_targets.go`
- Modify: `internal/server/ssh_bastion.go`
- Modify: `internal/server/mcp.go`
- Modify: `web/src/types.ts`
- Modify: `web/src/api.ts`
- Modify: `web/src/pages/MembersPage.tsx`
- Test: `internal/store/store_test.go`
- Test: `internal/server/api_test.go`

- [ ] Create `organization_user_group_targets(group_id,target_id,created_at)` with cascading foreign keys and indexes.
- [ ] Add group target IDs to API models and repository methods for list, replace, attach, detach, and access checks.
- [ ] Add owner/admin-protected endpoints: GET group targets, PUT replacement with `target_ids`, POST individual bind, DELETE individual unbind.
- [ ] Filter target lists and target lookup so bound targets require membership in a bound group; preserve unbound target behavior.
- [ ] Apply the same access rule to SSH alias resolution and MCP target listing.
- [ ] Add frontend target ID state and a multi-select batch binding editor in the user groups modal.
- [ ] Add repository and API tests for group binding, cross-organization rejection, visibility, and SSH access denial.

### Task 4: Harden HTTP authentication endpoints

**Files:**
- Modify: `internal/server/api.go`
- Modify: `internal/server/api_auth.go`
- Modify: `internal/server/rate_limit.go`
- Modify: `internal/server/app.go`
- Test: `internal/server/api_test.go`
- Test: `internal/server/app_test.go`

- [ ] Cap JSON bodies with `http.MaxBytesReader` and reject trailing JSON data.
- [ ] Add server read/write timeouts appropriate for API and websocket traffic.
- [ ] Rate-limit by normalized account subject and trusted remote address; ignore untrusted X-Forwarded-For unless explicitly configured.
- [ ] Bind OAuth state to a short-lived HttpOnly SameSite cookie and verify it during callback.
- [ ] Add tests for oversized JSON, same-account brute-force throttling, and OAuth state mismatch.

### Task 5: Validate and document

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] Document `--secret-key`, `--secret-key-path`, generated key file permissions, and HTTPS/reverse-proxy requirements.
- [ ] Run `gofmt`, `go test ./internal/store ./internal/server ./internal/auth`, and frontend type/build checks.
- [ ] Run the full test suite and record any pre-existing platform-specific failures.

