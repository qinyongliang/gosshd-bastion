# GOSSHD Bastion · Product brief

> One way in. Clear boundaries throughout.

Updated: October 11, 2026

Delivery: Self-hosted software

Website: https://qinyongliang.github.io/gosshd-bastion/

Source: https://github.com/qinyongliang/gosshd-bastion

Download: https://github.com/qinyongliang/gosshd-bastion/releases/latest

## One-line pitch

GOSSHD Bastion is a remote operations workspace for people and AI, combining SSH terminals, file management, private nodes and TCP tunnels with access policies, command review and audit replay.

## Short introduction

Remote operations should not require constant switching between terminals, file tools, tunnel settings and access records. GOSSHD Bastion brings those workflows into a self-hosted workspace. Teams troubleshoot, edit configurations and transfer files in the browser; AI agents use standard SSH or MCP for inspection and assisted troubleshooting. The gateway manages target credentials. Organizations, user groups and policies govern access, SSH exec commands go through rules and optional LLM review, and command audits plus enabled session recordings provide a trail for investigation.

## A 30-second walkthrough

- **One workspace:** Web terminals, file editing, batch file actions and service tunnels.
- **People and agents:** Browser access for everyday work, SSH for existing tools, MCP for AI integrations.
- **Explicit boundaries:** Bind targets, tags and user groups; check source IPs and capability settings.
- **Traceable work:** Inspect command decisions, exit codes, file transfer audits and recorded interactive sessions.
- **Your deployment:** Manage the control plane, target credentials and audit storage; configure an optional LLM endpoint yourself.

## Capability overview

| Capability | What it does | Important boundary |
| --- | --- | --- |
| Web workspace | Remote terminals, file editor, adjustable layout and desktop/mobile access | Marketing visuals are illustrative; deploy the real console yourself |
| File management | Recursive folder uploads; multi-selection; batch copy, move, download and delete | Web uploads retain a 1 GiB limit; downloads depend on browser capabilities |
| Transfer paths | Try encrypted browser-to-Agent WebRTC; relay when unavailable | Direct connectivity is not guaranteed; individual file authorization and auditing still apply |
| Private nodes | Agents connect outbound to the gateway; existing SSH hosts can also be added | Configure network reachability, targets and credentials |
| TCP tunnels | Manage entry, exit, destination, lifetime, connections, traffic and source IPs | SSH forwarding, external listeners and firewall settings still apply |
| Access governance | Organizations, groups, targets, tags, source IPs and capability controls | Validate policies and apply least privilege |
| Command review | SSH exec blocklists/allowlists and optional LLM review; review errors fail closed | Not a complete shell sandbox or a guarantee of detecting every dangerous operation |
| Temporary access | Service-specific, expiring SSH grants; expiry/deletion closes connections | UUIDs are sensitive credentials; policies and audit identity follow the creator |
| Audits and replay | Commands, decisions, exit codes, file audits and enabled interactive recordings | Not every operation has a replay; configure recording and storage |
| MCP | Token tool groups for terminal and tunnel operations | Account and organization permissions still apply |

## Three practical use cases

### Operations and development

Inspect logs, edit configurations, transfer deployment files and reach private services. Keep common tasks in one browser workspace.

### AI-assisted operations

Agents use SSH or MCP for inspection and assisted troubleshooting. Callers authenticate with their own SSH identity or MCP token, instead of holding target credentials directly. Actual rules and optional model configuration determine decisions.

### Private and customer networks

Enroll datacenter, VPC and on-site machines with private nodes. Separate targets and members by organization; use direct or relayed connections as the network allows.

## Getting started

1. Download a server package for your platform and follow the README.
2. Configure public addresses, an administrator and production HTTPS. Protect databases and keys.
3. Add a test SSH host or enroll a private node using the console-generated installer.
4. Define a target alias, user groups and policies. Test allowed/denied commands and audit records.
5. Connect team members and AI tools, then expand gradually.

Example (replace host, port and target alias; configure the caller’s SSH identity):

```sh
ssh -p 22022 prod-api@bastion.example.com "uptime"
```

## Ready-to-share announcement

**Title:** One way in: a shared remote operations workspace for people and AI

**Copy:** Open a terminal, edit configurations, move files in batches and reach private services in one self-hosted workspace. GOSSHD Bastion supports browsers, standard SSH and MCP, bringing organization permissions, command rules, optional AI review and audit replay into everyday operations. People and AI share an entry point; your team keeps the boundaries. Start with one test machine and grow into team and automation workflows.

## Messaging boundaries

- Do not promise absolute safety, guaranteed direct connectivity, zero latency or unlimited file sizes.
- Do not describe SSH exec review as a complete shell sandbox.
- Do not confuse centralized target credentials with callers needing no identity.
- Do not present temporary grants as publicly shareable unauthenticated links.
- Do not claim a hosted-service SLA, unverified customer counts, performance results or compliance certifications.

Full deployment guide: https://github.com/qinyongliang/gosshd-bastion/blob/main/README.md
