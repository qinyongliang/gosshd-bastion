import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import {
  Activity,
  Copy,
  KeyRound,
  LayoutDashboard,
  ListChecks,
  LockKeyhole,
  Menu,
  Plus,
  Search,
  Server,
  Settings,
  Shield,
  Users,
  X,
} from "lucide-react";
import { ComponentType, ReactNode, useMemo, useState } from "react";
import { Link, Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ApiError, api, type Enrollment } from "./api";
import { dateLocale, useI18n } from "./i18n";
import { useTheme } from "./theme";
import type { AdminOrg, AdminUser, AuditLog, LLMConfig, Member, Organization, Policy, PromptResource, PublicKey, Runtime, Target, User, UserGroup } from "./types";
import { copyText, ownerFromOrg, splitTags, tagColor, targetEndpoint } from "./utils";

type ConsoleData = {
  user: User;
  orgs: Organization[];
  activeOrg: Organization;
  setActiveOrgID: (id: string) => void;
  runtime: Runtime;
  keys: PublicKey[];
  members: Member[];
  groups: UserGroup[];
  targets: Target[];
  policies: Policy[];
  llms: LLMConfig[];
  prompts: PromptResource[];
  auditPage: { logs: AuditLog[]; total: number; page: number; page_size: number };
  refetchAll: () => void;
};

const activeOrgStorage = "gosshd_active_org";

export function App() {
  const providers = useQuery({ queryKey: ["providers"], queryFn: api.authProviders });
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });

  if (me.isLoading || providers.isLoading) return <Loading />;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <AuthPage dingTalkEnabled={Boolean(providers.data?.dingtalk?.enabled)} />;
  }
  if (me.error) return <Fatal error={me.error} />;
  if (!me.data) return <Loading />;
  return <ConsoleApp user={me.data.user} orgs={me.data.organizations} runtime={me.data.runtime} />;
}

