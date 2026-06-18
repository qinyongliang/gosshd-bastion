# Bastion Conversion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the approved SQLite-backed bastion conversion for `gosshd`, including user and organization management, public-key SSH authentication, direct and agent-backed SSH targets, command policies, command audit logs, an engineered management UI, and complete e2e coverage.

**Architecture:** Keep the current single Go server/agent deployment model. Add a SQLite-backed application layer under focused internal packages, expose management JSON APIs plus embedded static UI assets from `gosshd-server`, and replace UUID SSH routing with public-key-authenticated alias routing to direct or agent-backed SSH targets.

**Tech Stack:** Go 1.26, `database/sql`, `modernc.org/sqlite`, `golang.org/x/crypto/bcrypt`, `golang.org/x/crypto/ssh`, existing gorilla websocket/yamux agent transport, embedded static HTML/CSS/ES modules, Go integration/e2e tests.

---

## File Structure

- Create `internal/store/store.go`: opens SQLite, configures pragmas, runs migrations.
- Create `internal/store/migrations.go`: contains ordered schema statements.
- Create `internal/store/models.go`: shared storage model structs and constants.
- Create `internal/store/repository.go`: repository methods for users, sessions, organizations, keys, targets, agents, policies, and audit logs.
- Create `internal/store/store_test.go`: migration and repository tests.
- Create `internal/auth/service.go`: password hashing, session token generation, session lookup.
- Create `internal/auth/service_test.go`: auth/session tests.
- Create `internal/bastion/types.go`: target, policy, audit, and routing service types.
- Create `internal/bastion/service.go`: alias resolution, public key lookup, target secret handling, policy evaluation, audit writes.
- Create `internal/bastion/policy_test.go`: policy matching and precedence tests.
- Create `internal/bastion/keys_test.go`: public key fingerprint tests.
- Create `internal/server/api.go`: API route registration, JSON helpers, auth middleware.
- Create `internal/server/api_auth.go`: register/login/logout/me handlers.
- Create `internal/server/api_orgs.go`: organization and invite handlers.
- Create `internal/server/api_groups.go`: organization user group handlers.
- Create `internal/server/api_keys.go`: public key handlers.
- Create `internal/server/api_targets.go`: target handlers.
- Create `internal/server/api_agents.go`: agent enrollment and agent listing handlers.
- Create `internal/server/api_policies.go`: policy, rule, policy-target, and policy-user-group handlers.
- Create `internal/server/api_audit.go`: audit listing handler.
- Create `internal/server/api_test.go`: HTTP API integration tests.
- Create `internal/server/ssh_bastion.go`: public-key SSH auth, alias resolution, target SSH client connection, and channel bridging.
- Create `internal/server/ssh_bastion_test.go`: SSH auth/routing tests.
- Create `internal/server/e2e_bastion_test.go`: complete e2e test.
- Modify `internal/server/app.go`: initialize store/services, route API/static UI, and wire SSH bastion dependencies.
- Modify `internal/server/config.go`: add database and secret config.
- Modify `internal/server/http.go`: keep legacy routes and add API/static route composition.
- Modify `internal/server/ssh.go`: split legacy agent routing helpers from new bastion SSH routing.
- Modify `internal/server/registry.go`: support persisted agent ids while preserving existing online session behavior.
- Modify `internal/protocol/protocol.go`: add optional agent enrollment token field to `AgentHello`.
- Modify `internal/agent/config.go`, `internal/agent/client.go`, `cmd/gosshd-agent/main.go`: pass enrollment token to server.
- Modify `cmd/gosshd-server/main.go`: add flags for database path and secret key material.
- Create `web/index.html`: management UI shell.
- Create `web/styles.css`: polished operational UI styling with spacious hierarchy, subtle motion, and clear states.
- Create `web/app.js`: frontend state and screen routing.
- Create `web/api.js`: fetch wrapper and typed API calls.
- Create `web/components.js`: reusable render helpers for tables, forms, modals, badges, and buttons.
- Create `internal/server/web_embed.go`: embeds `web/*` and serves SPA fallback.
- Modify `README.md` and `README.zh-CN.md`: document bastion mode, install commands, test command, and e2e expectations.

