import { useQuery } from "@tanstack/react-query";
import { Button, Select, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Tunnel, TunnelTraffic } from "../types";
import { ErrorMessage, Modal } from "./ui";

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const k = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4);
  return `${(n / 1024 ** k).toFixed(n / 1024 ** k < 10 ? 2 : 1)} ${["B", "KiB", "MiB", "GiB", "TiB"][k]}`;
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
  const [days, setDays] = useState(1);
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
    () => ({
      from: Math.floor(now / 300) * 300 + 300 - days * 86400,
      to: Math.floor(now / 300) * 300 + 300,
    }),
    [days, now],
  );
  const query = useQuery({
    queryKey: ["tunnel-traffic", tunnel.id, range],
    queryFn: () => api.tunnelTraffic(tunnel.id, range.from, range.to),
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
            label: zh ? "上传" : "Upload",
            color: "#4385ef",
            values: buckets.map((b) => selected(b, true)),
          },
          {
            label: zh ? "下载" : "Download",
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
  return (
    <Modal
      title={`${tunnel.name} · ${zh ? "流量与连接" : "Traffic & connections"}`}
      wide
      className="tunnel-traffic-dialog"
      onClose={onClose}
    >
      <div className="tunnel-chart-toolbar">
        <Select
          aria-label={zh ? "时间范围" : "Time range"}
          value={days}
          onChange={setDays}
          options={[1, 7, 30].map((d) => ({
            value: d,
            label: zh
              ? d === 1
                ? "最近 24 小时"
                : `最近 ${d} 天`
              : d === 1
                ? "Last 24 hours"
                : `Last ${d} days`,
          }))}
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

      <div className="tunnel-chart-summary">
        <div>
          <small>{zh ? "所选时段流量" : "Traffic in range"}</small>
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
      <div className="tunnel-chart-footer">
        <Tag>
          {zh ? "审核库 · 5 分钟聚合" : "Audit database · 5-minute buckets"}
        </Tag>
        <span>
          {zh ? "当前并发" : "Active now"}: {tunnel.connections}
        </span>
        <span>
          {zh ? "累计连接" : "Lifetime connections"}:{" "}
          {tunnel.traffic?.connections_opened || 0}
        </span>
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
          <polyline
            key={s.label}
            points={s.values.map((v, i) => `${x(i)},${y(v)}`).join(" ")}
            fill="none"
            stroke={s.color}
            strokeWidth={2}
            vectorEffect="non-scaling-stroke"
          />
        ))}
        {[
          0,
          Math.floor(buckets.length / 2),
          Math.max(0, buckets.length - 1),
        ].map((i, k) => (
          <text
            key={k}
            x={x(i)}
            y={height - 10}
            textAnchor={k === 0 ? "start" : k === 2 ? "end" : "middle"}
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