function AuthPage({ dingTalkEnabled }: { dingTalkEnabled: boolean }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [error, setError] = useState("");
  const mutation = useMutation({
    mutationFn: (data: Record<string, string>) => mode === "login" ? api.login(data) : api.register(data),
    onSuccess: async () => {
      setError("");
      await queryClient.invalidateQueries();
    },
    onError: (err) => setError(localizeError(err, t)),
  });

  return (
    <section className="auth-screen">
      <div className="brand-panel">
        <div className="brand-row"><div className="mark">g</div><span>gosshd</span></div>
        <h1>AI 服务堡垒机</h1>
        <p>为自动化任务和运维人员提供 SSH 别名访问、命令安全组和完整审计。</p>
      </div>
      <div className="auth-card">
        <div className="auth-card-head">
          <Segmented value={theme} items={[["dark", "黑", "Black"], ["light", "白", "White"]]} onChange={(value) => setTheme(value as "light" | "dark")} />
          <Segmented value={locale} items={[["en", "EN", "EN"], ["zh-CN", "中文", "中文"]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          <span className="badge info">Auto</span>
        </div>
        <div className="tabs" role="tablist" aria-label="Auth mode">
          <button type="button" role="tab" aria-selected={mode === "register"} className={clsx(mode === "register" && "active")} onClick={() => setMode("register")}>{t("register")}</button>
          <button type="button" role="tab" aria-selected={mode === "login"} className={clsx(mode === "login" && "active")} onClick={() => setMode("login")}>{t("login")}</button>
        </div>
        <form className="stack" onSubmit={(event) => formSubmit(event, (data) => mutation.mutate(data))}>
          <Field label="Email" name="email" type={mode === "login" ? "text" : "email"} required />
          {mode === "register" && <Field label="Display name" name="display_name" required />}
          <Field label="Password" name="password" type="password" required />
          <button className="primary" type="submit" disabled={mutation.isPending}>
            {mode === "login" ? <KeyRound /> : <Plus />}
            {mode === "login" ? "Sign in" : "Create account"}
          </button>
        </form>
        <div className="sso-zone">
          <span>DingTalk</span>
          {dingTalkEnabled ? <a className="button-link" href="/api/auth/dingtalk/start?redirect_after=/">Continue</a> : <button type="button" className="ghost" disabled>Disabled</button>}
        </div>
        {error && <div className="status error">{error}</div>}
      </div>
    </section>
  );
}

function ConsoleApp({ user, orgs, runtime }: { user: User; orgs: Organization[]; runtime: Runtime }) {
  const [activeOrgID, setActiveOrgIDState] = useState(() => window.localStorage.getItem(activeOrgStorage) || "");
  const activeOrg = orgs.find((org) => org.id === activeOrgID) || orgs[0];
  const owner = ownerFromOrg(activeOrg);
  const queryClient = useQueryClient();
  const keys = useQuery({ queryKey: ["keys"], queryFn: api.keys });
  const targets = useQuery({ queryKey: ["targets", owner], queryFn: () => api.targets(owner!), enabled: Boolean(owner) });
  const members = useQuery({ queryKey: ["members", activeOrg?.id], queryFn: () => api.orgMembers(activeOrg.id), enabled: Boolean(activeOrg) });
  const groups = useQuery({ queryKey: ["groups", activeOrg?.id], queryFn: () => api.groups(activeOrg.id), enabled: Boolean(activeOrg) });
  const policies = useQuery({ queryKey: ["policies", owner], queryFn: () => api.policies(owner!), enabled: Boolean(owner) });
  const llms = useQuery({ queryKey: ["llms", owner], queryFn: () => api.llmConfigs(owner!), enabled: Boolean(owner) });
  const prompts = useQuery({ queryKey: ["prompts", owner], queryFn: () => api.prompts(owner!), enabled: Boolean(owner) });
  const audit = useQuery({ queryKey: ["audit", activeOrg?.id], queryFn: () => api.audit({ page: 1, page_size: 20 }), enabled: Boolean(activeOrg) });

  if (!activeOrg) return <Fatal error={new Error("No organization available")} />;

  const data: ConsoleData = {
    user,
    orgs,
    activeOrg,
    setActiveOrgID(id) {
      window.localStorage.setItem(activeOrgStorage, id);
      setActiveOrgIDState(id);
    },
    runtime,
    keys: keys.data?.keys || [],
    members: members.data?.members || [],
    groups: groups.data?.groups || [],
    targets: targets.data?.targets || [],
    policies: policies.data?.policies || [],
    llms: llms.data?.configs || [],
    prompts: prompts.data?.prompts || [],
    auditPage: { total: audit.data?.total || 0, page: audit.data?.page || 1, page_size: audit.data?.page_size || 20, logs: audit.data?.logs || [] },
    refetchAll: () => void queryClient.invalidateQueries(),
  };

  return <Shell data={data} />;
}

function Shell({ data }: { data: ConsoleData }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const logout = useMutation({
    mutationFn: api.logout,
    onSuccess: async () => {
      await queryClient.clear();
      navigate("/");
      window.location.reload();
    },
  });
  const nav: Array<[string, string, ComponentType<{ className?: string }>]> = [
    ["/", t("dashboard"), LayoutDashboard],
    ["/orgs", t("orgs"), Users],
    ["/org-admin", t("members"), Users],
    ["/keys", t("keys"), KeyRound],
    ["/targets", t("services"), Server],
    ["/policies", t("commandPolicy"), Shield],
    ["/audit", t("audit"), ListChecks],
  ];
  if (data.user.is_system_admin) nav.push(["/system-admin", t("settings"), Settings]);

  return (
    <section className="console">
      <aside className={clsx("sidebar", sidebarOpen && "open")}>
        <div className="brand-row"><div className="mark">g</div><strong>gosshd</strong></div>
        <div className="sidebar-user">
          <strong>{data.user.display_name || data.user.email}</strong>
          <span>{data.user.email}</span>
          {data.user.is_system_admin && <span className="pill">{t("admin")}</span>}
        </div>
        <nav className="side-nav">
          {nav.map(([to, label, Icon]) => <NavButton key={to} to={to} label={label} icon={<Icon />} onClick={() => setSidebarOpen(false)} />)}
        </nav>
        <OrgSwitcher data={data} />
        <button type="button" onClick={() => logout.mutate()}><LockKeyhole />{t("logout")}</button>
      </aside>
      {sidebarOpen && <button className="sidebar-backdrop" aria-label="Close menu" onClick={() => setSidebarOpen(false)} />}
      <section className="workspace">
        <header className="topbar">
          <button className="mobile-menu" type="button" onClick={() => setSidebarOpen(true)}><Menu /></button>
          <div>
            <small>AI 服务堡垒机</small>
            <h1>{pageTitle()}</h1>
            <span>{data.activeOrg.name}</span>
          </div>
          <div className="topbar-actions">
            <Segmented value={theme} items={[["dark", "黑", "Black"], ["light", "白", "White"]]} onChange={(value) => setTheme(value as "light" | "dark")} />
            <Segmented value={locale} items={[["en", "EN", "EN"], ["zh-CN", "中文", "中文"]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          </div>
        </header>
        <div className="hud-line">
          <span className="hud-pill"><i className="hud-dot" />SSH ingress online</span>
          <span className="hud-pill">policy guard ready</span>
          <span className="hud-pill">audit isolated</span>
        </div>
        <Routes>
          <Route path="/" element={<DashboardPage data={data} />} />
          <Route path="/orgs" element={<OrganizationsPage data={data} />} />
          <Route path="/org-admin" element={<MembersPage data={data} />} />
          <Route path="/keys" element={<KeysPage data={data} />} />
          <Route path="/targets" element={<TargetsPage data={data} />} />
          <Route path="/agents" element={<Navigate to="/targets" replace />} />
          <Route path="/policies" element={<PoliciesPage data={data} />} />
          <Route path="/audit" element={<AuditPage data={data} />} />
          <Route path="/system-admin" element={data.user.is_system_admin ? <SystemAdminPage data={data} /> : <Navigate to="/" replace />} />
        </Routes>
      </section>
    </section>
  );
}

function DashboardPage({ data }: { data: ConsoleData }) {
  return (
    <>
      <div className="signal-panel">
        <Panel title="访问链路" subtitle="从 SSH 公钥身份到目标服务与审计记录的真实访问路径。">
          <div className="access-summary-grid">
            <SummaryCard index="01" title="SSH 公钥身份" body="ssh alias@public-ip" />
            <SummaryCard index="02" title="策略决策" body="匹配命令规则、来源 IP 和可选 LLM 审核" />
            <SummaryCard index="03" title="目标服务" body="直连服务器或私有节点" />
            <SummaryCard index="04" title="命令审计" body="保存命令、终端回放、决策、退出码和时间" />
          </div>
        </Panel>
        <Panel title="控制状态" subtitle="当前作用域下的资源数量。">
          <div className="summary-list">
            <span><strong>{data.keys.length}</strong><small>公钥</small></span>
            <span><strong>{data.targets.length}</strong><small>SSH 服务</small></span>
            <span><strong>{data.policies.length}</strong><small>安全组</small></span>
            <span><strong>{data.auditPage.total}</strong><small>审计记录</small></span>
          </div>
        </Panel>
      </div>
      <div className="metrics">
        <Metric icon={<Server />} label="SSH 服务" value={data.targets.length} />
        <Metric icon={<Shield />} label="策略" value={data.policies.length} />
        <Metric icon={<Users />} label="用户组" value={data.groups.length} />
        <Metric icon={<ListChecks />} label="审计记录" value={data.auditPage.total} />
      </div>
      <Panel title="最近命令决策" subtitle="通过 SSH 执行的命令会记录策略决策。">
        {data.auditPage.logs.length ? <AuditTable logs={data.auditPage.logs.slice(0, 5)} /> : <Empty title="暂无审计记录" body="暂无可展示的命令决策。" />}
      </Panel>
    </>
  );
}

function OrganizationsPage({ data }: { data: ConsoleData }) {
  const [modal, setModal] = useState<"" | "create" | "join">("");
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createOrg, onSuccess: async (out) => { data.setActiveOrgID(out.organization.id); setModal(""); await queryClient.invalidateQueries(); } });
  const join = useMutation({ mutationFn: (body: Record<string, string>) => api.joinOrg(body.code), onSuccess: async (out) => { data.setActiveOrgID(out.organization.id); setModal(""); await queryClient.invalidateQueries(); } });
  return (
    <>
      <section className="resource-head">
        <div><small>资源控制台</small><h2>组织</h2><p>创建组织或切换当前作用域。</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("join")}>加入组织</button>
          <button type="button" className="primary" onClick={() => setModal("create")}><Plus />创建组织</button>
        </div>
      </section>
      <div className="metrics">
        <Metric label="组织数" value={data.orgs.length} />
        <Metric label="共享组织" value={data.orgs.filter((item) => !item.is_personal).length} />
        <Metric label="个人组织" value={data.orgs.filter((item) => item.is_personal).length} />
      </div>
      <Panel title="你的组织" subtitle="不展示内部标识，只展示可识别的名称和角色。">
        <SimpleTable headers={["名称", "类型", "角色", "操作"]} rows={data.orgs.map((org) => [
          <strong>{org.name}</strong>,
          org.is_personal ? "个人组织" : "共享组织",
          roleText(org.role),
          <button type="button" onClick={() => data.setActiveOrgID(org.id)}>切换</button>,
        ])} />
      </Panel>
      {modal === "create" && <Modal title="创建组织" onClose={() => setModal("")}>
        <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => create.mutate(body))}>
          <Field label="组织名称" name="name" required />
          <Field label="组织标识" name="slug" required />
          <ModalActions onCancel={() => setModal("")} submit="创建组织" />
        </form>
      </Modal>}
      {modal === "join" && <Modal title="加入组织" onClose={() => setModal("")}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => join.mutate(body))}>
          <Field label="邀请码" name="code" required />
          <ModalActions onCancel={() => setModal("")} submit="加入组织" />
        </form>
      </Modal>}
    </>
  );
}