## Cross-Cutting Rules

- Run Go commands with `GOPROXY=https://goproxy.cn,direct` in this environment.
- Keep each task green before moving to the next task.
- Commit after every task that passes its verification command.
- Do not remove legacy `/run.sh`, `/run.ps1`, `/download/agent/`, or `/ws/agent` routes.
- Use JSON error responses of the shape `{"error":"message"}` for API handlers.
- SSH command denial returns stderr text and exit code `126`.

---

### Task 1: SQLite Store, Schema, and Repository Core

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/migrations.go`
- Create: `internal/store/models.go`
- Create: `internal/store/repository.go`
- Create: `internal/store/store_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the SQLite dependency**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go get modernc.org/sqlite@latest
```

Expected: `go.mod` includes `modernc.org/sqlite`.

- [ ] **Step 2: Write failing migration tests**

Create `internal/store/store_test.go` with tests named:

```go
func TestOpenAppliesBastionSchema(t *testing.T)
func TestRepositoryCreatesUserOrganizationKeyTargetPolicyAndAudit(t *testing.T)
```

The first test opens `filepath.Join(t.TempDir(), "gosshd.db")`, queries `sqlite_master`, and asserts these tables exist: `users`, `sessions`, `organizations`, `organization_members`, `organization_user_groups`, `organization_user_group_members`, `organization_invites`, `user_public_keys`, `ssh_targets`, `agent_enrollments`, `agents`, `command_policies`, `policy_rules`, `policy_targets`, `policy_user_groups`, `llm_policy_configs`, `command_audit_logs`.

The second test creates a user, organization, default user group membership, public key, direct target, policy, policy rule, policy target link, policy user group link, and audit log through repository methods. It then reads each item back and asserts persisted ids are non-empty and relationships match.

- [ ] **Step 3: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/store -run TestOpenAppliesBastionSchema -v
```

Expected: package does not compile because `internal/store` does not exist.

- [ ] **Step 4: Implement store package**

Create the store package with:

- `Open(ctx context.Context, path string) (*Store, error)`
- `Close() error`
- `DB() *sql.DB`
- `Repository() *Repository`
- `ApplyMigrations(ctx context.Context) error`
- model structs for each schema table
- repository methods used by the tests

Use pragmas:

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
```

Use `TEXT` ids generated by `uuid.NewString()`. Store timestamps as UTC RFC3339 strings.

- [ ] **Step 5: Run store tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/store -v
```

Expected: PASS.

- [ ] **Step 6: Run full test suite**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```powershell
git add go.mod go.sum internal/store
git commit -m "feat: add sqlite store and bastion schema"
```

---

### Task 2: Auth, Sessions, Organizations, and Public Keys

**Files:**
- Create: `internal/auth/service.go`
- Create: `internal/auth/service_test.go`
- Create: `internal/bastion/types.go`
- Create: `internal/bastion/service.go`
- Create: `internal/bastion/keys_test.go`
- Modify: `internal/store/repository.go`
- Modify: `internal/store/models.go`

- [ ] **Step 1: Write auth and key tests**

Create tests:

```go
func TestAuthServiceRegistersAndAuthenticatesUser(t *testing.T)
func TestAuthServiceRejectsBadPassword(t *testing.T)
func TestPublicKeyFingerprintRoundTrip(t *testing.T)
func TestLookupUserByPublicKeyFingerprint(t *testing.T)
```

