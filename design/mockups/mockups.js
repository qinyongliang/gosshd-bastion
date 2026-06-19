const en = {
  lang: "EN",
  login: "Login",
  register: "Register",
  email: "Email",
  password: "Password",
  displayName: "Display name",
  signIn: "Sign in",
  createAccount: "Create account",
  dingtalk: "Continue with DingTalk",
};

const pages = [
  appAuth(),
  appDashboard(),
  appOrganizations(),
  appMembers(),
  appKeys(),
  appTargets(),
  appAgents(),
  appPolicies(),
  appAudit(),
  appSystemAdmin(),
  siteHome("en"),
  siteHome("zh"),
  siteDocs("en"),
  siteDocs("zh"),
];

document.querySelector("#mockups").innerHTML = pages.join("");

function appShell(id, title, body, active = "dashboard") {
  const nav = [
    ["dashboard", "Control", "控制台"],
    ["orgs", "Orgs", "组织"],
    ["members", "Members", "成员"],
    ["keys", "Keys", "密钥"],
    ["targets", "Services", "服务"],
    ["agents", "Agents", "Agent"],
    ["policies", "Policies", "策略"],
    ["audit", "Audit", "审计"],
    ["admin", "Admin", "管理"],
  ];
  return `<section class="mockup" id="${id}">
    <div class="console">
      <aside class="sidebar">
        <div class="brand"><div class="mark">g</div><span>gosshd</span></div>
        <div class="user-block"><strong>Qinyong Liang</strong><span>admin / qyl@example.com</span><span class="badge">System admin</span></div>
        <div class="nav">${nav.map(([key, enLabel, zhLabel]) => `<span class="${key === active ? "active" : ""}">◇ ${enLabel}<small>${zhLabel}</small></span>`).join("")}</div>
        <div class="orgs">
          <div class="org-btn active"><span>吉时雨</span><small>owner</small></div>
          <div class="org-btn"><span>Personal</span><small>个人</small></div>
        </div>
      </aside>
      <section class="workspace">
        <header class="topbar">
          <div><p class="eyebrow">AI service bastion</p><h1>${title}</h1><span class="context">吉时雨 / 测试环境</span></div>
          <div class="actions"><div class="lang"><span>EN</span><span class="active">中文</span></div><span class="badge green">Shared owner</span><span class="button ghost-button">Invite</span></div>
        </header>
        ${hudLine()}
        ${body}
      </section>
    </div>
  </section>`;
}

function appAuth() {
  return `<section class="mockup" id="app-auth">
    <div class="auth-grid">
      <div class="hero-copy">
        <div class="brand"><div class="mark">g</div><span>gosshd bastion</span></div>
        <h1>AI-ready SSH access with command policy in the path.</h1>
        <p>Organizations, SSH aliases, agent enrollment, command security groups, audit, and MCP automation live together.</p>
        ${orbitMap()}
      </div>
      <div class="auth-card">
        <div class="actions" style="justify-content: space-between; margin-bottom: 18px;"><div class="lang"><span class="active">EN</span><span>中文</span></div><span class="badge">Auto: system language</span></div>
        <div class="tabs"><div class="tab">Register</div><div class="tab active">Login</div></div>
        <label class="field">Email<div class="input"></div></label>
        <label class="field">Password<div class="input"></div></label>
        <div class="button" style="width: 100%; margin-top: 18px;">Sign in</div>
        <div class="sso"><p class="eyebrow">DingTalk Login</p><div class="button ghost-button">Continue with DingTalk</div></div>
      </div>
    </div>
  </section>`;
}

