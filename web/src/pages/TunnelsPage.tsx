import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input, InputNumber, Select, Switch, Tag, Tooltip } from "antd";
import {
  ArrowRight,
  ChartNoAxesCombined,
  Clock3,
  GitBranch,
  Globe,
  Network,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  Server,
  Square,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import { ConfirmDialog, ErrorMessage, Modal } from "../components/ui";
import { useI18n } from "../i18n";
import { TunnelPathStatus } from "../components/TunnelPathStatus";
import { TunnelTrafficDialog, formatBytes } from "../components/TunnelTraffic";
import type { ConsoleData, Target, Tunnel, TunnelConfig } from "../types";

const copy = {
  "zh-CN": {
    title: "隧道管理",
    creator: "创建者",
    temporary: "SSH 临时转发",
    managed: "持久配置",
    source: "来源地址",
    localUnknown: "SSH 客户端 · 监听地址未知",
    remoteUnknown: "目标地址未知",
    sshClient: "SSH 客户端",
    temporaryHelp:
      "由 SSH 客户端维持，断开后结束。可停止，需从客户端重新创建；本地监听或远端目标未由 SSH 协议上报。",
    subtitle: "用网络流向理解每条 TCP 隧道，统一配置入口、出口和运行时长。",
    create: "新建隧道",
    edit: "编辑隧道",
    search: "搜索隧道或目标地址",
    all: "全部状态",
    running: "运行中",
    starting: "连接中",
    error: "连接失败 · 自动重试",
    stopped: "已停止",
    expired: "已到期",
    entry: "入口 · 接收连接",
    exit: "出口 · 发起访问",
    destination: "目标服务",
    bastion: "堡垒机",
    relay: "经堡垒机中转",
    access: "访问者",
    returns: "箭头表示请求方向，响应沿原路返回。",
    jump: "SSH 连接链路",
    direct: "直接连接",
    listen: "监听地址",
    port: "监听端口",
    targetHost: "目标地址",
    targetPort: "目标端口",
    entryMachine: "入口机器",
    exitMachine: "出口机器",
    name: "隧道名称",
    duration: "每次启用时长",
    permanent: "永久启用",
    timed: "定时停止",
    hours: "小时",
    durationHelp: "到期自动停止。重连或服务器重启不会重置到期时间。",
    saved: "配置持久保存，启用后自动恢复断开的连接。",
    loopback: "仅本机访问",
    public: "允许其他机器访问",
    listenHelp:
      "此地址在入口机器上监听。127.0.0.1 仅本机可访问；0.0.0.0 监听所有 IPv4 网卡。",
    destHelp: "目标地址从出口机器访问。127.0.0.1 表示出口机器自身。",
    sshHelp:
      "SSH 入口依赖远程端口转发权限。对外监听还需要 SSH 服务允许 GatewayPorts，防火墙需放行监听端口。",
    save: "保存配置",
    cancel: "取消",
    start: "启用",
    stop: "停止",
    renew: "重新计时",
    renewHelp: "重新启用，按配置时长重新计算到期时间。现有连接会关闭。",
    remove: "删除",
    removeConfirm: "删除这条隧道？运行中的连接会立即关闭。",
    stopConfirm: "停止这条隧道？现有连接会立即关闭，配置仍会保留。",
    empty: "还没有隧道配置",
    emptyHelp: "先选择接收连接的入口机器，再选择访问目标服务的出口机器。",
    expires: "停止时间",
    connections: "当前连接",
    enableOnSave: "保存后立即启用",
    restricted: "组织管理员可以配置和维护隧道。",
    missing: "机器已删除",
    editHelp:
      "修改运行中的配置会重新建立隧道，现有连接会关闭；新时长在下次启用时生效。",
    flow: "网络流向预览",
    client: "客户端连接入口端口",
    outbound: "由出口机器连接目标",
    refresh: "刷新",
    total: "条配置",
    status: "状态",
    ssh: "SSH",
    agent: "Agent",
    machineRequired: "请选择机器",
    jumpHelp: "虚线显示建立 SSH 连接时使用的跳板链路。",
    enabled: "已启用",
    unavailable: "未建立监听",
    traffic: "流量与连接",
    totalTraffic: "累计流量",
    upload: "上传",
    download: "下载",
    opened: "累计连接",
    peak: "峰值并发",
    p2p: "P2P 直连",
    agentSegment: "可切换的 Agent 传输段",
    agentSegmentHelp: "仅此段在中转与直连间切换，SSH 段保持原来的连接。",
    mixed: "部分直连",
    negotiating: "中转 · 尝试直连",
    relayPath: "中转",
    autoP2P:
      "兼容的 Agent 会先中转，再自动尝试 P2P；切换路径保持已有 TCP 连接。",
  },
  en: {
    creator: "Created by",
    temporary: "Temporary SSH forward",
    managed: "Persistent configuration",
    source: "Source address",
    localUnknown: "SSH client · listener unknown",
    remoteUnknown: "Destination unknown",
    sshClient: "SSH client",
    temporaryHelp:
      "Owned by the SSH client and ends on disconnect. Stop here; recreate from the client. SSH does not report the local listener or remote destination.",
    title: "Tunnels",
    subtitle:
      "See how traffic flows through each TCP tunnel. Manage endpoints and runtime in one place.",
    create: "New tunnel",
    edit: "Edit tunnel",
    search: "Search tunnels or destinations",
    all: "All statuses",
    running: "Running",
    starting: "Connecting",
    error: "Connection failed · retrying",
    stopped: "Stopped",
    expired: "Expired",
    entry: "Entry · accepts connections",
    exit: "Exit · connects onward",
    destination: "Destination service",
    bastion: "Bastion",
    relay: "Via bastion relay",
    access: "Client",
    returns: "Arrows show requests. Responses return along the same path.",
    jump: "SSH connection path",
    direct: "Direct connection",
    listen: "Listen address",
    port: "Listen port",
    targetHost: "Destination address",
    targetPort: "Destination port",
    entryMachine: "Entry machine",
    exitMachine: "Exit machine",
    name: "Tunnel name",
    duration: "Runtime per activation",
    permanent: "Always enabled",
    timed: "Stop after duration",
    hours: "hours",
    durationHelp:
      "Stops on expiry. Reconnects and server restarts keep the original deadline.",
    saved: "Configurations persist. Enabled tunnels reconnect automatically.",
    loopback: "Local access only",
    public: "Allow remote access",
    listenHelp:
      "Listens on the entry machine. 127.0.0.1 is local only; 0.0.0.0 binds all IPv4 interfaces.",
    destHelp:
      "Resolved and reached from the exit machine. 127.0.0.1 means the exit machine itself.",
    sshHelp:
      "SSH entries require remote forwarding permission. Public listeners also need GatewayPorts and an open firewall port.",
    save: "Save configuration",
    cancel: "Cancel",
    start: "Enable",
    stop: "Stop",
    renew: "Reset timer",
    renewHelp:
      "Re-enable for the configured duration. Existing connections will close.",
    remove: "Delete",
    removeConfirm:
      "Delete this tunnel? Active connections will close immediately.",
    stopConfirm:
      "Stop this tunnel? Existing connections will close; the configuration is kept.",
    empty: "No tunnels yet",
    emptyHelp:
      "Choose the machine accepting connections, then the machine reaching the destination service.",
    expires: "Stops at",
    connections: "Active connections",
    enableOnSave: "Enable after saving",
    restricted: "Organization admins can configure and maintain tunnels.",
    missing: "Deleted machine",
    editHelp:
      "Editing a running tunnel reconnects it and closes existing connections. A new duration applies at the next activation.",
    flow: "Network flow preview",
    client: "Client connects to the entry port",
    outbound: "Exit connects to the destination",
    refresh: "Refresh",
    total: "configurations",
    status: "Status",
    ssh: "SSH",
    agent: "Agent",
    machineRequired: "Select a machine",
    jumpHelp:
      "Dashed lines show the jump hosts used to establish SSH connections.",
    enabled: "Enabled",
    unavailable: "Listener not established",
    traffic: "Traffic & connections",
    totalTraffic: "Total traffic",
    upload: "Upload",
    download: "Download",
    opened: "Total connections",
    peak: "Peak concurrency",
    p2p: "P2P direct",
    agentSegment: "Agent transport segment",
    agentSegmentHelp:
      "Only this segment changes paths; SSH connections stay established.",
    mixed: "Partially direct",
    negotiating: "Relay · trying direct",
    relayPath: "Relay",
    autoP2P:
      "Compatible Agents start over relay, then try P2P. Path changes preserve established TCP connections.",
  },
};
type Words = (typeof copy)["en"];
const blank: TunnelConfig = {
  name: "",
  entry_target_id: "",
  listen_host: "127.0.0.1",
  listen_port: 8080,
  exit_target_id: "",
  destination_host: "127.0.0.1",
  destination_port: 80,
  duration_seconds: 0,
};
const endpoint = (host: string, port: number) =>
  `${host.includes(":") ? `[${host}]` : host}:${port}`;

function Flow({
  config,
  targets,
  words,
  preview = false,
  transport,
}: {
  config: TunnelConfig;
  targets: Target[];
  words: Words;
  preview?: boolean;
  transport?: Tunnel["transport"];
}) {
  const machine = (id: string) =>
    id
      ? targets.find((t) => t.id === id)?.name || words.missing
      : words.bastion;
  const kind = (id: string) =>
    !id
      ? words.bastion
      : targets.find((t) => t.id === id)?.target_type === "agent"
        ? words.agent
        : words.ssh;
  const chain = (id: string) => {
    const names: string[] = [];
    const seen = new Set<string>();
    let target = targets.find((t) => t.id === id);
    while (
      target?.proxy_target_id &&
      !seen.has(target.proxy_target_id) &&
      names.length < 4
    ) {
      seen.add(target.proxy_target_id);
      target = targets.find((t) => t.id === target?.proxy_target_id);
      names.unshift(target?.name || words.missing);
    }
    return names;
  };
  const anchor = (id: string): Target | undefined => {
    let target = targets.find((t) => t.id === id);
    const seen = new Set<string>();
    while (
      target &&
      target.target_type !== "agent" &&
      target.proxy_target_id &&
      !seen.has(target.id)
    ) {
      seen.add(target.id);
      target = targets.find((t) => t.id === target?.proxy_target_id);
    }
    return target?.target_type === "agent" ? target : undefined;
  };
  const entryAnchor = anchor(config.entry_target_id),
    exitAnchor = anchor(config.exit_target_id);
  return (
    <div
      className={`tunnel-flow ${preview ? "is-preview" : ""}`}
      aria-label={words.flow}
    >
      <div className="tunnel-flow-nodes">
        <div className="tunnel-node entry-node">
          <div className="tunnel-node-label">
            <Server size={16} />
            {words.entry}
          </div>
          <strong>{machine(config.entry_target_id)}</strong>
          <code>{endpoint(config.listen_host, config.listen_port)}</code>
          <span className="tunnel-node-kind">
            {kind(config.entry_target_id)}
          </span>
          {chain(config.entry_target_id).length > 0 && (
            <div className="tunnel-jump">
              <GitBranch size={13} />
              <span>
                {words.jump}: {words.bastion} ⇢{" "}
                {chain(config.entry_target_id).join(" ⇢ ")} ⇢{" "}
                {machine(config.entry_target_id)}
              </span>
            </div>
          )}
        </div>
        <div className="tunnel-flow-arrow">
          <span>
            {transport === "direct"
              ? words.p2p
              : transport === "mixed"
                ? words.mixed
                : words.relay}
          </span>
          <ArrowRight aria-label="→" />
        </div>
        <div className="tunnel-node exit-node">
          <div className="tunnel-node-label">
            <Network size={16} />
            {words.exit}
          </div>
          <strong>{machine(config.exit_target_id)}</strong>
          <span className="tunnel-node-detail">{words.outbound}</span>
          <span className="tunnel-node-kind">
            {kind(config.exit_target_id)}
          </span>
          {chain(config.exit_target_id).length > 0 && (
            <div className="tunnel-jump">
              <GitBranch size={13} />
              <span>
                {words.jump}: {words.bastion} ⇢{" "}
                {chain(config.exit_target_id).join(" ⇢ ")} ⇢{" "}
                {machine(config.exit_target_id)}
              </span>
            </div>
          )}
        </div>
        <div className="tunnel-flow-arrow destination-arrow">
          <ArrowRight aria-label="→" />
        </div>
        <div className="tunnel-node destination-node">
          <div className="tunnel-node-label">
            <Globe size={16} />
            {words.destination}
          </div>
          <strong>{config.destination_host || "…"}</strong>
          <code>TCP / {config.destination_port}</code>
          <span className="tunnel-node-detail">{words.destHelp}</span>
        </div>
      </div>
      {entryAnchor && exitAnchor && (
        <div className="tunnel-agent-path">
          <span>{words.agentSegment}</span>
          <strong>{entryAnchor.name}</strong>
          <ArrowRight />
          <Tag color={transport === "direct" ? "cyan" : undefined}>
            {transport === "direct"
              ? words.p2p
              : transport === "mixed"
                ? words.mixed
                : words.relayPath}
          </Tag>
          <ArrowRight />
          <strong>{exitAnchor.name}</strong>
          {(config.entry_target_id !== entryAnchor.id ||
            config.exit_target_id !== exitAnchor.id) && (
            <small>{words.agentSegmentHelp}</small>
          )}
        </div>
      )}
      {preview && (
        <div className="tunnel-flow-caption">
          <span>{words.client}</span>
          <span>{words.returns}</span>
        </div>
      )}
    </div>
  );
}

export function TunnelsPage({ data }: { data: ConsoleData }) {
  const { locale } = useI18n();
  const w = copy[locale];
  const qc = useQueryClient();
  const allowed =
    data.user.is_system_admin ||
    ["owner", "admin"].includes(data.activeOrg.role || "");
  const [editing, setEditing] = useState<Tunnel | "new" | null>(null);
  const [confirmation, setConfirmation] = useState<{
    tunnel: Tunnel;
    action: "stop" | "delete" | "enable";
  } | null>(null);
  const [traffic, setTraffic] = useState<Tunnel | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const key = ["tunnels", data.activeOrg.id];
  const tunnels = useQuery({
    queryKey: key,
    queryFn: () => api.tunnels(data.activeOrg.id),
    enabled: allowed,
    refetchInterval: 2000,
  });
  const action = useMutation({
    mutationFn: async ({
      tunnel,
      action,
    }: {
      tunnel: Tunnel;
      action: "stop" | "delete" | "enable";
    }) => {
      if (action === "delete") await api.deleteTunnel(tunnel.id);
      else await api.tunnelAction(tunnel.id, action);
    },
    onSuccess: async () => {
      setConfirmation(null);
      await qc.invalidateQueries({ queryKey: key });
    },
  });
  const all = tunnels.data?.tunnels || [];
  const items = all.filter(
    (t) =>
      (filter === "all" || t.status === filter) &&
      `${t.name} ${t.destination_host} ${t.listen_host}`
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  return (
    <div className="tunnels-page">
      <section className="resource-head">
        <div>
          <small>TCP · NETWORK</small>
          <h2>{w.title}</h2>
          <p>{w.subtitle}</p>
        </div>
        <Button
          type="primary"
          icon={<Plus size={16} />}
          disabled={!allowed}
          onClick={() => setEditing("new")}
        >
          {w.create}
        </Button>
      </section>
      <div className="tunnel-persistence-note">
        <RefreshCw size={16} />
        <span>{w.saved}</span>
      </div>
      {!allowed ? (
        <p>{w.restricted}</p>
      ) : (
        <>
          <div className="tunnel-toolbar">
            <Input
              prefix={<Search size={16} />}
              placeholder={w.search}
              aria-label={w.search}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <Select
              aria-label={w.status}
              value={filter}
              onChange={setFilter}
              options={[
                "all",
                "running",
                "starting",
                "error",
                "stopped",
                "expired",
              ].map((value) => ({ value, label: w[value as keyof Words] }))}
            />
            <span>
              {all.length} {w.total}
            </span>
            <Tooltip title={w.refresh}>
              <Button
                aria-label={w.refresh}
                icon={<RefreshCw size={16} />}
                onClick={() => void tunnels.refetch()}
              />
            </Tooltip>
          </div>
          <ErrorMessage error={tunnels.error || action.error} />
          {items.length === 0 && !tunnels.isLoading && (
            <div className="tunnel-empty">
              <Network size={40} />
              <h3>{all.length ? w.search : w.empty}</h3>
              <p>{w.emptyHelp}</p>
              {!all.length && (
                <Button type="primary" onClick={() => setEditing("new")}>
                  {w.create}
                </Button>
              )}
            </div>
          )}
          {items.map((t) => (
            <article className="tunnel-card" key={t.id}>
              <div className="tunnel-card-head">
                <div>
                  <h3>{t.name}</h3>
                  <TunnelPathStatus tunnel={t} targets={data.targets} />
                </div>
                <div className="tunnel-card-actions">
                  <Button
                    disabled={action.isPending}
                    icon={t.enabled ? <Square size={14} /> : <Play size={14} />}
                    onClick={() =>
                      t.enabled
                        ? setConfirmation({ tunnel: t, action: "stop" })
                        : action.mutate({ tunnel: t, action: "enable" })
                    }
                  >
                    {t.enabled ? w.stop : w.start}
                  </Button>
                  <Tooltip title={w.edit}>
                    <Button
                      aria-label={w.edit}
                      disabled={t.temporary}
                      icon={<Pencil size={15} />}
                      onClick={() => setEditing(t)}
                    />
                  </Tooltip>
                  <Tooltip title={w.remove}>
                    <Button
                      danger
                      aria-label={w.remove}
                      disabled={t.temporary}
                      icon={<Trash2 size={15} />}
                      onClick={() =>
                        setConfirmation({ tunnel: t, action: "delete" })
                      }
                    />
                  </Tooltip>
                </div>
              </div>
              <div className="tunnel-owner-note">
                <Tag>{t.temporary ? w.temporary : w.managed}</Tag>
                <span>
                  {w.creator}: {t.creator_name || t.created_by || "—"}
                </span>
                {t.remote_address && (
                  <span title={t.public_key_fingerprint}>
                    {w.source}: {t.remote_address}
                  </span>
                )}
              </div>
              {t.temporary ? (
                <div className="tunnel-flow" aria-label={w.flow}>
                  <div className="tunnel-flow-nodes">
                    <div className="tunnel-node entry-node">
                      <small>{w.entry}</small>
                      <strong>
                        {t.forward_type === "local"
                          ? w.localUnknown
                          : w.bastion}
                      </strong>
                      <code>
                        {t.forward_type === "remote"
                          ? t.listen_address
                          : t.remote_address}
                      </code>
                    </div>
                    <div className="tunnel-flow-arrow">
                      <span>SSH</span>
                      <ArrowRight aria-label="→" />
                    </div>
                    <div className="tunnel-node exit-node">
                      <small>{w.exit}</small>
                      <strong>
                        {t.forward_type === "remote"
                          ? w.sshClient
                          : data.targets.find((x) => x.id === t.exit_target_id)
                              ?.name || w.missing}
                      </strong>
                    </div>
                    <div className="tunnel-flow-arrow destination-arrow">
                      <ArrowRight aria-label="→" />
                    </div>
                    <div className="tunnel-node destination-node">
                      <small>{w.destination}</small>
                      <strong>
                        {t.forward_type === "local"
                          ? t.destination_host
                          : w.remoteUnknown}
                      </strong>
                      {t.forward_type === "local" && (
                        <code>TCP / {t.destination_port}</code>
                      )}
                    </div>
                  </div>
                  <p className="tunnel-help">{w.temporaryHelp}</p>
                </div>
              ) : (
                <Flow
                  config={t}
                  targets={data.targets}
                  words={w}
                  transport={t.transport}
                />
              )}
              <div className="tunnel-card-metrics">
                <div>
                  <small>{w.totalTraffic}</small>
                  <strong>
                    {formatBytes(
                      (t.traffic?.relay_up || 0) +
                        (t.traffic?.relay_down || 0) +
                        (t.traffic?.direct_up || 0) +
                        (t.traffic?.direct_down || 0),
                    )}
                  </strong>
                  <span>
                    ↑{" "}
                    {formatBytes(
                      (t.traffic?.relay_up || 0) + (t.traffic?.direct_up || 0),
                    )}{" "}
                    · ↓{" "}
                    {formatBytes(
                      (t.traffic?.relay_down || 0) +
                        (t.traffic?.direct_down || 0),
                    )}
                  </span>
                </div>
                <div>
                  <small>{w.connections}</small>
                  <strong>{t.connections}</strong>
                  <span>
                    {w.p2p}: {t.direct_connections || 0}
                  </span>
                </div>
                <div>
                  <small>{w.opened}</small>
                  <strong>{t.traffic?.connections_opened || 0}</strong>
                  <span>
                    {w.peak}: {t.traffic?.peak_connections || 0}
                  </span>
                </div>
                <Button
                  icon={<ChartNoAxesCombined size={16} />}
                  onClick={() => setTraffic(t)}
                >
                  {w.traffic}
                </Button>
              </div>
              <div className="tunnel-card-foot">
                <span>
                  <Clock3 size={14} />
                  {t.expires_at
                    ? `${w.expires}: ${new Date(t.expires_at).toLocaleString(locale)}`
                    : t.temporary
                      ? w.temporary
                      : w.permanent}
                </span>
                <span>
                  {w.connections}: {t.connections}
                </span>
                {t.enabled && t.duration_seconds > 0 && (
                  <Tooltip title={w.renewHelp}>
                    <Button
                      size="small"
                      type="text"
                      onClick={() =>
                        setConfirmation({ tunnel: t, action: "enable" })
                      }
                    >
                      {w.renew}
                    </Button>
                  </Tooltip>
                )}
                {t.listen_address && t.status === "running" && (
                  <code>
                    {w.listen}: {t.listen_address}
                  </code>
                )}
              </div>
              {t.error && (
                <div className="tunnel-runtime-error" role="status">
                  {t.error}
                </div>
              )}
            </article>
          ))}
        </>
      )}
      {traffic && (
        <TunnelTrafficDialog
          tunnel={all.find((t) => t.id === traffic.id) || traffic}
          onClose={() => setTraffic(null)}
        />
      )}
      {editing && (
        <TunnelEditor
          key={`${data.activeOrg.id}-${editing === "new" ? "new" : editing.id}`}
          data={data}
          tunnel={editing === "new" ? null : editing}
          words={w}
          onClose={() => setEditing(null)}
          onSaved={() => qc.invalidateQueries({ queryKey: key })}
        />
      )}
      {confirmation && (
        <ConfirmDialog
          title={confirmation.tunnel.name}
          confirmLabel={
            confirmation.action === "delete"
              ? w.remove
              : confirmation.action === "stop"
                ? w.stop
                : w.renew
          }
          danger={confirmation.action !== "enable"}
          body={
            confirmation.action === "delete"
              ? w.removeConfirm
              : confirmation.action === "stop"
                ? w.stopConfirm
                : w.renewHelp
          }
          onClose={() => setConfirmation(null)}
          onConfirm={() => action.mutate(confirmation)}
        />
      )}
    </div>
  );
}

function TunnelEditor({
  data,
  tunnel,
  words: w,
  onClose,
  onSaved,
}: {
  data: ConsoleData;
  tunnel: Tunnel | null;
  words: Words;
  onClose: () => void;
  onSaved: () => Promise<unknown>;
}) {
  const firstMachine = data.user.is_system_admin
    ? ""
    : data.targets[0]?.id || "__select__";
  const [config, setConfig] = useState<TunnelConfig>(
    tunnel
      ? {
          name: tunnel.name,
          entry_target_id: tunnel.entry_target_id,
          listen_host: tunnel.listen_host,
          listen_port: tunnel.listen_port,
          exit_target_id: tunnel.exit_target_id,
          destination_host: tunnel.destination_host,
          destination_port: tunnel.destination_port,
          duration_seconds: tunnel.duration_seconds,
        }
      : {
          ...blank,
          entry_target_id: firstMachine,
          exit_target_id: firstMachine,
        },
  );
  const [savedID, setSavedID] = useState<string | null>(null);
  const [enable, setEnable] = useState(false);
  const [timed, setTimed] = useState(config.duration_seconds > 0);
  const set = <K extends keyof TunnelConfig>(key: K, value: TunnelConfig[K]) =>
    setConfig((c) => ({ ...c, [key]: value }));
  const machineOptions = [
    ...(data.user.is_system_admin ? [{ value: "", label: w.bastion }] : []),
    ...data.targets.map((t) => ({
      value: t.id,
      label: `${t.name} · ${t.target_type === "agent" ? w.agent : w.ssh}`,
    })),
  ];
  const mutation = useMutation({
    mutationFn: async () => {
      const result =
        tunnel || savedID
          ? await api.updateTunnel(tunnel?.id || savedID!, config)
          : await api.createTunnel({
              ...config,
              organization_id: data.activeOrg.id,
            });
      setSavedID(result.tunnel.id);
      await onSaved();
      if (enable) {
        await api.tunnelAction(result.tunnel.id, "enable");
        await onSaved();
      }
      return result;
    },
    onSuccess: onClose,
  });
  return (
    <Modal
      title={tunnel ? w.edit : w.create}
      wide
      className="tunnel-editor"
      onClose={onClose}
      closeOnEscape={!mutation.isPending}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          mutation.mutate();
        }}
      >
        <div className="tunnel-preview-heading">
          <Network size={17} />
          <strong>{w.flow}</strong>
          <Tag>TCP</Tag>
        </div>
        <Flow config={config} targets={data.targets} words={w} preview />
        {(data.targets.find((t) => t.id === config.entry_target_id)
          ?.proxy_target_id ||
          data.targets.find((t) => t.id === config.exit_target_id)
            ?.proxy_target_id) && <p className="tunnel-help">{w.jumpHelp}</p>}
        <label className="field tunnel-name">
          <span>{w.name}</span>
          <Input
            required
            maxLength={128}
            value={config.name}
            onChange={(e) => set("name", e.target.value)}
            placeholder="Web / Database / TCP"
          />
        </label>
        <div className="tunnel-form-grid">
          <fieldset>
            <legend>
              <Server size={16} />
              {w.entry}
            </legend>
            <label className="field">
              <span>{w.entryMachine}</span>
              <Select
                showSearch
                optionFilterProp="label"
                aria-label={w.entryMachine}
                value={
                  config.entry_target_id === "__select__"
                    ? undefined
                    : config.entry_target_id
                }
                placeholder={w.machineRequired}
                options={machineOptions}
                onChange={(v) => set("entry_target_id", v)}
              />
            </label>
            <div className="tunnel-input-row">
              <label className="field">
                <span>{w.listen}</span>
                <Input
                  required
                  value={config.listen_host}
                  onChange={(e) => set("listen_host", e.target.value)}
                />
              </label>
              <label className="field">
                <span>{w.port}</span>
                <InputNumber
                  aria-label={w.port}
                  min={1}
                  max={65535}
                  value={config.listen_port}
                  onChange={(v) => set("listen_port", v || 1)}
                />
              </label>
            </div>
            <div className="tunnel-bind-presets">
              <Button
                size="small"
                type={
                  config.listen_host === "127.0.0.1" ? "primary" : "default"
                }
                onClick={() => set("listen_host", "127.0.0.1")}
              >
                {w.loopback}
              </Button>
              <Button
                size="small"
                type={config.listen_host === "0.0.0.0" ? "primary" : "default"}
                onClick={() => set("listen_host", "0.0.0.0")}
              >
                {w.public}
              </Button>
            </div>
            <p className="tunnel-help">{w.listenHelp}</p>
          </fieldset>
          <fieldset>
            <legend>
              <Network size={16} />
              {w.exit}
            </legend>
            <label className="field">
              <span>{w.exitMachine}</span>
              <Select
                showSearch
                optionFilterProp="label"
                aria-label={w.exitMachine}
                value={
                  config.exit_target_id === "__select__"
                    ? undefined
                    : config.exit_target_id
                }
                placeholder={w.machineRequired}
                options={machineOptions}
                onChange={(v) => set("exit_target_id", v)}
              />
            </label>
            <div className="tunnel-input-row">
              <label className="field">
                <span>{w.targetHost}</span>
                <Input
                  required
                  value={config.destination_host}
                  onChange={(e) => set("destination_host", e.target.value)}
                />
              </label>
              <label className="field">
                <span>{w.targetPort}</span>
                <InputNumber
                  aria-label={w.targetPort}
                  min={1}
                  max={65535}
                  value={config.destination_port}
                  onChange={(v) => set("destination_port", v || 1)}
                />
              </label>
            </div>
            <p className="tunnel-help">{w.destHelp}</p>
          </fieldset>
        </div>
        {data.targets.find((t) => t.id === config.entry_target_id)
          ?.target_type === "direct" && (
          <div className="tunnel-ssh-note">{w.sshHelp}</div>
        )}
        <p className="tunnel-help tunnel-p2p-help">{w.autoP2P}</p>
        <div className="tunnel-duration">
          <label className="field">
            <span>
              <Clock3 size={14} />
              {w.duration}
            </span>
            <Select
              aria-label={w.duration}
              value={timed ? "timed" : "permanent"}
              options={[
                { value: "permanent", label: w.permanent },
                { value: "timed", label: w.timed },
              ]}
              onChange={(v) => {
                setTimed(v === "timed");
                set("duration_seconds", v === "timed" ? 3600 : 0);
              }}
            />
          </label>
          {timed && (
            <label className="field">
              <span>{w.hours}</span>
              <InputNumber
                aria-label={w.hours}
                min={1 / 60}
                max={8760}
                step={0.25}
                value={config.duration_seconds / 3600}
                onChange={(v) =>
                  set("duration_seconds", Math.round((v || 1) * 3600))
                }
              />
            </label>
          )}
          <p className="tunnel-help">{w.durationHelp}</p>
        </div>
        {tunnel?.enabled && <p className="tunnel-help">{w.editHelp}</p>}
        {!tunnel && (
          <label className="tunnel-enable">
            <Switch checked={enable} onChange={setEnable} />
            <span>{w.enableOnSave}</span>
          </label>
        )}
        <ErrorMessage error={mutation.error} />
        <div className="form-actions">
          <Button onClick={onClose} disabled={mutation.isPending}>
            {w.cancel}
          </Button>
          <Button
            type="primary"
            htmlType="submit"
            loading={mutation.isPending}
            disabled={
              config.entry_target_id === "__select__" ||
              config.exit_target_id === "__select__"
            }
          >
            {w.save}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