function MembersPage({ data }: { data: ConsoleData }) {
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<"role" | "name" | "newest">("role");
  const [modal, setModal] = useState<"" | "add" | "groups" | "transfer">("");
  const queryClient = useQueryClient();
  const add = useMutation({ mutationFn: (body: Record<string, string>) => api.addOrgMember(data.activeOrg.id, body), onSuccess: async () => { setModal(""); await queryClient.invalidateQueries(); } });
  const group = useMutation({ mutationFn: (body: Record<string, string>) => api.createGroup(data.activeOrg.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const update = useMutation({ mutationFn: ({ userID, role }: { userID: string; role: string }) => api.updateOrgMember(data.activeOrg.id, userID, { role }), onSuccess: async () => queryClient.invalidateQueries() });
  const transfer = useMutation({ mutationFn: (body: Record<string, string>) => api.transferOrgOwner(data.activeOrg.id, body.user_id), onSuccess: async () => { setModal(""); await queryClient.invalidateQueries(); } });
  const members = useMemo(() => sortMembers(data.members, query, sort), [data.members, query, sort]);
  return (
    <>
      <section className="resource-head">
        <div><small>组织</small><h2>组织成员</h2><p>成员支持搜索、排序、角色调整和所有权转移。</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("groups")}>用户组</button>
          <button type="button" onClick={() => setModal("transfer")}>转移所有者</button>
          <button type="button" className="primary" onClick={() => setModal("add")}><Plus />添加成员</button>
        </div>
      </section>
      <Toolbar query={query} setQuery={setQuery}>
        <select value={sort} onChange={(event) => setSort(event.target.value as typeof sort)}>
          <option value="role">角色</option>
          <option value="name">名称</option>
          <option value="newest">最新加入</option>
        </select>
      </Toolbar>
      <Panel title="成员" subtitle="所有者行不可重复设置为所有者。">
        <SimpleTable headers={["用户", "角色", "加入时间", "操作"]} rows={members.map((member) => [
          <UserCell member={member} />,
          roleText(member.role),
          formatDate(member.created_at || member.joined_at),
          member.role === "owner" ? <span className="badge info">所有者</span> : <span className="inline-actions">
            <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "admin" })}>设为管理员</button>
            <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "member" })}>设为成员</button>
          </span>,
        ])} />
      </Panel>
      {modal === "add" && <Modal title="添加成员" onClose={() => setModal("")}>
        <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => add.mutate(body))}>
          <Field label="邮箱" name="email" />
          <Field label="用户 ID" name="user_id" />
          <Select label="角色" name="role" options={[["member", "成员"], ["admin", "管理员"]]} />
          <ModalActions onCancel={() => setModal("")} submit="添加成员" />
        </form>
      </Modal>}
      {modal === "groups" && <Modal title="用户组" onClose={() => setModal("")}>
        <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => group.mutate(body))}>
          <Field label="用户组名称" name="name" required />
          <Field label="group-slug" name="slug" required />
          <ModalActions onCancel={() => setModal("")} submit="添加用户组" />
        </form>
        <SimpleTable headers={["名称", "标识"]} rows={data.groups.map((item) => [item.name, item.slug])} />
      </Modal>}
      {modal === "transfer" && <Modal title="转移所有者" onClose={() => setModal("")}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => transfer.mutate(body))}>
          <Select label="新所有者" name="user_id" options={data.members.filter((item) => item.role !== "owner").map((item) => [item.user_id, item.display_name || item.email])} />
          <ModalActions onCancel={() => setModal("")} submit="转移所有者" />
        </form>
      </Modal>}
    </>
  );
}

function KeysPage({ data }: { data: ConsoleData }) {
  const [modal, setModal] = useState(false);
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createKey, onSuccess: async () => { setModal(false); await queryClient.invalidateQueries({ queryKey: ["keys"] }); } });
  const remove = useMutation({ mutationFn: api.deleteKey, onSuccess: async () => queryClient.invalidateQueries({ queryKey: ["keys"] }) });
  return (
    <>
      <section className="resource-head">
        <div><small>SSH 身份</small><h2>公钥</h2><p>像 GitHub 一样为同一个用户添加多个 SSH 公钥。</p></div>
        <button type="button" className="primary" onClick={() => setModal(true)}><Plus />添加公钥</button>
      </section>
      <Panel title="公钥列表" subtitle="SSH 登录时先用公钥识别用户，然后堡垒机再解析目标别名。">
        {data.keys.length ? <SimpleTable headers={["名称", "指纹", "创建时间", "操作"]} rows={data.keys.map((key) => [
          <strong>{key.name}</strong>,
          <code>{key.fingerprint}</code>,
          formatDate(key.created_at),
          <button type="button" onClick={() => remove.mutate(key.id)}>删除</button>,
        ])} /> : <Empty title="暂无公钥" body="使用 SSH 别名前，请先添加一个公钥。" />}
      </Panel>
      {modal && <Modal title="添加 SSH 公钥" onClose={() => setModal(false)}>
        <form className="stack" onSubmit={(event) => formSubmit(event, (body) => create.mutate({ name: body.name, authorized_key: body.authorized_key }))}>
          <Field label="标题" name="name" required />
          <label className="field"><span>公钥</span><textarea name="authorized_key" placeholder="ssh-ed25519 AAAA..." required /></label>
          <ModalActions onCancel={() => setModal(false)} submit="添加公钥" />
        </form>
      </Modal>}
    </>
  );
}