function appDashboard() {
  return appShell("app-dashboard", "Control plane / 控制台", `
    <div class="signal-panel">
      <div class="panel"><h2>Live access topology</h2><p>Public key identity, alias routing, policy decision, and audit sealing in one path.</p>${orbitMap()}</div>
      <div class="panel"><h2>Policy stream</h2><p>Realtime command decisions for operators and AI automation.</p>${streamList()}</div>
    </div>
    <div class="metrics"><div class="metric"><span>SSH services</span><strong>18</strong></div><div class="metric"><span>Policies</span><strong>7</strong></div><div class="metric"><span>User groups</span><strong>5</strong></div><div class="metric"><span>Audit rows</span><strong>248</strong></div></div>
    <div class="grid two"><div class="panel"><h2>Operating context</h2><p>Current organization and access role.</p><div class="table"><div class="row"><div class="cell">Scope</div><div class="cell">Role</div></div><div class="row"><div class="cell"><strong>吉时雨</strong></div><div class="cell"><span class="badge green">owner</span></div></div></div></div><div class="panel"><h2>Fast path</h2><p>Frequently used control actions.</p><div class="grid two"><div class="button">Manage SSH services</div><div class="button">Create agent enrollment</div><div class="button ghost-button">Bind policies</div><div class="button ghost-button">Review audit</div></div></div></div>
    <div class="panel" style="margin-top:18px"><h2>Recent command decisions</h2>${auditTable()}</div>
  `, "dashboard");
}

function appOrganizations() {
  return appShell("app-organizations", "Organizations / 组织", `
    <div class="grid two"><div class="panel"><h2>Create organization</h2><p>New organizations receive a default all-members group.</p><div class="field">Organization name<div class="input"></div></div><div class="field">Slug<div class="input"></div></div><div class="button" style="margin-top:14px">Create organization</div></div><div class="panel"><h2>Join organization</h2><p>Use an invite code from an owner or admin.</p><div class="field">Invite code<div class="input"></div></div><div class="button" style="margin-top:14px">Join organization</div></div></div>
    <div class="panel" style="margin-top:18px"><h2>Your organizations</h2><div class="table"><div class="row"><div class="cell">Name</div><div class="cell">Slug</div><div class="cell">Role</div></div><div class="row"><div class="cell"><strong>吉时雨</strong><small>shared</small></div><div class="cell">jsy</div><div class="cell">owner</div></div><div class="row"><div class="cell"><strong>Personal</strong><small>个人</small></div><div class="cell">qyl</div><div class="cell">owner</div></div></div></div>
  `, "orgs");
}

function appMembers() {
  return appShell("app-members", "Organization members / 成员与用户组", `
    <div class="grid two"><div class="panel"><h2>Add member</h2><p>Add by email or user id and assign a role.</p><div class="form-grid"><div class="input"></div><div class="select"></div></div><div class="button" style="margin-top:14px">Add member</div></div><div class="panel"><h2>User groups</h2><p>Bind command policies to groups.</p><div class="chips"><span class="chip active">All Members 默认全员</span><span class="chip">Read Only</span><span class="chip">Ops</span></div><div class="field">New group<div class="input"></div></div></div></div>
    <div class="panel" style="margin-top:18px"><h2>Members</h2><div class="table" style="--cols:1.4fr .8fr 1.2fr .6fr"><div class="row"><div class="cell">User</div><div class="cell">Role</div><div class="cell">Set role</div><div class="cell">Action</div></div><div class="row"><div class="cell"><strong>Ada</strong><small>ada@example.com</small></div><div class="cell"><span class="badge green">owner</span></div><div class="cell"><div class="select" style="width:160px"></div></div><div class="cell"></div></div><div class="row"><div class="cell"><strong>Agent Runner</strong><small>runner@example.com</small></div><div class="cell">member</div><div class="cell"><div class="select" style="width:160px"></div></div><div class="cell"><span class="badge red">Remove</span></div></div></div></div>
  `, "members");
}

function appKeys() {
  return appShell("app-keys", "Public keys / SSH 公钥", `
    <div class="panel"><h2>Add public key</h2><p>Keys identify users at SSH login before alias resolution.</p><div class="field">Key name<div class="input"></div></div><div class="field">Authorized key<div class="textarea"></div></div><div class="button" style="margin-top:14px">Add key</div></div>
    <div class="panel" style="margin-top:18px"><h2>Configured keys</h2><div class="table" style="--cols:1fr 2fr .6fr"><div class="row"><div class="cell">Name</div><div class="cell">Fingerprint</div><div class="cell"></div></div><div class="row"><div class="cell">Laptop</div><div class="cell">SHA256:jD6oa7s1...</div><div class="cell"><span class="badge red">Remove</span></div></div></div></div>
  `, "keys");
}

