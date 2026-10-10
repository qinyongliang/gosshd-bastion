import { useQuery } from "@tanstack/react-query";
import { Button, Input, Select, Table } from "antd";
import { useEffect, useMemo, useState, type ChangeEvent } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Tunnel, TunnelSourceTraffic, TunnelTraffic } from "../types";
import { ErrorMessage, Modal } from "./ui";

export const tunnelTrafficCopy = {
  "zh-CN": {
    up: "发往目标",
    down: "目标返回",
    upDirection: "入口 → 目标服务",
    downDirection: "目标服务 → 入口",
  },
  en: {
    up: "To destination",
    down: "From destination",
    upDirection: "Entry → Destination service",
    downDirection: "Destination service → Entry",
  },
};

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const k = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4);
  return `${(n / 1024 ** k).toFixed(n / 1024 ** k < 10 ? 2 : 1)} ${["B", "KiB", "MiB", "GiB", "TiB"][k]}`;
}
function localDateTime(seconds: number): string {
  const date = new Date(seconds * 1000);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
export function TunnelTrafficDialog({
  tunnel,
  onClose,
}: {
  tunnel: Tunnel;
  onClose: () => void;
}) {
  const { locale } = useI18n();
  const zh = locale === "zh-CN";
  const words = tunnelTrafficCopy[locale];
  const [days, setDays] = useState(1);
  const [sourceIP, setSourceIP] = useState("");
  const [ipInput, setIPInput] = useState("");
  const [customStart, setCustomStart] = useState(() => localDateTime(Math.floor(Date.now() / 1000) - 86400));
  const [customEnd, setCustomEnd] = useState(() => localDateTime(Math.floor(Date.now() / 1000)));
  const [customRange, setCustomRange] = useState<{from: number; to: number} | null>(null);
  const [sort, setSort] = useState("traffic_desc");
  const [metric, setMetric] = useState("traffic");
  const [path, setPath] = useState("all");
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));
  useEffect(() => {
    const timer = setInterval(
      () => setNow(Math.floor(Date.now() / 1000)),
      30000,
    );
    return () => clearInterval(timer);
  }, []);
  const range = useMemo(
    () => days === 0 && customRange ? customRange : ({
      from: Math.floor(now / 300) * 300 + 300 - (days || 1) * 86400,
      to: Math.floor(now / 300) * 300 + 300,
    }),
    [days, now, customRange],
  );
  const query = useQuery({
    queryKey: ["tunnel-traffic", tunnel.id, range, sourceIP],
    queryFn: () => api.tunnelTraffic(tunnel.id, range.from, range.to, sourceIP),
    refetchInterval: 30000,
  });
  const buckets = query.data?.buckets || [];
  const selected = (b: TunnelTraffic, up: boolean) =>
    path === "direct"
      ? up
        ? b.direct_up
        : b.direct_down
      : path === "relay"
        ? up
          ? b.relay_up
          : b.relay_down
        : up
          ? b.relay_up + b.direct_up
          : b.relay_down + b.direct_down;
  const series =
    metric === "traffic"
      ? [
          {
            label: `${words.up} (${words.upDirection})`,
            color: "#4385ef",
            values: buckets.map((b) => selected(b, true)),
          },
          {
            label: `${words.down} (${words.downDirection})`,
            color: "#32a987",
            values: buckets.map((b) => selected(b, false)),
          },
        ]
      : [
          {
            label: zh ? "峰值并发" : "Peak concurrency",
            color: "#8474d9",
            values: buckets.map((b) => b.peak_connections),
          },
          {
            label: zh ? "新建连接" : "New connections",
            color: "#4385ef",
            values: buckets.map((b) => b.connections_opened),
          },
        ];
  const total = buckets.reduce(
    (s, b) => s + selected(b, true) + selected(b, false),
    0,
  );
  const opened = buckets.reduce((s, b) => s + b.connections_opened, 0);
  const peak = Math.max(0, ...buckets.map((b) => b.peak_connections));
  const sources = [...(query.data?.sources || [])].sort((a, b) => {
    const value = (row: TunnelSourceTraffic) => sort.startsWith("traffic")
      ? selected(row, true) + selected(row, false) : row.connections_opened;
    return (value(a) - value(b)) * (sort.endsWith("asc") ? 1 : -1) || a.source_ip.localeCompare(b.source_ip);
  });
  const filterSource = (ip: string) => { setSourceIP(ip.trim()); setIPInput(ip.trim()); };
  const customFrom = Math.floor(new Date(customStart).getTime() / 300000) * 300;
  const customTo = Math.ceil(new Date(customEnd).getTime() / 300000) * 300;
  const validCustom = Number.isFinite(customFrom) && Number.isFinite(customTo)
    && new Date(customEnd).getTime() > new Date(customStart).getTime()
    && customFrom >= 0 && customTo - customFrom <= 31 * 86400;
  return (
    <Modal
      title={`${tunnel.name} · ${zh ? "统计" : "Statistics"}`}
      wide
      className="tunnel-traffic-dialog"
      onClose={onClose}
    >
      <div className="tunnel-chart-toolbar">
        <Select
          aria-label={zh ? "时间范围" : "Time range"}
          value={days}
          onChange={setDays}
          options={[...[1, 7, 30].map((d) => ({
            value: d,
            label: zh
              ? d === 1
                ? "最近 24 小时"
                : `最近 ${d} 天`
              : d === 1
                ? "Last 24 hours"
                : `Last ${d} days`,
          })), {value: 0, label: zh ? "自定义时间" : "Custom range"}]}
        />
        <div className="tunnel-chart-tabs">
          <Button
            type={metric === "traffic" ? "primary" : "default"}
            onClick={() => setMetric("traffic")}
          >
            {zh ? "流量" : "Traffic"}
          </Button>
          <Button
            type={metric === "connections" ? "primary" : "default"}
            onClick={() => setMetric("connections")}
          >
            {zh ? "连接数" : "Connections"}
          </Button>
        </div>
        {metric === "traffic" && (
          <Select
            aria-label={zh ? "传输路径" : "Transport path"}
            value={path}
            onChange={setPath}
            options={[
              { value: "all", label: zh ? "全部路径" : "All paths" },
              { value: "relay", label: zh ? "服务器中转" : "Server relay" },
              { value: "direct", label: zh ? "直连" : "Direct" },
            ]}
          />
        )}
      </div>
      {days === 0 && <div className="tunnel-statistics-range">
        <label>{zh ? "开始时间" : "Start time"}<Input type="datetime-local" value={customStart} onChange={(event: ChangeEvent<HTMLInputElement>) => setCustomStart(event.target.value)} /></label>
        <label>{zh ? "结束时间" : "End time"}<Input type="datetime-local" value={customEnd} onChange={(event: ChangeEvent<HTMLInputElement>) => setCustomEnd(event.target.value)} /></label>
        <Button disabled={!validCustom} onClick={() => setCustomRange({from: customFrom, to: customTo})}>{zh ? "应用时间范围" : "Apply range"}</Button>
        {!validCustom && <span role="alert">{zh ? "请选择有效的时间范围，最长 31 天" : "Select a valid time range of up to 31 days"}</span>}
      </div>}
      <div className="tunnel-statistics-filter">
        <Input.Search aria-label={zh ? "来源 IP" : "Source IP"} placeholder={zh ? "输入 IP 查看统计" : "Enter an IP to view statistics"}
          value={ipInput} onChange={(event: ChangeEvent<HTMLInputElement>) => setIPInput(event.target.value)} onSearch={filterSource}
          enterButton={<Button aria-label={zh ? "筛选" : "Filter"}>{zh ? "筛选" : "Filter"}</Button>} />
        <Button onClick={() => filterSource("")}>{zh ? "全部 IP" : "All IPs"}</Button>
      </div>
      {sourceIP && <p className="tunnel-statistics-note">{zh ? "当前来源" : "Selected source"}: {sourceIP === "unknown" ? (zh ? "未知来源" : "Unknown source") : sourceIP}</p>}
      <div className="tunnel-chart-summary">
        <div>
          <small>
            {zh ? "所选时段流量" : "Traffic in range"}
          </small>
          <strong>{formatBytes(total)}</strong>
        </div>
        <div>
          <small>{zh ? "新建连接" : "Connections opened"}</small>
          <strong>{opened}</strong>
        </div>
        <div>
          <small>{zh ? "峰值并发" : "Peak concurrency"}</small>
          <strong>{peak}</strong>
        </div>
      </div>
      <ErrorMessage error={query.error} />
      {query.isLoading ? (
        <p>{zh ? "加载统计…" : "Loading statistics…"}</p>
      ) : (
        <TrafficChart
          buckets={buckets}
          series={series}
          locale={locale}
          bytes={metric === "traffic"}
        />
      )}
      <div className="tunnel-statistics-heading">
        <h3>{zh ? "IP 来源统计" : "Source IP statistics"}</h3>
        <Select aria-label={zh ? "排序" : "Sort by"} value={sort} onChange={setSort} options={[
          {value: "traffic_desc", label: zh ? "流量从高到低" : "Traffic: high to low"},
          {value: "traffic_asc", label: zh ? "流量从低到高" : "Traffic: low to high"},
          {value: "connections_desc", label: zh ? "连接数从高到低" : "Connections: high to low"},
          {value: "connections_asc", label: zh ? "连接数从低到高" : "Connections: low to high"},
        ]} />
      </div>
      <Table<TunnelSourceTraffic> className="tunnel-statistics-table" size="small" rowKey="source_ip" dataSource={sources}
        loading={query.isLoading} scroll={{x: 720}} pagination={{pageSize: 10, showSizeChanger: false, hideOnSinglePage: true}}
        locale={{emptyText: zh ? "所选条件下暂无来源统计" : "No source statistics for this selection"}}
        columns={[
          {title: zh ? "来源 IP" : "Source IP", dataIndex: "source_ip", render: (ip: string) => <Button type="link" onClick={() => filterSource(ip)}>{ip === "unknown" ? (zh ? "未知来源" : "Unknown source") : ip}</Button>},
          {title: words.up, key: "up", render: (_: unknown, row: TunnelSourceTraffic) => formatBytes(selected(row, true))},
          {title: words.down, key: "down", render: (_: unknown, row: TunnelSourceTraffic) => formatBytes(selected(row, false))},
          {title: zh ? "总流量" : "Total traffic", key: "total", render: (_: unknown, row: TunnelSourceTraffic) => formatBytes(selected(row, true) + selected(row, false))},
          {title: zh ? "新建连接" : "Connections opened", dataIndex: "connections_opened"},
          {title: zh ? "峰值并发" : "Peak concurrency", dataIndex: "peak_connections"},
          {title: zh ? "当前并发" : "Active now", dataIndex: "active_connections"},
        ]} />
      <p className="tunnel-statistics-note">{zh ? "按 5 分钟汇总，时间范围会扩展至完整统计区间；新建连接为所选时段建立的连接数。点击 IP 可查看该来源的趋势和统计。" : "Aggregated in five-minute buckets; ranges expand to complete buckets. Opened connections counts connections established in the selected range. Click an IP to view its trend and statistics."}</p>
      <div className="tunnel-chart-footer">
        <span>
          {zh ? "当前并发" : "Active now"}: {query.data?.active_connections || 0}
        </span>
        {!sourceIP && <span>
          {zh ? "累计连接" : "Lifetime connections"}:{" "}
          {tunnel.traffic?.connections_opened || 0}
        </span>}
      </div>
    </Modal>
  );
}
function TrafficChart({
  buckets,
  series,
  locale,
  bytes,
}: {
  buckets: TunnelTraffic[];
  series: { label: string; color: string; values: number[] }[];
  locale: string;
  bytes: boolean;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const maxValue = Math.max(1, ...series.flatMap((s) => s.values));
  const maxY = bytes ? maxValue : Math.max(1, Math.ceil(maxValue));
  const width = 1000,
    height = 270,
    left = 70,
    right = 20,
    top = 15,
    bottom = 38;
  const plotWidth = width - left - right,
    plotHeight = height - top - bottom;
  const x = (i: number) =>
    left + (i * plotWidth) / Math.max(1, buckets.length - 1);
  const y = (v: number) => height - bottom - (v * plotHeight) / maxY;
  const stamp = (i: number) =>
    new Date((buckets[i]?.bucket_start || 0) * 1000).toLocaleString(locale, {
      month: "numeric",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  const ticks = [...new Set([0, Math.floor(buckets.length / 2), Math.max(0, buckets.length - 1)])];
  return (
    <div className="tunnel-chart">
      <div className="tunnel-chart-legend">
        {series.map((s) => (
          <span key={s.label}>
            <i style={{ background: s.color }} />
            {s.label}
          </span>
        ))}
      </div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={
          locale === "zh-CN"
            ? "隧道流量和连接趋势"
            : "Tunnel traffic and connection trend"
        }
        onMouseLeave={() => setHover(null)}
        onPointerMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          const cursor = ((e.clientX - rect.left) / rect.width) * width;
          setHover(
            Math.max(
              0,
              Math.min(
                buckets.length - 1,
                Math.round(
                  ((cursor - left) / plotWidth) * (buckets.length - 1),
                ),
              ),
            ),
          );
        }}
      >
        {[0, 0.25, 0.5, 0.75, 1].map((r) => (
          <g key={r}>
            <line
              x1={left}
              x2={width - right}
              y1={y(r * maxY)}
              y2={y(r * maxY)}
              className="tunnel-chart-grid"
            />
            <text x={left - 8} y={y(r * maxY) + 4} textAnchor="end">
              {bytes
                ? formatBytes(Math.round(r * maxY))
                : Number((r * maxY).toFixed(1))}
            </text>
          </g>
        ))}
        {series.map((s) => (
          s.values.length === 1 ? <circle key={s.label} cx={x(0)} cy={y(s.values[0])} r={4} fill={s.color} /> : <polyline
            key={s.label}
            points={s.values.map((v, i) => `${x(i)},${y(v)}`).join(" ")}
            fill="none"
            stroke={s.color}
            strokeWidth={2}
            vectorEffect="non-scaling-stroke"
          />
        ))}
        {ticks.map((i, k) => (
          <text
            key={k}
            x={x(i)}
            y={height - 10}
            textAnchor={k === 0 ? "start" : k === ticks.length - 1 ? "end" : "middle"}
          >
            {stamp(i)}
          </text>
        ))}
        {hover !== null && buckets[hover] && (
          <g>
            <line
              x1={x(hover)}
              x2={x(hover)}
              y1={top}
              y2={height - bottom}
              stroke="currentColor"
              strokeDasharray="4 4"
              opacity=".3"
            />
            {series.map((s) => (
              <circle
                key={s.label}
                cx={x(hover)}
                cy={y(s.values[hover])}
                r={4}
                fill={s.color}
              />
            ))}
          </g>
        )}
      </svg>
      <div className="tunnel-chart-readout" aria-live="polite">
        {hover !== null && buckets[hover] ? (
          <>
            <strong>{stamp(hover)}</strong>
            {series.map((s) => (
              <span key={s.label}>
                {s.label}:{" "}
                {bytes ? formatBytes(s.values[hover]) : s.values[hover]}
              </span>
            ))}
          </>
        ) : (
          <span>
            {locale === "zh-CN"
              ? "移动鼠标查看具体时间段的数据"
              : "Move over the chart to inspect a time bucket"}
          </span>
        )}
      </div>
    </div>
  );
}