function TargetsPage({ data }: { data: ConsoleData }) {
  const [query, setQuery] = useState("");
  const [modal, setModal] = useState(false);
  const [drawer, setDrawer] = useState<Target | null>(null);
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null);
  const filtered = data.targets.filter((target) => [target.name, target.alias, target.host, target.remote_username, ...(target.tags || [])].join(" ").toLowerCase().includes(query.toLowerCase()));
  return (
    <>
      <section className="resource-head">
        <div><small>资源控制台</small><h2>SSH 服务</h2><p>添加直连服务器或私有节点，支持标签、编辑连接信息和复制跳板机命令。</p></div>
        <button type="button" className="primary" onClick={() => setModal(true)}><Plus />添加服务</button>
      </section>
      <div className="metrics">
        <Metric label="服务总数" value={data.targets.length} />
        <Metric label="直连" value={data.targets.filter((item) => item.target_type === "direct").length} />
        <Metric label="私有节点" value={data.targets.filter((item) => item.target_type === "agent").length} />
        <Metric label="标签" value={new Set(data.targets.flatMap((item) => item.tags || [])).size} />
      </div>
      <Toolbar query={query} setQuery={setQuery} />
      <Panel title="服务列表" subtitle="">
        {filtered.length ? <SimpleTable headers={["服务", "别名", "端点", "认证", "标签", "操作"]} rows={filtered.map((target) => [
          <strong>{target.name}</strong>,
          <code>{target.alias}</code>,
          target.target_type === "agent" ? "私有节点" : targetEndpoint(target),
          target.auth_type === "private_key" ? "私钥" : "账号密码",
          <TagList target={target} />,
          <span className="inline-actions">
            <CopyButton value={`ssh -p ${data.runtime.ssh_port || 22} ${target.alias}@${data.runtime.ssh_host || location.hostname}`} />
            <button type="button" onClick={() => setDrawer(target)}>编辑</button>
          </span>,
        ])} /> : <Empty title="暂无 SSH 服务" body="添加直连目标，或注册一个私有节点。" />}
      </Panel>
      {modal && <TargetCreateModal data={data} onClose={() => setModal(false)} onEnrollment={(out) => { setModal(false); setEnrollment(out); }} />}
      {drawer && <TargetDrawer data={data} target={drawer} onClose={() => setDrawer(null)} />}
      {enrollment && <InstallDrawer enrollment={enrollment} onClose={() => setEnrollment(null)} />}
    </>
  );
}

function TargetCreateModal({ data, onClose, onEnrollment }: { data: ConsoleData; onClose: () => void; onEnrollment: (enrollment: Enrollment) => void }) {
  const [mode, setMode] = useState<"direct" | "private">("direct");
  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createTarget, onSuccess: async () => { onClose(); await queryClient.invalidateQueries(); } });
  const enroll = useMutation({ mutationFn: api.enrollPrivateNode, onSuccess: async (out) => { await queryClient.invalidateQueries(); onEnrollment(out); } });

  function next(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const values = { ...draft, ...formValues(event.currentTarget) };
    setDraft(values);
    if (mode === "private") {
      enroll.mutate({ owner_type: "organization", owner_id: data.activeOrg.id, label: values.alias || values.name || "private-node", default_host: "127.0.0.1", default_port: 22 });
      return;
    }
    if (step < 2) {
      setStep(step + 1);
      return;
    }
    create.mutate({
      owner_type: "organization",
      owner_id: data.activeOrg.id,
      target_type: "direct",
      name: values.name,
      alias: values.alias,
      host: values.host,
      port: Number(values.port || 22),
      remote_username: values.remote_username,
      auth_type: values.auth_type || "password",
      secret: values.secret || "",
      tags: splitTags(values.tags || ""),
      proxy_target_id: values.proxy_target_id || "",
    });
  }

  return <Modal title="添加 SSH 服务" onClose={onClose}>
    <div className="tabs" role="tablist">
      <button type="button" role="tab" aria-selected={mode === "direct"} className={clsx(mode === "direct" && "active")} onClick={() => { setMode("direct"); setStep(0); }}>SSH 服务器</button>
      <button type="button" role="tab" aria-selected={mode === "private"} className={clsx(mode === "private" && "active")} onClick={() => { setMode("private"); setStep(0); }}>私有节点</button>
    </div>
    <form className="grid two" onSubmit={next}>
      {mode === "private" ? <>
        <Field label="服务别名" name="alias" defaultValue={draft.alias} required />
        <p className="span-two muted">只需要填写别名。安装完成后，私有节点会作为普通 SSH 服务出现在列表中，可以再重命名和打标签。</p>
      </> : <>
        {step === 0 && <>
          <Field label="服务名称" name="name" defaultValue={draft.name} required />
          <Field label="目标别名" name="alias" defaultValue={draft.alias} required />
          <Field label="标签" name="tags" defaultValue={draft.tags} placeholder="测试环境, common" />
        </>}
        {step === 1 && <>
          <Field label="目标主机" name="host" defaultValue={draft.host} required />
          <Field label="目标端口" name="port" defaultValue={draft.port || "22"} required />
          <Field label="远程用户名" name="remote_username" defaultValue={draft.remote_username} required />
        </>}
        {step === 2 && <>
          <Select label="认证方式" name="auth_type" defaultValue={draft.auth_type || "password"} options={[["password", "账号密码"], ["private_key", "私钥"]]} />
          <label className="field"><span>密钥或密码</span><textarea name="secret" defaultValue={draft.secret} /></label>
          <Select label="高级：使用已有跳板服务" name="proxy_target_id" defaultValue={draft.proxy_target_id || ""} options={[["", "不使用"], ...data.targets.map((target): [string, string] => [target.id, `${target.name} (${target.alias})`])]} />
        </>}
      </>}
      <ModalActions onCancel={onClose} submit={mode === "private" ? "创建安装令牌" : step < 2 ? "下一步" : "添加服务"} />
    </form>
  </Modal>;
}

function TargetDrawer({ data, target, onClose }: { data: ConsoleData; target: Target; onClose: () => void }) {
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updateTarget(target.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const color = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updateTargetTagColor(body), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={target.name} subtitle="编辑连接信息、标签和标签颜色。" onClose={onClose}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => update.mutate({
      name: body.name,
      alias: body.alias,
      host: body.host,
      port: Number(body.port || target.port || 22),
      remote_username: body.remote_username,
      auth_type: body.auth_type,
      secret: body.secret,
      tags: splitTags(body.tags || ""),
      proxy_target_id: body.proxy_target_id || "",
    }))}>
      <Field label="服务名称" name="name" defaultValue={target.name} required />
      <Field label="目标别名" name="alias" defaultValue={target.alias} required />
      <Field label="目标主机" name="host" defaultValue={target.host} disabled={target.target_type === "agent"} />
      <Field label="目标端口" name="port" defaultValue={String(target.port || 22)} disabled={target.target_type === "agent"} />
      <Field label="远程用户名" name="remote_username" defaultValue={target.remote_username} disabled={target.target_type === "agent"} />
      <Select label="认证方式" name="auth_type" defaultValue={target.auth_type} options={[["password", "账号密码"], ["private_key", "私钥"]]} />
      <label className="field"><span>密钥或密码</span><textarea name="secret" /></label>
      <Field label="标签" name="tags" defaultValue={(target.tags || []).join(", ")} />
      <Select label="跳板服务" name="proxy_target_id" defaultValue={target.proxy_target_id || ""} options={[["", "不使用"], ...data.targets.filter((item) => item.id !== target.id).map((item): [string, string] => [item.id, `${item.name} (${item.alias})`])]} />
      <ModalActions onCancel={onClose} submit="保存" />
    </form>
    <section className="section-block embedded">
      <h3>标签颜色</h3>
      {(target.tags || []).map((tag) => <div className="tag-color-row" key={tag}>
        <Tag tag={tag} color={tagColor(tag, target.tag_colors)} />
        <div className="tag-color-swatches">
          {["gray", "red", "orange", "yellow", "green", "blue", "purple"].map((item) => <button key={item} type="button" className={`tag-color-${item}`} onClick={() => color.mutate({ owner_type: "organization", owner_id: data.activeOrg.id, name: tag, color: item })}>{item}</button>)}
        </div>
      </div>)}
    </section>
  </Drawer>;
}