function appTargets() {
  return appShell("app-targets", "SSH services / SSH 服务", `
    <div class="panel"><h2>Add SSH service</h2><p>Direct and agent-enrolled clients share the same target model.</p><div class="form-grid"><div class="input"></div><div class="input"></div><div class="input"></div><div class="select"></div><div class="input"></div><div class="input"></div></div><div class="button" style="margin-top:14px">Add service</div></div>
    <div class="chips" style="margin:16px 0"><span class="chip active">测试环境</span><span class="chip">GPU</span><span class="chip">prod-ai</span></div>
    <div class="panel"><h2>SSH services</h2><div class="table" style="--cols:1.2fr .8fr 1.2fr .8fr 1fr 1.4fr"><div class="row"><div class="cell">Service</div><div class="cell">Alias</div><div class="cell">Endpoint</div><div class="cell">Auth</div><div class="cell">Tags</div><div class="cell">Rename</div></div><div class="row"><div class="cell"><strong>Test2 server</strong><small>direct</small></div><div class="cell">test2</div><div class="cell">root@10.0.0.12:22</div><div class="cell">private key</div><div class="cell"><span class="chip">测试环境</span></div><div class="cell"><div class="input"></div></div></div></div></div>
  `, "targets");
}

function appAgents() {
  return appShell("app-agents", "Agent SSH enrollment / Agent 接入", `
    <div class="grid two"><div class="panel"><h2>Create enrollment</h2><p>The generated commands include a scoped token.</p><div class="field">Agent service alias<div class="input"></div></div><div class="field">Default host<div class="input"></div></div><div class="button" style="margin-top:14px">Create enrollment</div></div><div class="panel"><h2>Startup install</h2><p>Linux registers with systemctl. Windows registers with sc.exe.</p><span class="badge green">Agent becomes a normal renameable SSH service</span></div></div>
    <div class="panel" style="margin-top:18px"><h2>Generated commands</h2><div class="command"><span>Linux/macOS</span><code>curl -fsSL http://host/install/&lt;token&gt;.sh | sh</code><span>Copy</span></div><div class="command"><span>Windows</span><code>irm http://host/install/&lt;token&gt;.ps1 | iex</code><span>Copy</span></div><div class="command"><span>systemctl</span><code>curl -fsSL http://host/install/&lt;token&gt;.sh | sudo sh -s -- install</code><span>Copy</span></div><div class="command"><span>sc.exe</span><code>powershell -File gosshd-agent-install.ps1 -Install</code><span>Copy</span></div></div>
  `, "agents");
}

function appPolicies() {
  return appShell("app-policies", "Command security groups / 命令安全组", `
    <div class="panel"><h2>Create policy</h2><p>Bind policies to services, tags, and user groups. Unmatched commands can route to LLM review.</p><div class="form-grid"><div class="input"></div><div class="select"></div><div class="select"></div><div class="select"></div></div><div class="button" style="margin-top:14px">Create policy</div></div>
    <div class="grid four" style="margin-top:18px"><div class="panel"><h2>Rules</h2><p>Whitelist or blacklist patterns.</p><div class="button ghost-button">Add rule</div></div><div class="panel"><h2>Targets</h2><p>Bind single SSH services.</p><div class="button ghost-button">Bind target</div></div><div class="panel"><h2>Tags</h2><p>Bind all services with a tag.</p><div class="button ghost-button">Bind tag</div></div><div class="panel"><h2>Groups</h2><p>Bind one or more user groups.</p><div class="button ghost-button">Bind group</div></div></div>
    <div class="grid two" style="margin-top:18px"><div class="panel"><h2>LLM configs</h2><div class="field">deepseek-flash / no reasoning<div class="input"></div></div></div><div class="panel"><h2>Prompt resources</h2><div class="chips"><span class="chip active">Default readonly prompt</span><span class="chip">Ops policy prompt</span></div></div></div>
  `, "policies");
}