Use an in-memory or temp SQLite database. Generate an RSA signer with `gossh.NewSignerFromKey`, marshal the public key with `gossh.MarshalAuthorizedKey`, store it, and assert lookup by `gossh.FingerprintSHA256(key)` returns the expected user id.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/auth ./internal/bastion -v
```

Expected: packages or functions do not exist.

- [ ] **Step 3: Implement auth service**

Add:

- `Register(ctx, email, displayName, password string) (store.User, string, error)`
- `Login(ctx, email, password string) (store.User, string, error)`
- `UserForSession(ctx, token string) (store.User, error)`
- `Logout(ctx, token string) error`

Hash passwords with bcrypt. Store only SHA-256 token hashes in SQLite. Return raw session token only to the caller.

- [ ] **Step 4: Implement public-key helpers**

Add bastion helpers:

- `NormalizeAuthorizedKey(raw string) (normalized string, fingerprint string, err error)`
- `LookupUserByPublicKey(ctx context.Context, key gossh.PublicKey) (store.User, error)`

Use `gossh.ParseAuthorizedKey` and `gossh.FingerprintSHA256`.

- [ ] **Step 5: Run tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/auth ./internal/bastion ./internal/store -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/auth internal/bastion internal/store
git commit -m "feat: add auth sessions and public key lookup"
```

---

### Task 3: HTTP API Foundation, Auth API, Organizations, User Groups, and Keys

**Files:**
- Create: `internal/server/api.go`
- Create: `internal/server/api_auth.go`
- Create: `internal/server/api_orgs.go`
- Create: `internal/server/api_groups.go`
- Create: `internal/server/api_keys.go`
- Create: `internal/server/api_test.go`
- Modify: `internal/server/app.go`
- Modify: `internal/server/config.go`
- Modify: `cmd/gosshd-server/main.go`

- [ ] **Step 1: Write API integration tests**

Create `internal/server/api_test.go` tests:

```go
func TestAPIRegisterLoginMeAndLogout(t *testing.T)
func TestAPIOrganizationCreateInviteJoin(t *testing.T)
func TestAPIOrganizationDefaultAndCustomUserGroups(t *testing.T)
func TestAPIPublicKeyCRUD(t *testing.T)
```