function InstallDrawer({ enrollment, onClose }: { enrollment: Enrollment; onClose: () => void }) {
  return <Drawer title="安装引导" subtitle="选择平台并复制包含专属 token 的命令。" onClose={onClose}>
    <div className="grid two">
      <section className="section-block embedded">
        <h3>Linux / macOS</h3>
        <CommandBox label="运行一次" value={enrollment.install_sh || ""} />
        <CommandBox label="systemctl 开机启动" value={enrollment.service_sh || ""} />
      </section>
      <section className="section-block embedded">
        <h3>Windows</h3>
        <CommandBox label="PowerShell 运行一次" value={enrollment.install_ps1 || ""} />
        <CommandBox label="sc.exe 开机启动" value={enrollment.service_ps1 || ""} />
      </section>
    </div>
  </Drawer>;
}

function PoliciesPage({ data }: { data: ConsoleData }) {
  const [modal, setModal] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [drawer, setDrawer] = useState<Policy | null>(null);
  const queryClient = useQueryClient();
  const create = useMutation({ mutationFn: api.createPolicy, onSuccess: async () => { setModal(false); await queryClient.invalidateQueries(); } });
  const remove = useMutation({ mutationFn: api.deletePolicy, onSuccess: async () => { setSelected([]); await queryClient.invalidateQueries(); } });
  const copy = useMutation({ mutationFn: api.copyPolicy, onSuccess: async () => queryClient.invalidateQueries() });
  return (
    <>
      <section className="resource-head">
        <div><small>命令安全</small><h2>命令安全组</h2><p>点击某个安全组进入编辑，也支持多选批量删除和复制。</p></div>
        <button type="button" className="primary" onClick={() => setModal(true)}><Plus />创建安全组</button>
      </section>
      {selected.length > 0 && <div className="batch-bar"><select onChange={(event) => { if (event.target.value === "delete") selected.forEach((id) => remove.mutate(id)); }}><option>批量操作</option><option value="delete">删除</option></select></div>}
      <Panel title="安全组列表" subtitle="">
        <SimpleTable headers={["", "名称", "默认", "能力", "操作"]} rows={data.policies.map((policy) => [
          <input type="checkbox" checked={selected.includes(policy.id)} onChange={(event) => setSelected(event.target.checked ? [...selected, policy.id] : selected.filter((id) => id !== policy.id))} />,
          <button type="button" className="row-link" onClick={() => setDrawer(policy)}><strong>{policy.name}</strong><small>{policy.llm_config_id ? "LLM" : "无 LLM"}</small></button>,
          policy.default_action,
          <span className="capability-row">{policy.allow_interactive && "终端 "}{policy.allow_port_forward && "转发 "}{policy.allow_upload && "上传 "}{policy.allow_download && "下载"}</span>,
          <span className="inline-actions"><button type="button" onClick={() => copy.mutate(policy.id)}>复制</button><button type="button" onClick={() => remove.mutate(policy.id)}>删除</button></span>,
        ])} />
      </Panel>
      {modal && <PolicyFormModal data={data} onClose={() => setModal(false)} onSubmit={(body) => create.mutate({ ...body, owner_type: "organization", owner_id: data.activeOrg.id })} />}
      {drawer && <PolicyDrawer data={data} policy={drawer} onClose={() => setDrawer(null)} />}
    </>
  );
}

function PolicyFormModal({ data, onClose, onSubmit, policy }: { data: ConsoleData; onClose: () => void; onSubmit: (body: Record<string, unknown>) => void; policy?: Policy }) {
  return <Modal title={policy ? "编辑安全组" : "创建安全组"} onClose={onClose}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => onSubmit(policyPayload(body)))}>
      <Field label="名称" name="name" defaultValue={policy?.name || ""} required />
      <Select label="默认动作" name="default_action" defaultValue={policy?.default_action || "deny"} options={[["deny", "拒绝"], ["allow", "允许"]]} />
      <Select label="LLM" name="llm_config_id" defaultValue={policy?.llm_config_id || ""} options={[["", "不使用"], ...data.llms.map((item): [string, string] => [item.id, item.name])]} />
      <Select label="提示词" name="llm_prompt_id" defaultValue={policy?.llm_prompt_id || ""} options={[["", "默认"], ...data.prompts.map((item): [string, string] => [item.id, item.title])]} />
      <label className="field span-two"><span>IP 白名单或范围</span><textarea name="ip_allowlist" defaultValue={policy?.ip_allowlist || ""} placeholder="private, 10.0.0.0/8, 192.168.1.1-192.168.1.20" /></label>
      <Toggle name="allow_interactive" label="允许交互式终端" defaultChecked={policy?.allow_interactive} />
      <Toggle name="allow_port_forward" label="允许端口转发" defaultChecked={policy?.allow_port_forward} />
      <Toggle name="allow_upload" label="允许上传" defaultChecked={policy?.allow_upload} />
      <Toggle name="allow_download" label="允许下载" defaultChecked={policy?.allow_download} />
      <ModalActions onCancel={onClose} submit={policy ? "保存" : "创建"} />
    </form>
  </Modal>;
}