function appAudit() {
  return appShell("app-audit", "Command audit / 命令审计", `
    <div class="signal-panel"><div class="panel"><h2>Audit telemetry</h2><p>Each exec request leaves a policy verdict and a sealed trace.</p>${streamList()}</div><div class="panel"><h2>Decision posture</h2><p>Readonly traffic is green. Mutating or dangerous commands are rejected before they reach the target.</p><div class="metrics" style="grid-template-columns:repeat(3,1fr);margin:0"><div class="metric"><span>allow</span><strong>231</strong></div><div class="metric"><span>deny</span><strong>17</strong></div><div class="metric"><span>llm</span><strong>42</strong></div></div></div></div>
    <div class="panel"><h2>Command decisions</h2><p>Every SSH exec decision is recorded with policy context.</p>${auditTable(true)}</div>
  `, "audit");
}

function appSystemAdmin() {
  return appShell("app-system-admin", "System administration / 系统管理", `
    <div class="grid two"><div class="panel"><h2>Global settings</h2><p>DingTalk and LDAP configuration for the whole system.</p><div class="grid two"><div><h3>DingTalk</h3><div class="field">Client ID<div class="input"></div></div><div class="field">Redirect URL<div class="input"></div></div></div><div><h3>LDAP</h3><div class="field">Server URL<div class="input"></div></div><div class="field">Base DN<div class="input"></div></div></div></div></div><div class="panel"><h2>Account management</h2><div class="table" style="--cols:1.4fr .8fr .8fr"><div class="row"><div class="cell">User</div><div class="cell">Provider</div><div class="cell">Role</div></div><div class="row"><div class="cell"><strong>admin</strong><small>admin</small></div><div class="cell">local</div><div class="cell"><span class="badge green">admin</span></div></div></div></div></div>
    <div class="panel" style="margin-top:18px"><h2>Organization management</h2><p>Inspect organizations, update roles, and transfer ownership.</p><div class="table"><div class="row"><div class="cell">Organization</div><div class="cell">Members</div><div class="cell">Owner</div><div class="cell">Repair</div></div><div class="row"><div class="cell">吉时雨</div><div class="cell">12</div><div class="cell">Ada</div><div class="cell"><span class="badge amber">Transfer</span></div></div></div></div>
  `, "admin");
}

function auditTable(wide = false) {
  return `<div class="table" style="--cols:${wide ? "1.4fr .7fr 1.3fr .5fr 1fr" : "1.3fr .8fr 1.4fr"}"><div class="row"><div class="cell">Command</div><div class="cell">Decision</div><div class="cell">Reason</div>${wide ? '<div class="cell">Exit</div><div class="cell">Started</div>' : ""}</div><div class="row"><div class="cell"><code>whoami</code></div><div class="cell"><span class="badge green">allow</span></div><div class="cell">readonly whitelist</div>${wide ? '<div class="cell">0</div><div class="cell">10:42</div>' : ""}</div><div class="row"><div class="cell"><code>rm -rf /</code></div><div class="cell"><span class="badge red">deny</span></div><div class="cell">blacklist match</div>${wide ? '<div class="cell"></div><div class="cell">10:44</div>' : ""}</div></div>`;
}

function hudLine() {
  return `<div class="hud-line"><span class="hud-pill"><i class="hud-dot"></i>SSH ingress online</span><span class="hud-pill">policy latency 38ms</span><span class="hud-pill">LLM guard armed</span></div>`;
}

function orbitMap() {
  return `<div class="orbit-map">
    <div class="orbit-ring"></div><div class="orbit-ring"></div><div class="orbit-ring"></div>
    <div class="orbit-path p1"></div><div class="orbit-path p2"></div><div class="orbit-path p3"></div>
    <div class="orbit-node n1">AI agent</div><div class="orbit-node n2">public key</div><div class="orbit-node n3">target</div><div class="orbit-node n4">audit</div>
    <div class="orbit-core">BASTION</div>
  </div>`;
}

function streamList() {
  return `<div class="stream-list">
    <span><b>identity</b> public-key: qyl-laptop</span>
    <span><b>route</b> alias test2 -> 10.0.0.12</span>
    <span><b>policy</b> readonly group matched</span>
    <span><b>llm</b> deepseek-flash: allow</span>
    <span><b>audit</b> row sealed / exit 0</span>
  </div>`;
}

