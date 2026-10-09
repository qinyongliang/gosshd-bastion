import { Popover, Tag } from "antd";
import { ArrowRight, Network } from "lucide-react";
import { useI18n } from "../i18n";
import type { Target, Tunnel, TunnelPeerInfo } from "../types";

const words = {
  "zh-CN": {
    title: "实际网络路径",
    running: "运行中",
    starting: "连接中",
    error: "连接失败 · 自动重试",
    stopped: "已停止",
    expired: "已到期",
    direct: "直连",
    mixed: "部分直连",
    negotiating: "中转 · 尝试直连",
    relay: "服务器中转",
    entry: "入口",
    exit: "出口",
    nic: "出口网卡",
    local: "本地地址",
    selected: "选中直连地址",
    remote: "对端地址",
    protocol: "传输协议",
    candidate: "连接方式",
    rtt: "往返延迟",
    pending: "等待路径更新",
    unknown: "未识别",
    noPair: "尚未选中直连路径",
    host: "本地网卡候选",
    srflx: "STUN 公网映射",
    prflx: "对端发现映射",
    turn: "TURN 候选",
    connection: "连接",
    updated: "上报时间",
    relayHelp: "暂无连接路径。",
    stoppedHelp: "当前没有运行中的隧道连接。",
    shown: "显示最近上报的连接",
    details: "查看网络路径",
    lastPath: "以下为最近选中的直连路径，当前数据使用中转。",
  },
  en: {
    title: "Actual network path",
    running: "Running",
    starting: "Connecting",
    error: "Connection failed · retrying",
    stopped: "Stopped",
    expired: "Expired",
    direct: "Direct",
    mixed: "Partially direct",
    negotiating: "Relay · trying direct",
    relay: "Server relay",
    entry: "Entry",
    exit: "Exit",
    nic: "Egress interface",
    local: "Local address",
    selected: "Selected address",
    remote: "Peer address",
    protocol: "Protocol",
    candidate: "Candidate type",
    rtt: "Round-trip time",
    pending: "Waiting for path update",
    unknown: "Unidentified",
    noPair: "No direct path selected yet",
    host: "Local interface candidate",
    srflx: "STUN public mapping",
    prflx: "Peer-reflexive mapping",
    turn: "TURN candidate",
    connection: "Connection",
    updated: "Reported at",
    relayHelp: "No connection paths yet.",
    stoppedHelp: "There are no active tunnel connections.",
    shown: "Most recently reported connections",
    details: "Inspect network path",
    lastPath:
      "This is the last selected direct path. Traffic currently uses relay.",
  },
};
const address = (host: string, port?: number) =>
  `${host.includes(":") ? `[${host}]` : host}${port ? `:${port}` : ""}`;
export function TunnelPathStatus({
  tunnel,
  targets,
}: {
  tunnel: Tunnel;
  targets: Target[];
}) {
  const { locale } = useI18n();
  const w = words[locale];
  const colors: Record<string, string> = {
    running: "green",
    starting: "blue",
    error: "red",
    expired: "orange",
  };
  const machine = (id: string) =>
    targets.find((t) => t.agent_id === id)?.name || id;
  const content = (
    <div className="tunnel-path-content">
      {!tunnel.paths?.length && (
        <p>{tunnel.enabled ? w.relayHelp : w.stoppedHelp}</p>
      )}
      {tunnel.paths?.map((path) => (
        <section key={path.id} className="tunnel-path-connection">
          <header>
            <Network size={14} />
            <strong>
              {w.connection} {path.id.slice(0, 8)}
            </strong>
            <Tag color={path.entry?.active ? "cyan" : undefined}>
              {path.entry?.active ? w.direct : w.relay}
            </Tag>
          </header>
          <div className="tunnel-path-peers">
            <PeerEndpoint
              label={`${w.entry} · ${machine(path.entry_agent_id)}`}
              info={path.entry}
              words={w}
              locale={locale}
            />
            <ArrowRight className="tunnel-path-between" />
            <PeerEndpoint
              label={`${w.exit} · ${machine(path.exit_agent_id)}`}
              info={path.exit}
              words={w}
              locale={locale}
            />
          </div>
        </section>
      ))}
      {!!tunnel.paths?.length && (
        <small className="tunnel-path-limit">
          {w.shown}: {tunnel.paths.length} / {tunnel.connections}
        </small>
      )}
      {tunnel.error && <p className="tunnel-runtime-error">{tunnel.error}</p>}
    </div>
  );
  return (
    <Popover
      title={w.title}
      content={content}
      trigger={["hover", "focus", "click"]}
      placement="bottomLeft"
    >
      <span
        className="tunnel-status-trigger"
        role="button"
        tabIndex={0}
        aria-label={`${tunnel.name} · ${w.details}`}
      >
        <Tag color={colors[tunnel.status]}>{w[tunnel.status]}</Tag>
        {tunnel.enabled && (
          <Tag color={tunnel.transport === "direct" ? "cyan" : undefined}>
            {w[tunnel.transport]}
          </Tag>
        )}
      </span>
    </Popover>
  );
}
function PeerEndpoint({
  label,
  info,
  words: w,
  locale,
}: {
  label: string;
  info?: TunnelPeerInfo;
  words: (typeof words)["en"];
  locale: string;
}) {
  const peerStates: Record<string, string> =
    locale === "zh-CN"
      ? {
          new: "待连接",
          connecting: "协商中",
          connected: "已连接",
          disconnected: "连接断开",
          failed: "连接失败",
          closed: "已关闭",
        }
      : {};
  const candidate = info?.local;
  const candidateLabel =
    candidate?.type === "host"
      ? w.host
      : candidate?.type === "srflx"
        ? w.srflx
        : candidate?.type === "prflx"
          ? w.prflx
          : candidate?.type === "relay"
            ? w.turn
            : candidate?.type;
  return (
    <div className="tunnel-peer-endpoint">
      <strong>{label}</strong>
      {!info ? (
        <p>{w.pending}</p>
      ) : (
        <>
          <span className="tunnel-peer-state">
            {info.active ? w.direct : w.relay} ·{" "}
            {peerStates[info.state] || info.state}
          </span>
          {!info.active && candidate && <small>{w.lastPath}</small>}
          {!candidate ? (
            <p>{w.noPair}</p>
          ) : (
            <dl>
              <dt>{w.nic}</dt>
              <dd>{candidate.interface || w.unknown}</dd>
              <dt>{w.local}</dt>
              <dd>
                <code>
                  {candidate.local_address
                    ? address(candidate.local_address, candidate.local_port)
                    : w.unknown}
                </code>
              </dd>
              <dt>{w.selected}</dt>
              <dd>
                <code>{address(candidate.address, candidate.port)}</code>
              </dd>
              <dt>{w.remote}</dt>
              <dd>
                <code>
                  {info.remote
                    ? address(info.remote.address, info.remote.port)
                    : w.unknown}
                </code>
              </dd>
              <dt>{w.protocol}</dt>
              <dd>
                {candidate.protocol.toUpperCase()} · {candidate.network}
              </dd>
              <dt>{w.candidate}</dt>
              <dd>{candidateLabel}</dd>
              {!!info.rtt_ms && (
                <>
                  <dt>{w.rtt}</dt>
                  <dd>{info.rtt_ms.toFixed(1)} ms</dd>
                </>
              )}
            </dl>
          )}
          <small>
            {w.updated}: {new Date(info.updated_at).toLocaleTimeString(locale)}
          </small>
        </>
      )}
    </div>
  );
}