function PolicyDrawer({ data, policy, onClose }: { data: ConsoleData; policy: Policy; onClose: () => void }) {
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: (body: Record<string, unknown>) => api.updatePolicy(policy.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const rule = useMutation({ mutationFn: (body: Record<string, string>) => api.addRule(policy.id, body), onSuccess: async () => queryClient.invalidateQueries() });
  const bindTarget = useMutation({ mutationFn: (id: string) => api.bindTarget(policy.id, id), onSuccess: async () => queryClient.invalidateQueries() });
  const bindTag = useMutation({ mutationFn: (tag: string) => api.bindTargetTag(policy.id, { owner_type: "organization", owner_id: data.activeOrg.id, tag }), onSuccess: async () => queryClient.invalidateQueries() });
  const bindGroup = useMutation({ mutationFn: (id: string) => api.bindGroup(policy.id, id), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={policy.name} subtitle="编辑安全组规则、绑定目标、标签和用户组。" onClose={onClose}>
    <PolicyFormInline data={data} policy={policy} onSubmit={(body) => update.mutate(body)} />
    <section className="section-block embedded">
      <h3>规则</h3>
      <form className="grid three" onSubmit={(event) => formSubmit(event, (body) => rule.mutate(body))}>
        <Select label="类型" name="rule_type" options={[["whitelist", "白名单"], ["blacklist", "黑名单"]]} />
        <Select label="匹配" name="pattern_type" options={[["exact", "精确"], ["prefix", "前缀"], ["contains", "包含"]]} />
        <Field label="命令" name="pattern" required />
        <ModalActions submit="添加规则" />
      </form>
    </section>
    <section className="section-block embedded">
      <h3>绑定</h3>
      <div className="grid three">
        <SelectButton label="绑定服务" items={data.targets.map((item): [string, string] => [item.id, item.name])} onSelect={(id) => bindTarget.mutate(id)} />
        <SelectButton label="绑定标签" items={[...new Set(data.targets.flatMap((item) => item.tags || []))].map((tag): [string, string] => [tag, tag])} onSelect={(tag) => bindTag.mutate(tag)} />
        <SelectButton label="绑定用户组" items={data.groups.map((item): [string, string] => [item.id, item.name])} onSelect={(id) => bindGroup.mutate(id)} />
      </div>
    </section>
  </Drawer>;
}

function PolicyFormInline({ data, policy, onSubmit }: { data: ConsoleData; policy: Policy; onSubmit: (body: Record<string, unknown>) => void }) {
  return <section className="section-block embedded">
    <h3>基础配置</h3>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => onSubmit(policyPayload(body)))}>
      <Field label="名称" name="name" defaultValue={policy.name} required />
      <Select label="默认动作" name="default_action" defaultValue={policy.default_action} options={[["deny", "拒绝"], ["allow", "允许"]]} />
      <Select label="LLM" name="llm_config_id" defaultValue={policy.llm_config_id || ""} options={[["", "不使用"], ...data.llms.map((item): [string, string] => [item.id, item.name])]} />
      <Select label="提示词" name="llm_prompt_id" defaultValue={policy.llm_prompt_id || ""} options={[["", "默认"], ...data.prompts.map((item): [string, string] => [item.id, item.title])]} />
      <label className="field span-two"><span>IP 白名单或范围</span><textarea name="ip_allowlist" defaultValue={policy.ip_allowlist || ""} /></label>
      <Toggle name="allow_interactive" label="允许交互式终端" defaultChecked={policy.allow_interactive} />
      <Toggle name="allow_port_forward" label="允许端口转发" defaultChecked={policy.allow_port_forward} />
      <Toggle name="allow_upload" label="允许上传" defaultChecked={policy.allow_upload} />
      <Toggle name="allow_download" label="允许下载" defaultChecked={policy.allow_download} />
      <ModalActions submit="保存" />
    </form>
  </section>;
}

function AuditPage({ data }: { data: ConsoleData }) {
  const [filters, setFilters] = useState({ query: "", started_from: "", started_to: "", page: 1, page_size: 20 });
  const audit = useQuery({ queryKey: ["audit-page", filters], queryFn: () => api.audit(filters) });
  const logs = audit.data?.logs || data.auditPage.logs;
  return (
    <>
      <section className="resource-head">
        <div><small>审计</small><h2>命令审计</h2><p>支持分页、搜索和时间范围筛选。</p></div>
      </section>
      <form className="toolbar" onSubmit={(event) => formSubmit(event, (body) => setFilters({ query: body.query || "", started_from: body.started_from || "", started_to: body.started_to || "", page: 1, page_size: 20 }))}>
        <Search />
        <input name="query" placeholder="搜索命令、用户、公钥、目标..." />
        <input name="started_from" type="datetime-local" />
        <input name="started_to" type="datetime-local" />
        <button type="submit">搜索</button>
      </form>
      <Panel title="审计列表" subtitle="">
        {logs.length ? <AuditTable logs={logs} /> : <Empty title="暂无审计记录" body="暂无可展示的命令决策。" />}
      </Panel>
      <div className="pager">
        <button type="button" disabled={filters.page <= 1} onClick={() => setFilters({ ...filters, page: filters.page - 1 })}>上一页</button>
        <span>Page {audit.data?.page || 1}</span>
        <button type="button" disabled={(audit.data?.total || 0) <= filters.page * filters.page_size} onClick={() => setFilters({ ...filters, page: filters.page + 1 })}>下一页</button>
      </div>
    </>
  );
}

function SystemAdminPage({ data }: { data: ConsoleData }) {
  const [modal, setModal] = useState<"" | "users" | "orgs" | "dingtalk" | "ldap">("");
  const adminUsers = useQuery({ queryKey: ["admin-users"], queryFn: api.adminUsers, enabled: data.user.is_system_admin && modal === "users" });
  const adminOrgs = useQuery({ queryKey: ["admin-orgs"], queryFn: api.adminOrgs, enabled: data.user.is_system_admin && modal === "orgs" });
  return (
    <>
      <section className="resource-head">
        <div><small>资源控制台</small><h2>系统管理</h2><p>配置登录源、账号和组织管理。</p></div>
        <div className="resource-actions">
          <button type="button" onClick={() => setModal("dingtalk")}>配置钉钉</button>
          <button type="button" className="primary" onClick={() => setModal("ldap")}>配置 LDAP</button>
        </div>
      </section>
      <div className="identity-grid">
        <button type="button" className="admin-card" onClick={() => setModal("users")}><strong>账号管理</strong><span>搜索用户、调整系统管理员权限、重置本地账号密码。</span></button>
        <button type="button" className="admin-card" onClick={() => setModal("orgs")}><strong>组织管理</strong><span>共享组织列表和成员修复，不展示个人组织。</span></button>
      </div>
      {modal === "users" && <AdminUsersModal users={adminUsers.data?.users || []} onClose={() => setModal("")} />}
      {modal === "orgs" && <AdminOrgsModal orgs={(adminOrgs.data?.organizations || []).filter((org) => !org.is_personal)} onClose={() => setModal("")} />}
      {modal === "dingtalk" && <ProviderModal title="配置钉钉" action={api.updateDingTalkSettings} onClose={() => setModal("")} />}
      {modal === "ldap" && <ProviderModal title="配置 LDAP" action={api.updateLDAPSettings} onClose={() => setModal("")} />}
    </>
  );
}

function AdminUsersModal({ users, onClose }: { users: AdminUser[]; onClose: () => void }) {
  const [resetUser, setResetUser] = useState<AdminUser | null>(null);
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ id, is_system_admin }: { id: string; is_system_admin: boolean }) => api.updateAdminUser(id, { is_system_admin }), onSuccess: async () => queryClient.invalidateQueries() });
  return <Modal title="账号管理" onClose={onClose} wide>
    <SimpleTable headers={["邮箱", "登录源", "系统管理员", "操作"]} rows={users.map((user) => [
      <UserCell member={{ user_id: user.id, email: user.email, display_name: user.display_name, role: "member" }} />,
      user.auth_provider === "local" ? "本地" : user.auth_provider,
      <select defaultValue={user.is_system_admin ? "admin" : "user"} onChange={(event) => update.mutate({ id: user.id, is_system_admin: event.target.value === "admin" })}><option value="user">用户</option><option value="admin">管理员</option></select>,
      <button type="button" disabled={user.auth_provider !== "local"} onClick={() => setResetUser(user)}>重置密码</button>,
    ])} />
    {resetUser && <ResetPasswordModal user={resetUser} onClose={() => setResetUser(null)} />}
  </Modal>;
}