function siteHome(lang) {
  const zh = lang === "zh";
  return `<section class="mockup site" id="site-home-${lang}">
    ${siteHeader(zh, "home")}
    <section class="site-hero">
      <div><p class="eyebrow">${zh ? "为 AI 服务而生的堡垒机" : "Bastion for AI services"}</p><h1>GOSSHD Bastion</h1><p>${zh ? "让 AI Agent、运维人员和自动化任务通过 SSH 别名、命令策略和完整审计安全访问私有服务。" : "A quiet sci-fi access plane where AI agents, operators, and automation reach private SSH services through aliases, policy checks, and audit trails."}</p><div class="actions"><span class="button">${zh ? "阅读文档" : "Read docs"}</span><span class="button ghost-button">${zh ? "下载发布包" : "Download release"}</span></div>${hudLine()}</div>
      <div class="mission"><p class="eyebrow">policy stream active</p>${orbitMap()}<div class="trace"><span>public key → user: ai-runner</span><span>alias → inference-gpu</span><span>group policy → production-ai</span><span>LLM verdict → allow with watch</span><span>audit row sealed → exit 0</span></div></div>
    </section>
    <section class="site-band"><div><p class="eyebrow">${zh ? "控制面" : "Control plane"}</p><h2>${zh ? "人类运维和自治 AI 共用一个网关。" : "One gateway for human operators and autonomous AI work."}</h2><p>${zh ? "保留熟悉的 SSH 接口，把身份、目标路由、命令策略和审计收束到 SQLite 控制面。" : "Keep SSH familiar while identity, target routing, command policy, and audit move into a SQLite-backed control plane."}</p></div><div class="grid four"><div class="feature-card"><h3>Identity</h3><p>Public key maps user.</p></div><div class="feature-card"><h3>Agents</h3><p>Private hosts enroll.</p></div><div class="feature-card"><h3>Policy</h3><p>Rules and LLM review.</p></div><div class="feature-card"><h3>Audit</h3><p>Every exec decision.</p></div></div></section>
  </section>`;
}

function siteDocs(lang) {
  const zh = lang === "zh";
  return `<section class="mockup" id="site-docs-${lang}">
    ${siteHeader(zh, "docs")}
    <main class="docs">
      <aside class="docs-side"><div class="brand"><div class="mark">GB</div><span>Docs</span></div><span>Overview</span><span>Quickstart</span><span>Identity</span><span>SSH targets</span><span>Agents</span><span>Policies</span><span>Audit</span></aside>
      <article class="docs-content"><section class="doc-hero"><p class="eyebrow">${zh ? "文档" : "Documentation"}</p><h1>${zh ? "在不给自动化盲目 shell 的前提下运营 SSH 访问。" : "Operate SSH access for AI services without giving automation a blind shell."}</h1><p>${zh ? "单个 Go 服务、SQLite、嵌入式控制台和 SSH 入口，组成可部署的访问控制面。" : "A single Go server with SQLite storage, an embedded console, and an SSH endpoint form one deployable access plane."}</p></section><section class="doc-section"><h2>${zh ? "快速开始" : "Quickstart"}</h2><div class="code">gosshd-server --http-listen :80 --ssh-listen :22</div><div class="code" style="margin-top:10px">ssh test2@public-host</div></section><section class="doc-section"><h2>${zh ? "Agent 安装" : "Agent install"}</h2><p>${zh ? "控制台生成带 token 的 Linux、Windows 和开机启动命令。" : "The console generates token-scoped Linux, Windows, and startup-service commands."}</p><div class="command"><span>systemctl</span><code>curl ... | sudo sh -s -- install</code><span>Copy</span></div></section></article>
    </main>
  </section>`;
}

function siteHeader(zh, active) {
  return `<header class="site-header"><div class="brand"><div class="mark">GB</div><span>GOSSHD Bastion</span></div><nav class="site-nav"><span>${zh ? "架构" : "Architecture"}</span><span>${zh ? "安全" : "Security"}</span><span>${zh ? "文档" : "Docs"}</span><div class="lang"><span class="${zh ? "" : "active"}">EN</span><span class="${zh ? "active" : ""}">中文</span></div></nav></header>`;
}