Use `httptest.NewServer` with `NewApp(Config{DatabasePath: tempDB, SecretKeyPath: tempKey})`. Register users through HTTP, preserve cookies with `http.Client` and `cookiejar.Jar`, create an organization, assert its default all-members user group exists and contains the creator, create an invite, join with a second user, assert the second user is also in the default group, create a custom group, add/remove a member, add and delete a public key, and assert JSON responses.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestAPI -v
```

Expected: missing API routes and config fields.

- [ ] **Step 3: Wire app dependencies**

Modify `Config` with:

- `DatabasePath string`
- `SecretKey string`
- `SecretKeyPath string`
- `SessionCookieName string`

Modify `App` to hold store, auth service, bastion service, and secret manager. `NewApp` should be able to return an app that initializes lazily in `Run`/`RunListeners`, while tests can use temp paths.

- [ ] **Step 4: Implement API helpers**

Add helpers:

- `writeJSON(w http.ResponseWriter, status int, v any)`
- `writeError(w http.ResponseWriter, status int, msg string)`
- `readJSON(r *http.Request, dst any) error`
- `requireUser(next func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc`

- [ ] **Step 5: Implement auth, org, and key routes**

Register routes in `routes`:

- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/me`
- `POST /api/orgs`
- `GET /api/orgs`
- `POST /api/orgs/{id}/invites`
- `POST /api/orgs/join`
- `GET /api/orgs/{id}/groups`
- `POST /api/orgs/{id}/groups`
- `POST /api/orgs/{id}/groups/{group_id}/members`
- `DELETE /api/orgs/{id}/groups/{group_id}/members/{user_id}`
- `GET /api/keys`
- `POST /api/keys`
- `DELETE /api/keys/{id}`

Use `http.ServeMux` path matching available in Go 1.26.

- [ ] **Step 6: Run API tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestAPI -v
```

Expected: PASS.

- [ ] **Step 7: Run full tests and commit**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
git add cmd/gosshd-server internal/auth internal/bastion internal/server internal/store
git commit -m "feat: add management auth organization group and key APIs"
```

---

### Task 4: Target, Agent Enrollment, Policy, User Group Binding, and Audit APIs

**Files:**
- Create: `internal/server/api_targets.go`
- Create: `internal/server/api_agents.go`
- Create: `internal/server/api_policies.go`
- Create: `internal/server/api_audit.go`
- Create: `internal/bastion/policy_test.go`
- Modify: `internal/bastion/service.go`
- Modify: `internal/store/repository.go`
- Modify: `internal/store/models.go`
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Write target and policy tests**

Add tests:

```go
func TestAPITargetPolicyAndAuditFlow(t *testing.T)
func TestAPIAgentEnrollmentReturnsInstallScripts(t *testing.T)
func TestPolicyEvaluationWhitelistBlacklistAndDefault(t *testing.T)
```

The API test creates a direct target alias `test2`, creates a policy with default `deny`, adds a whitelist rule, attaches it to the target, attaches it to the organization default user group, inserts an audit log through service code, and reads it through `/api/audit`.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server ./internal/bastion -run "TestAPI(Target|Agent)|TestPolicy" -v
```

Expected: missing handlers and policy service.

- [ ] **Step 3: Implement policy evaluation**

Add:

- `EvaluateCommand(ctx, user, target, command string) (Decision, error)`
- rule matching for `exact`, `prefix`, `contains`
- applicability: policy applies when it has no user-group bindings or the user belongs to at least one bound group
- precedence: whitelist allows, blacklist denies, unmatched uses LLM when configured, otherwise default action
- audit helper methods for allowed and denied commands

- [ ] **Step 4: Implement target, enrollment, policy, and audit routes**

Register:

- `GET /api/targets`
- `POST /api/targets`
- `PATCH /api/targets/{id}`
- `DELETE /api/targets/{id}`
- `POST /api/agent-enrollments`
- `GET /api/agents`
- `GET /install/{token}.sh`
- `GET /install/{token}.ps1`
- `GET /api/policies`
- `POST /api/policies`
- `POST /api/policies/{id}/rules`
- `POST /api/policies/{id}/targets`
- `DELETE /api/policies/{id}/targets/{target_id}`
- `POST /api/policies/{id}/user-groups`
- `DELETE /api/policies/{id}/user-groups/{group_id}`
- `GET /api/audit`

- [ ] **Step 5: Run tests and commit**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server ./internal/bastion ./internal/store -v
go test ./...
git add internal/bastion internal/server internal/store
git commit -m "feat: add target policy agent and audit APIs"
```

---

### Task 5: Agent Enrollment Protocol

**Files:**
- Modify: `internal/protocol/protocol.go`
- Modify: `internal/agent/config.go`
- Modify: `internal/agent/client.go`
- Modify: `cmd/gosshd-agent/main.go`
- Modify: `internal/server/http.go`
- Modify: `internal/server/registry.go`
- Modify: `internal/server/agent_ws_test.go`

- [ ] **Step 1: Write enrollment websocket tests**

Add tests:

```go
func TestAgentWSEnrollmentCreatesPersistedAgent(t *testing.T)
func TestAgentWSRejectsInvalidEnrollmentToken(t *testing.T)
```

The success test creates an enrollment through the repository, connects websocket with `AgentHello{EnrollmentToken: rawToken}`, expects OK, and asserts an `agents` row exists, a normal `ssh_targets` row with `target_type = agent` exists and uses the enrollment label as its initial alias, and the registry has the persisted agent id online.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestAgentWSEnrollment -v
```

Expected: `EnrollmentToken` field and enrollment handling missing.

- [ ] **Step 3: Add protocol and agent config fields**

Add `EnrollmentToken string json:"enrollment_token,omitempty"` to `protocol.AgentHello`. Add `EnrollmentToken string` to `agent.Config`. Parse `--enrollment-token` in `cmd/gosshd-agent`.

- [ ] **Step 4: Update agent websocket registration**

When enrollment token is present, validate token hash, create/update persisted agent, create/update a normal renameable `ssh_targets` row for the agent-backed SSH service, and register yamux session by persisted agent id. Without enrollment token, keep legacy UUID registration.

- [ ] **Step 5: Run tests and commit**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server ./internal/agent ./internal/protocol -v
go test ./...
git add cmd/gosshd-agent internal/agent internal/protocol internal/server internal/store
git commit -m "feat: enroll agents into bastion owners"
```

---

### Task 6: SSH Bastion Routing and Direct Target Bridging

**Files:**
- Create: `internal/server/ssh_bastion.go`
- Create: `internal/server/ssh_bastion_test.go`
- Modify: `internal/server/ssh.go`
- Modify: `internal/server/app.go`
- Modify: `internal/bastion/service.go`
- Modify: `internal/store/repository.go`

- [ ] **Step 1: Write SSH bastion tests**

Create tests:

```go
func TestSSHRejectsUnknownPublicKey(t *testing.T)
func TestSSHExecRoutesAliasToDirectTarget(t *testing.T)
func TestSSHDeniesBlacklistedExecAndAudits(t *testing.T)
```

Use a local in-process SSH target server in the test. Store its password or private key auth in SQLite. Create platform user public key and connect to bastion with `gossh.ClientConfig{User: "test2", Auth: []gossh.AuthMethod{gossh.PublicKeys(userSigner)}}`.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestSSH -v
```

Expected: current server still uses `NoClientAuth` and UUID lookup.

- [ ] **Step 3: Implement public-key SSH server config**

Change SSH config construction to accept a bastion public-key lookup callback. Store authenticated user id in `gossh.Permissions.Extensions["user_id"]`.

- [ ] **Step 4: Implement alias resolution and direct SSH client**

For each accepted SSH connection:

- authenticated user id comes from permissions
- alias comes from `conn.User()`
- resolve personal target first, then organization targets
- connect to direct target with stored password or private key
- reject ambiguous or missing aliases with stderr and exit 255

- [ ] **Step 5: Implement session request bridging**

Bridge `exec`, `shell`, and `sftp` to the target SSH server. For `exec`, evaluate command policy before opening the remote exec. Write audit rows on allow, deny, and completion.

- [ ] **Step 6: Run SSH tests and commit**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestSSH -v
go test ./...
git add internal/bastion internal/server internal/store
git commit -m "feat: route ssh aliases to direct bastion targets"
```

---

### Task 7: Agent-Backed Target Bridging

**Files:**
- Modify: `internal/server/ssh_bastion.go`
- Modify: `internal/server/ssh_bastion_test.go`
- Modify: `internal/server/e2e_bastion_test.go`
- Modify: `internal/server/registry.go`
- Modify: `internal/bastion/service.go`

- [ ] **Step 1: Write agent-backed SSH tests**

Add:

```go
func TestSSHExecRoutesAliasThroughAgentTarget(t *testing.T)
```

Start app HTTP and SSH listeners, start `agent.Client` with an enrollment token, start a local in-process SSH target reachable from the agent process, assert the enrollment created a normal agent-backed target, rename that target alias to `agentbox`, connect to bastion with `User: "agentbox"`, and assert command output plus audit row.

- [ ] **Step 2: Run test to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestSSHExecRoutesAliasThroughAgentTarget -v
```

Expected: agent target connection not implemented.

- [ ] **Step 3: Implement SSH-over-agent TCP connection**

For `target.Type == "agent"`:

- look up online persisted agent id
- open yamux stream from registry
- send `protocol.StreamRequest{Type: protocol.StreamTCP, Target: net.JoinHostPort(target.Host, target.Port)}`
- run `gossh.NewClientConn` over the stream
- bridge session requests as with direct targets

- [ ] **Step 4: Run tests and commit**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run "TestSSHExecRoutesAliasThroughAgentTarget|TestSSHExecRoutesAliasToDirectTarget" -v
go test ./...
git add internal/bastion internal/server
git commit -m "feat: route bastion targets through enrolled agents"
```

---

### Task 8: LLM Policy Review

**Files:**
- Create: `internal/bastion/llm.go`
- Create: `internal/bastion/llm_test.go`
- Modify: `internal/bastion/service.go`
- Modify: `internal/store/repository.go`
- Modify: `internal/server/api_policies.go`

- [ ] **Step 1: Write LLM tests**

Create tests:

```go
func TestLLMPolicyAllowsJSONAllowResponse(t *testing.T)
func TestLLMPolicyDeniesJSONDenyResponse(t *testing.T)
func TestLLMPolicyFailsClosedOnInvalidJSON(t *testing.T)
```

Use `httptest.Server` to simulate an OpenAI-compatible chat completions endpoint.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/bastion -run TestLLMPolicy -v
```

Expected: LLM client does not exist.

- [ ] **Step 3: Implement LLM client**

Implement a small HTTP client that posts chat completions with configured base URL, API key, model, and prompt. Parse assistant content as JSON with `allow` and `reason`. Treat request errors and parse errors as deny.

- [ ] **Step 4: Wire policy API**

Allow policy create/update payloads to include LLM config fields. Store API key encrypted like target secret material.

- [ ] **Step 5: Run tests and commit**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/bastion ./internal/server -run "TestLLMPolicy|TestAPI" -v
go test ./...
git add internal/bastion internal/server internal/store
git commit -m "feat: add llm-backed command policy review"
```

---

### Task 9: Embedded Management Frontend

**Files:**
- Create: `web/index.html`
- Create: `web/styles.css`
- Create: `web/api.js`
- Create: `web/components.js`
- Create: `web/app.js`
- Create: `internal/server/web_embed.go`
- Create: `internal/server/web_embed_test.go`
- Modify: `internal/server/http.go`

- [ ] **Step 1: Write static serving test**

Create:

```go
func TestWebAppServesIndexAndStaticAssets(t *testing.T)
```

The test requests `/`, `/app.js`, `/styles.css`, and an unknown non-API path such as `/targets`, asserting the SPA entrypoint is served for `/targets`.

- [ ] **Step 2: Run test to verify failure**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestWebApp -v
```

Expected: embedded UI serving not implemented.

- [ ] **Step 3: Implement static UI**

Build a plain ES-module frontend with:

- auth screens
- organization create/join controls
- public key table and form
- target table and create form for direct and agent targets
- agent enrollment command view
- organization user group management
- policy editor with blacklist, whitelist, default action, LLM fields, target bindings, and user group bindings
- audit log table

Use accessible forms and table-first operational layouts, but make the interface feel polished, spacious, and lively: layered surfaces, confident spacing, responsive hover/focus states, subtle transitions, clear status badges, and thoughtful empty/loading/error states. Keep CSS palette restrained and not dominated by one hue.

- [ ] **Step 4: Embed and serve UI**

Use `//go:embed ../../web/*` only if the path compiles from package location; otherwise place embed in a package that can legally reference `web/*`. Register static routes after API and legacy routes so `/api/*`, `/install/*`, `/run.sh`, `/run.ps1`, `/download/*`, and `/ws/agent` keep precedence.

- [ ] **Step 5: Run tests and commit**

Before committing, review `web/*.html`, `web/*.css`, and `web/*.js` with the available `web-design-guidelines` skill. Fix actionable issues that affect layout, accessibility, interaction clarity, or visual polish.

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestWebApp -v
go test ./...
git add web internal/server
git commit -m "feat: add embedded bastion management ui"
```

---

### Task 10: Complete E2E Test

**Files:**
- Create: `internal/server/e2e_bastion_test.go`
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] **Step 1: Write full e2e test**

Create:

```go
func TestBastionE2E(t *testing.T)
```

The test must:

1. Start app with temp SQLite and temp host key.
2. Register a user through HTTP.
3. Add the user's SSH public key through HTTP.
4. Create an organization through HTTP.
5. Assert the default all-members user group exists and includes the creator.
6. Start a local in-process SSH target server.
7. Add direct target alias `test2`.
8. SSH to bastion with public-key auth and `User: "test2"`.
9. Execute a command and assert output.
10. Query `/api/audit` and assert command, target, user, allow decision, and exit code.
11. Attach blacklist policy to `test2` and the default group, and assert denied command exits 126 and writes denied audit.
12. Attach whitelist rule and assert allowed command succeeds.
13. Create agent enrollment, start agent with token, assert a normal agent-backed target is created, rename its alias, execute through it, and assert output plus audit.
14. Request `/` and assert the management UI entrypoint is served.

- [ ] **Step 2: Run e2e test to verify failure or pass depending on prior tasks**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./internal/server -run TestBastionE2E -v
```

Expected after prior tasks: PASS. If it fails, fix the failing product behavior rather than weakening assertions.

- [ ] **Step 3: Document run commands**

Update READMEs with:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
go test ./internal/server -run TestBastionE2E -v
```

Also document first-run server flags for database path, secret key path, HTTP listen, SSH listen, and agent enrollment scripts.

- [ ] **Step 4: Run full verification**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
```

Expected: PASS, including `TestBastionE2E`.

- [ ] **Step 5: Commit**

Run:

```powershell
git add internal/server/e2e_bastion_test.go README.md README.zh-CN.md
git commit -m "test: add bastion e2e coverage"
```

---

### Task 11: Final Hardening and Completion Audit

**Files:**
- Modify as needed based on audit findings.

- [ ] **Step 1: Run static searches for legacy-only behavior**

Run:

```powershell
rg -n "NoClientAuth|ssh UUID|UUID@|AgentToken|EnrollmentToken|organization_user_groups|policy_user_groups|command_audit_logs|policy_rules|llm_policy_configs" .
```

Expected:

- `NoClientAuth` does not appear in active SSH server config.
- `ssh UUID` and `UUID@` only appear as legacy documentation or tests that explicitly verify compatibility.
- enrollment, audit, policies, and LLM tables are present in implementation and tests.
- organization user groups and policy user-group bindings are present in implementation and tests.

- [ ] **Step 2: Run complete tests**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Manually verify build commands**

Run:

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go build ./cmd/gosshd-server
go build ./cmd/gosshd-agent
```

Expected: both binaries build.

- [ ] **Step 4: Inspect git status**

Run:

```powershell
git status --short --branch
```

Expected: clean working tree.

- [ ] **Step 5: Final completion audit**

Map objective requirements to evidence:

- SQLite: schema and store tests.
- Organization/user system: API tests and e2e setup.
- login/create/join org: API tests and e2e.
- organization user groups: default group repository/API tests, custom group API tests, and e2e default group assertions.
- user public key: API tests and SSH auth test.
- direct and agent SSH services with alias: SSH tests and e2e.
- agent-enrolled clients as normal targets: enrollment websocket/API tests, target rename test, and e2e.
- `ssh test2@public-ip` semantics: SSH tests using `User: "test2"`.
- command recording: audit API assertions and e2e.
- command security groups: policy tests and e2e allow/deny, including policy binding to one or more user groups.
- LLM path: LLM unit tests and policy integration tests.
- engineered frontend/backend components: web serving test and API tests.
- complete e2e: `TestBastionE2E`.

- [ ] **Step 6: Commit hardening changes if any**

If Step 1 through Step 5 required code or docs changes, run:

```powershell
git add .
git commit -m "chore: harden bastion completion checks"
```