function ResetPasswordModal({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const reset = useMutation({ mutationFn: (body: Record<string, string>) => api.resetAdminUserPassword(user.id, body), onSuccess: onClose });
  return <Modal title="重置用户密码" onClose={onClose}>
    <form className="stack" onSubmit={(event) => formSubmit(event, (body) => reset.mutate(body))}>
      <p>{user.display_name || user.email}</p>
      <Field label="新密码" name="password" type="password" required />
      <ModalActions onCancel={onClose} submit="保存新密码" />
    </form>
  </Modal>;
}

function AdminOrgsModal({ orgs, onClose }: { orgs: AdminOrg[]; onClose: () => void }) {
  const [selected, setSelected] = useState<AdminOrg | null>(null);
  return <Modal title="组织管理" onClose={onClose} wide>
    <SimpleTable headers={["组织", "角色", "操作"]} rows={orgs.map((org) => [
      <strong>{org.name}</strong>,
      roleText(org.role),
      <button type="button" onClick={() => setSelected(org)}>管理成员</button>,
    ])} />
    {selected && <AdminOrgDrawer org={selected} onClose={() => setSelected(null)} />}
  </Modal>;
}

function AdminOrgDrawer({ org, onClose }: { org: AdminOrg; onClose: () => void }) {
  const members = useQuery({ queryKey: ["admin-org-members", org.id], queryFn: () => api.adminOrgMembers(org.id) });
  const queryClient = useQueryClient();
  const update = useMutation({ mutationFn: ({ userID, role }: { userID: string; role: string }) => api.adminUpdateOrgMember(org.id, userID, { role }), onSuccess: async () => queryClient.invalidateQueries() });
  return <Drawer title={org.name} subtitle="组织成员管理" onClose={onClose}>
    <div className="member-card-list">
      {(members.data?.members || []).map((member) => <article className="member-card" key={member.user_id}>
        <UserCell member={member} />
        <span>{roleText(member.role)}</span>
        {member.role === "owner" ? <span className="badge info">当前所有者</span> : <span className="inline-actions">
          <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "admin" })}>设管理员</button>
          <button type="button" onClick={() => update.mutate({ userID: member.user_id, role: "member" })}>设成员</button>
        </span>}
      </article>)}
    </div>
  </Drawer>;
}

function ProviderModal({ title, action, onClose }: { title: string; action: (body: Record<string, unknown>) => Promise<void>; onClose: () => void }) {
  const mutation = useMutation({ mutationFn: action, onSuccess: onClose });
  return <Modal title={title} onClose={onClose}>
    <form className="grid two" onSubmit={(event) => formSubmit(event, (body) => mutation.mutate(body))}>
      <Field label="启用" name="enabled" />
      <Field label="Client ID / Server URL" name="client_id" />
      <Field label="Client Secret / Bind DN" name="client_secret" />
      <Field label="Redirect URL / Base DN" name="redirect_url" />
      <ModalActions onCancel={onClose} submit="保存" />
    </form>
  </Modal>;
}

function OrgSwitcher({ data }: { data: ConsoleData }) {
  return <select className="org-switcher" value={data.activeOrg.id} onChange={(event) => data.setActiveOrgID(event.target.value)}>
    {data.orgs.map((org) => <option key={org.id} value={org.id}>{org.name} {org.is_personal ? "个人" : ""}</option>)}
  </select>;
}

function AuditTable({ logs }: { logs: AuditLog[] }) {
  return <SimpleTable headers={["用户", "公钥", "目标服务器", "命令", "类型", "决策", "原因", "退出", "开始时间"]} rows={logs.map((log) => [
    <span><strong>{log.user_display_name || log.user_email || "-"}</strong><small>{log.user_email || ""}</small></span>,
    <span><strong>{log.public_key_name || "-"}</strong><small>{log.public_key_fingerprint || ""}</small></span>,
    <span><strong>{log.target_name || log.target_alias || "-"}</strong><small>{log.target_endpoint || ""}</small></span>,
    <code>{log.command || "-"}</code>,
    log.request_type,
    <span className={clsx("badge", log.policy_decision === "allow" ? "success" : "danger")}>{log.policy_decision === "allow" ? "允许" : "拒绝"}</span>,
    log.policy_reason || "-",
    String(log.exit_code ?? ""),
    formatDate(log.started_at),
  ])} />;
}

function NavButton({ to, label, icon, onClick }: { to: string; label: string; icon: ReactNode; onClick: () => void }) {
  const location = useLocation();
  const active = to === "/" ? location.pathname === "/" : location.pathname.startsWith(to);
  return <Link className={clsx(active && "active")} to={to} onClick={onClick}>{icon}{label}</Link>;
}

function Panel({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return <section className="panel"><div className="panel-head"><div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div></div>{children}</section>;
}

function SummaryCard({ index, title, body }: { index: string; title: string; body: string }) {
  return <section className="access-summary-card"><span>{index}</span><strong>{title}</strong><small>{body}</small></section>;
}

function Metric({ label, value, icon }: { label: string; value: number; icon?: ReactNode }) {
  return <div className="metric">{icon || <Activity />}<span>{label}</span><strong>{value}</strong></div>;
}

function Modal({ title, children, onClose, wide = false }: { title: string; children: ReactNode; onClose: () => void; wide?: boolean }) {
  return <div className="overlay"><section className={clsx("modal", wide && "wide")} role="dialog" aria-label={title}>
    <header><div><h2>{title}</h2></div><button className="icon-button" type="button" aria-label="Close" onClick={onClose}><X /></button></header>
    <div className="surface-body modal-body-list">{children}</div>
  </section></div>;
}

function Drawer({ title, subtitle, children, onClose }: { title: string; subtitle?: string; children: ReactNode; onClose: () => void }) {
  return <div className="drawer-scrim"><aside className="drawer">
    <header><div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div><button className="icon-button" type="button" aria-label="Close" onClick={onClose}><X /></button></header>
    <div className="surface-body">{children}</div>
  </aside></div>;
}

function Field({ label, name, type = "text", defaultValue = "", required = false, placeholder = "", disabled = false }: { label: string; name: string; type?: string; defaultValue?: string; required?: boolean; placeholder?: string; disabled?: boolean }) {
  return <label className="field"><span>{label}</span><input name={name} type={type} defaultValue={defaultValue} required={required} placeholder={placeholder} disabled={disabled} /></label>;
}

function Select({ label, name, options, defaultValue = "" }: { label: string; name: string; options: (readonly [string, string])[]; defaultValue?: string }) {
  return <label className="field"><span>{label}</span><select name={name} defaultValue={defaultValue}>{options.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select></label>;
}

function Toggle({ name, label, defaultChecked }: { name: string; label: string; defaultChecked?: boolean }) {
  return <label className="toggle-row"><input type="checkbox" name={name} defaultChecked={defaultChecked} /><span>{label}</span></label>;
}

function ModalActions({ onCancel, submit }: { onCancel?: () => void; submit: string }) {
  return <div className="form-actions span-two">{onCancel && <button type="button" onClick={onCancel}>取消</button>}<button type="submit" className="primary">{submit}</button></div>;
}

function Segmented({ value, items, onChange }: { value: string; items: (readonly [string, string, string])[]; onChange: (value: string) => void }) {
  const { locale } = useI18n();
  return <div className="theme-switch">{items.map(([id, zh, en]) => <button key={id} type="button" className={clsx(value === id && "active")} onClick={() => onChange(id)}>{locale === "en" ? en : zh}</button>)}</div>;
}

function Toolbar({ query, setQuery, children }: { query: string; setQuery: (value: string) => void; children?: ReactNode }) {
  return <div className="toolbar"><Search /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索..." />{children}<button type="button" onClick={() => setQuery("")}>清空筛选</button></div>;
}

function SimpleTable({ headers, rows }: { headers: string[]; rows: ReactNode[][] }) {
  return <div className="table-wrap"><table><thead><tr>{headers.map((item) => <th key={item}>{item}</th>)}</tr></thead><tbody>{rows.map((row, index) => <tr key={index}>{row.map((cell, cellIndex) => <td key={cellIndex}>{cell}</td>)}</tr>)}</tbody></table></div>;
}

function Empty({ title, body }: { title: string; body: string }) {
  return <div className="empty-state"><div className="empty-orbit" /><strong>{title}</strong><span>{body}</span></div>;
}

function UserCell({ member }: { member: Pick<Member, "display_name" | "email" | "user_id" | "role"> }) {
  return <span><strong>{member.display_name || member.email}</strong><small>{member.email}</small></span>;
}

function TagList({ target }: { target: Target }) {
  return <span className="tag-row">{(target.tags || []).map((tag) => <Tag key={tag} tag={tag} color={tagColor(tag, target.tag_colors)} />)}</span>;
}

function Tag({ tag, color }: { tag: string; color: string }) {
  return <span className={`tag-chip tag-color-${color}`} data-tag={tag}>{tag}</span>;
}

function CopyButton({ value }: { value: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  return <button type="button" className="copy-anchor" data-value={value} onClick={async () => { await copyText(value); setCopied(true); window.setTimeout(() => setCopied(false), 1300); }}>
    <Copy />复制连接命令{copied && <span className="copy-tip">{t("copied")}</span>}
  </button>;
}

function CommandBox({ label, value }: { label: string; value: string }) {
  return <div className="command-box"><span>{label}</span><code>{value}</code><CopyButton value={value} /></div>;
}

function SelectButton({ label, items, onSelect }: { label: string; items: (readonly [string, string])[]; onSelect: (value: string) => void }) {
  return <label className="field"><span>{label}</span><select defaultValue="" onChange={(event) => { if (event.target.value) onSelect(event.target.value); event.target.value = ""; }}><option value="">选择...</option>{items.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select></label>;
}

function Loading() {
  return <section className="loading-view"><div className="mark">g</div><p>Loading bastion console...</p></section>;
}

function Fatal({ error }: { error: unknown }) {
  return <section className="auth-screen"><div className="auth-card"><div className="status error">{error instanceof Error ? error.message : String(error)}</div></div></section>;
}

function formSubmit(event: React.FormEvent<HTMLFormElement>, next: (data: Record<string, string>) => void) {
  event.preventDefault();
  next(formValues(event.currentTarget));
}

function formValues(form: HTMLFormElement) {
  const data: Record<string, string> = {};
  for (const [key, value] of new FormData(form).entries()) data[key] = String(value);
  for (const element of Array.from(form.elements)) {
    if (element instanceof HTMLInputElement && element.type === "checkbox") data[element.name] = element.checked ? "on" : "";
  }
  return data;
}

function policyPayload(body: Record<string, string>): Record<string, unknown> {
  return {
    name: body.name,
    default_action: body.default_action || "deny",
    llm_config_id: body.llm_config_id || "",
    llm_prompt_id: body.llm_prompt_id || "",
    ip_allowlist: body.ip_allowlist || "",
    allow_interactive: body.allow_interactive === "on",
    allow_port_forward: body.allow_port_forward === "on",
    allow_upload: body.allow_upload === "on",
    allow_download: body.allow_download === "on",
  };
}

function sortMembers(members: Member[], query: string, sort: "role" | "name" | "newest") {
  const filtered = members.filter((item) => [item.display_name, item.email, item.role].join(" ").toLowerCase().includes(query.toLowerCase()));
  return [...filtered].sort((a, b) => {
    if (sort === "newest") return String(b.created_at || "").localeCompare(String(a.created_at || ""));
    if (sort === "name") return (a.display_name || a.email).localeCompare(b.display_name || b.email);
    const weight = { owner: 0, admin: 1, member: 2 };
    return weight[a.role] - weight[b.role];
  });
}

function roleText(role?: string) {
  if (role === "owner") return "所有者";
  if (role === "admin") return "管理员";
  return "成员";
}

function formatDate(value?: string) {
  if (!value) return "";
  try {
    return new Intl.DateTimeFormat(dateLocale(document.documentElement.lang === "en" ? "en" : "zh-CN"), { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
  } catch {
    return value;
  }
}

function pageTitle() {
  const path = location.pathname.replace(/^\/+/, "") || "dashboard";
  const titles: Record<string, string> = {
    dashboard: "控制台",
    orgs: "组织",
    "org-admin": "组织成员",
    keys: "公钥",
    targets: "SSH 服务",
    policies: "命令安全组",
    audit: "命令审计",
    "system-admin": "系统管理",
  };
  return titles[path] || "控制台";
}

function localizeError(error: unknown, t: (key: string) => string) {
  const message = error instanceof Error ? error.message : String(error);
  if (message === "invalid credentials") return t("invalidCredentials");
  return message;
}
