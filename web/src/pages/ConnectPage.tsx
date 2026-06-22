import { useQuery } from "@tanstack/react-query";
import { Terminal } from "@xterm/xterm";
import { Activity, ArrowLeft, ChevronLeft, ChevronRight, Copy, Cpu, FileText, Globe, HardDrive, Maximize, Minimize, Monitor, Network, RefreshCw, Server, Shield, Unplug } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api";
import { CopyButton, Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { useTheme } from "../theme";
import type { ConsoleData, Target, TargetSystemSnapshot, TargetSystemUsage } from "../types";
import { targetEndpoint } from "../utils";
import { FileManager } from "./FileManager";

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error";
type MetricSample = {
  at: number;
  cpu: number;
  memory: number;
  swap: number;
  rx: number;
  tx: number;
};

const DEFAULT_COLS = 120;
const DEFAULT_ROWS = 32;
const SYSTEM_REFRESH_MS = 5000;
const MAX_SYSTEM_SAMPLES = 60;

export function ConnectPage({ data }: { data: ConsoleData }) {
  const { targetID } = useParams<{ targetID: string }>();
  const { t } = useI18n();
  const target = data.targets.find((item) => item.id === targetID);

  if (!target) {
    return (
      <main className="connect-workspace empty">
        <section className="connect-error">
          <div className="connect-error-icon"><Server /></div>
          <h2>{t("connect")}</h2>
          <p>{t("serviceEmptyBody")}</p>
          <a className="button-link" href="/targets">
            <ArrowLeft />{t("connectBack")}
          </a>
        </section>
      </main>
    );
  }

  return <ConnectWorkspace data={data} target={target} />;
}

function ConnectWorkspace({ data, target }: { data: ConsoleData; target: Target }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const endpoint = targetEndpoint(target);
  const sshCommand = `ssh -p ${data.runtime.ssh_port || 22} ${target.alias}@${data.runtime.ssh_host || location.hostname}`;

  return (
    <main className="connect-workspace">
      <header className="connect-appbar">
        <div className="connect-appbar-brand">
          <div className="connect-appbar-mark">g</div>
          <div className="connect-appbar-title">
            <strong>gosshd</strong>
            <span>{t("shellProduct")}</span>
          </div>
        </div>

        <button
          type="button"
          className="icon-button connect-sidebar-toggle"
          onClick={() => setSidebarOpen((prev) => !prev)}
          aria-label={sidebarOpen ? t("connectCollapseSidebar") : t("connectExpandSidebar")}
          title={sidebarOpen ? t("connectCollapseSidebar") : t("connectExpandSidebar")}
        >
          {sidebarOpen ? <ChevronLeft /> : <ChevronRight />}
        </button>

        <div className="connect-appbar-host">
          <Server />
          <div>
            <strong>{target.name}</strong>
            <code>{target.alias}</code>
          </div>
        </div>

        <div className="connect-appbar-meta">
          <span className="connect-appbar-endpoint"><Globe />{endpoint}</span>
          <span className="connect-appbar-type">
            {target.target_type === "agent" ? <Monitor /> : <HardDrive />}
            {target.target_type === "agent" ? t("privateNode") : t("serviceDirect")}
          </span>
          <span className="connect-appbar-auth"><Shield />{target.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword")}</span>
        </div>

        <div className="connect-appbar-actions">
          <Segmented value={locale} items={[["en", "EN"], ["zh-CN", t("languageChinese")]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          <Segmented value={theme} items={[["dark", t("themeDark")], ["light", t("themeLight")]]} onChange={(value) => setTheme(value as "light" | "dark")} />
          <a className="button-link back" href="/targets"><ArrowLeft />{t("connectBack")}</a>
        </div>
      </header>

      <section className="connect-body">
        <aside className={`connect-host-panel ${sidebarOpen ? "" : "collapsed"}`}>
          <section className="connect-panel compact">
            <h3><Monitor />{t("connectHostInfo")}</h3>
            <dl className="connect-host-list">
              <div><dt>{t("serviceName")}</dt><dd>{target.name}</dd></div>
              <div><dt>{t("serviceAlias")}</dt><dd><code>{target.alias}</code></dd></div>
              <div><dt>{t("targetHost")}</dt><dd>{target.host || "-"}</dd></div>
              <div><dt>{t("targetPort")}</dt><dd>{target.port || 22}</dd></div>
              <div><dt>{t("serviceRemoteUser")}</dt><dd>{target.remote_username}</dd></div>
              <div><dt>{t("serviceAuthType")}</dt><dd>{target.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword")}</dd></div>
              <div><dt>{t("commonTag")}</dt><dd>{(target.tags || []).join(", ") || "-"}</dd></div>
            </dl>
          </section>
          <section className="connect-panel compact connect-command-panel">
            <h3><Copy />{t("copyConnectionCommand")}</h3>
            <code className="connect-command">{sshCommand}</code>
            <CopyButton value={sshCommand} label={t("copyConnectionCommand")} />
          </section>
          <SystemSnapshotPanel targetID={target.id} />
        </aside>

        <section className="connect-main">
          <div className="connect-zone terminal-zone">
            <div className="connect-zone-head">
              <span><Monitor />{t("connectTerminalTitle")}</span>
            </div>
            <TerminalPanel target={target} />
          </div>
          <div className="connect-zone files-zone">
            <div className="connect-zone-head">
              <span><FileText />{t("connectFilesTitle")}</span>
            </div>
            <FileManager target={target} />
          </div>
        </section>
      </section>
    </main>
  );
}

function SystemSnapshotPanel({ targetID }: { targetID: string }) {
  const { t } = useI18n();
  const [samples, setSamples] = useState<MetricSample[]>([]);
  const system = useQuery({
    queryKey: ["target-system", targetID],
    queryFn: () => api.targetSystem(targetID),
    refetchInterval: SYSTEM_REFRESH_MS,
    staleTime: 4000,
    retry: 1,
  });
  const snapshot = system.data;
  const networkTrend = buildNetworkRates(samples);

  useEffect(() => {
    setSamples([]);
  }, [targetID]);

  useEffect(() => {
    if (!snapshot) return;
    const sample = snapshotToSample(snapshot);
    setSamples((current) => [...current, sample].slice(-MAX_SYSTEM_SAMPLES));
  }, [snapshot]);

  return (
    <section className="connect-panel compact telemetry-panel">
      <header className="telemetry-head">
        <h3><Activity />{t("connectSystemInfo")}</h3>
        <button type="button" className="icon-button" onClick={() => system.refetch()} disabled={system.isFetching} title={t("commonRefresh")}>
          <RefreshCw />
        </button>
      </header>

      {!snapshot && (
        <div className="telemetry-empty">
          <strong>{system.isLoading ? t("loading") : t("connectSystemUnavailable")}</strong>
          {system.error ? <span>{String((system.error as Error).message || "")}</span> : <span>{t("connectSystemHint")}</span>}
        </div>
      )}

      {snapshot && (
        <>
          <div className="telemetry-ip-card">
            <span>{t("connectSystemIP")}</span>
            <strong title={snapshot.ip || snapshot.hostname || "-"}>{snapshot.ip || snapshot.hostname || "-"}</strong>
            <small>{[snapshot.os, snapshot.hostname].filter(Boolean).join(" / ") || "-"}</small>
          </div>

          <dl className="telemetry-summary">
            <div>
              <dt>{t("connectSystemUptime")}</dt>
              <dd title={snapshot.uptime || "-"}>{snapshot.uptime || "-"}</dd>
            </div>
            <div>
              <dt>{t("connectSystemLoad")}</dt>
              <dd>{snapshot.load || "-"}</dd>
            </div>
          </dl>

          <ResourceMeter icon={<Cpu />} label={t("connectSystemCPU")} percent={snapshot.cpu_percent} trend={samples.map((item) => item.cpu)} />
          <ResourceMeter label={t("connectSystemMemory")} usage={snapshot.memory} trend={samples.map((item) => item.memory)} />
          <ResourceMeter label={t("connectSystemSwap")} usage={snapshot.swap} trend={samples.map((item) => item.swap)} />

          <section className="telemetry-block">
            <h4>{t("connectSystemProcesses")}</h4>
            <div className="telemetry-process-list">
              {(snapshot.processes || []).slice(0, 5).map((item) => (
                <div className="telemetry-process" key={`${item.command}-${item.rss_bytes}-${item.cpu_percent}`}>
                  <strong title={item.command}>{item.command}</strong>
                  <span>{formatBytes(item.rss_bytes)}</span>
                  <span>{item.cpu_percent.toFixed(1)}%</span>
                </div>
              ))}
              {!snapshot.processes?.length && <p>{t("connectSystemNoData")}</p>}
            </div>
          </section>

          <section className="telemetry-block">
            <h4><Network />{t("connectSystemNetwork")}</h4>
            <div className="telemetry-trend-pair">
              <TrendLine label={t("connectSystemRX")} values={networkTrend.rx} />
              <TrendLine label={t("connectSystemTX")} values={networkTrend.tx} />
            </div>
            <div className="telemetry-network-list">
              {(snapshot.network || []).slice(0, 4).map((item) => (
                <div className="telemetry-network" key={item.interface}>
                  <strong title={item.interface}>{item.interface}</strong>
                  <span>↓ {formatBytes(item.rx_bytes)}</span>
                  <span>↑ {formatBytes(item.tx_bytes)}</span>
                </div>
              ))}
              {!snapshot.network?.length && <p>{t("connectSystemNoData")}</p>}
            </div>
          </section>

          <section className="telemetry-block">
            <h4><HardDrive />{t("connectSystemFilesystems")}</h4>
            <div className="telemetry-disk-list">
              {(snapshot.filesystems || []).slice(0, 12).map((item) => (
                <div className="telemetry-disk" key={item.path}>
                  <div>
                    <strong title={item.path}>{item.path}</strong>
                    <span>{formatBytes(item.used_bytes)}/{formatBytes(item.total_bytes)}</span>
                  </div>
                  <Meter percent={item.percent} />
                </div>
              ))}
              {!snapshot.filesystems?.length && <p>{t("connectSystemNoData")}</p>}
            </div>
          </section>
        </>
      )}
    </section>
  );
}

function ResourceMeter({ icon, label, percent, usage, trend }: { icon?: ReactNode; label: string; percent?: number; usage?: TargetSystemUsage; trend?: number[] }) {
  const value = clampNumber(percent ?? usage?.percent ?? 0);
  return (
    <div className="resource-meter">
      <div className="resource-meter-head">
        <span>{icon}{label}</span>
        <strong>{value.toFixed(0)}%</strong>
      </div>
      <Meter percent={value} />
      {usage && usage.total_bytes > 0 && (
        <small>{formatBytes(usage.used_bytes)}/{formatBytes(usage.total_bytes)}</small>
      )}
      <TrendLine label={label} values={trend || []} max={100} compact />
    </div>
  );
}

function Meter({ percent }: { percent: number }) {
  const value = clampNumber(percent);
  return (
    <div className="meter-track" aria-label={`${value.toFixed(0)}%`}>
      <span style={{ width: `${value}%` }} />
    </div>
  );
}

function TrendLine({ label, values, max, compact = false }: { label: string; values: number[]; max?: number; compact?: boolean }) {
  const points = sparklinePoints(values, max);
  const latest = values.length ? values[values.length - 1] : 0;
  return (
    <div className={compact ? "trend-line compact" : "trend-line"} title={`${label}: ${latest.toFixed(1)}`}>
      <svg viewBox="0 0 100 30" preserveAspectRatio="none" aria-label={label}>
        <polyline points={points} />
      </svg>
      {!compact && <span>{label}</span>}
    </div>
  );
}

function TerminalPanel({ target }: { target: Target }) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [error, setError] = useState("");
  const [dims, setDims] = useState({ cols: DEFAULT_COLS, rows: DEFAULT_ROWS });
  const [isFullscreen, setIsFullscreen] = useState(false);
  const fitRetryRef = useRef<number | null>(null);

  const connect = () => {
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
    setStatus("connecting");
    setError("");
    const terminal = terminalRef.current;
    if (!terminal) return;

    const cols = terminal.cols || DEFAULT_COLS;
    const rows = terminal.rows || DEFAULT_ROWS;
    const url = api.targetTerminalURL(target.id, cols, rows);
    const socket = new WebSocket(url);
    socketRef.current = socket;

    socket.onopen = () => {
      setStatus("connected");
      setDims({ cols, rows });
    };

    socket.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as { type: string; data?: string; code?: number; cols?: number; rows?: number };
        if (message.type === "output" && message.data !== undefined) {
          terminal.write(message.data);
        } else if (message.type === "error" && message.data !== undefined) {
          terminal.write(`\r\n\x1b[1;31m${message.data}\x1b[0m\r\n`);
          setStatus("error");
          setError(message.data);
        } else if (message.type === "exit") {
          terminal.write(`\r\n\x1b[2;37mSession ended (exit ${message.code ?? "-"})\x1b[0m\r\n`);
          setStatus("disconnected");
        }
      } catch {
        terminal.write(event.data);
      }
    };

    socket.onerror = () => {
      setStatus("error");
      setError(t("connectStatusError"));
    };

    socket.onclose = () => {
      setStatus((prev) => (prev === "connected" ? "disconnected" : prev));
    };
  };

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    if (terminalRef.current) return;

    const terminal = new Terminal({
      cols: DEFAULT_COLS,
      rows: DEFAULT_ROWS,
      convertEol: true,
      cursorBlink: true,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      theme: { background: "#08111e", foreground: "#dbeafe", cursor: "#67e8f9", selectionBackground: "#0e7490" },
      screenReaderMode: true,
    });
    terminal.open(container);
    terminalRef.current = terminal;

    terminal.onData((value) => {
      const socket = socketRef.current;
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "input", data: value }));
      }
    });

    terminal.onResize(({ cols, rows }) => {
      setDims({ cols, rows });
      const socket = socketRef.current;
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "resize", cols, rows }));
      }
    });

    const resizeObserver = new ResizeObserver(() => {
      fitTerminal(terminal);
    });
    resizeObserver.observe(container);

    connect();

    return () => {
      if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
      resizeObserver.disconnect();
      socketRef.current?.close();
      socketRef.current = null;
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [target.id]);

  const fitTerminal = (terminal: Terminal) => {
    if (!containerRef.current) return;
    const padding = 20;
    const width = containerRef.current.clientWidth - padding;
    const height = containerRef.current.clientHeight - padding;
    if (width <= 0 || height <= 0) return;

    const dims = estimateTerminalDimensions(width, height, terminal.options.fontSize || 13);
    if (dims.cols >= 20 && dims.rows >= 8 && (dims.cols !== terminal.cols || dims.rows !== terminal.rows)) {
      terminal.resize(dims.cols, dims.rows);
    }
  };

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "F11") {
        event.preventDefault();
        setIsFullscreen((prev) => !prev);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    const terminal = terminalRef.current;
    if (!terminal) return;
    if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
    fitRetryRef.current = window.setTimeout(() => fitTerminal(terminal), 120);
  }, [isFullscreen]);

  const statusBadgeClass = status === "connected" ? "success" : status === "error" || status === "disconnected" ? "danger" : "info";

  return (
    <section className={`terminal-panel ${isFullscreen ? "fullscreen" : ""}`}>
      <header className="terminal-header">
        <div className="terminal-status">
          <span className={`badge ${statusBadgeClass}`}>{t(`connectStatus${status.charAt(0).toUpperCase()}${status.slice(1)}` as never)}</span>
          <span className="terminal-dims">{dims.cols}x{dims.rows}</span>
        </div>
        <div className="terminal-controls">
          {status === "disconnected" || status === "error" ? (
            <button type="button" onClick={connect}><RefreshCw />{t("connectReconnect")}</button>
          ) : (
            <button type="button" onClick={() => socketRef.current?.close()} disabled={status !== "connected"}><Unplug />{t("connectDisconnect")}</button>
          )}
          <button type="button" className="icon-button" onClick={() => setIsFullscreen((prev) => !prev)} aria-label={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")} title={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")}>
            {isFullscreen ? <Minimize /> : <Maximize />}
          </button>
        </div>
      </header>
      {error && <div className="terminal-error">{error}</div>}
      <div className="terminal-viewport" ref={containerRef} />
    </section>
  );
}

function estimateTerminalDimensions(width: number, height: number, fontSize: number) {
  const charWidth = Math.ceil(fontSize * 0.601);
  const charHeight = Math.ceil(fontSize * 1.23);
  return {
    cols: Math.max(20, Math.floor(width / charWidth)),
    rows: Math.max(8, Math.floor(height / charHeight)),
  };
}

function snapshotToSample(snapshot: TargetSystemSnapshot): MetricSample {
  const network = sumNetwork(snapshot);
  return {
    at: snapshot.collected_at ? new Date(snapshot.collected_at).getTime() || Date.now() : Date.now(),
    cpu: clampNumber(snapshot.cpu_percent),
    memory: clampNumber(snapshot.memory?.percent || 0),
    swap: clampNumber(snapshot.swap?.percent || 0),
    rx: network.rx,
    tx: network.tx,
  };
}

function sumNetwork(snapshot: TargetSystemSnapshot) {
  return (snapshot.network || []).reduce((total, item) => ({
    rx: total.rx + Math.max(0, item.rx_bytes || 0),
    tx: total.tx + Math.max(0, item.tx_bytes || 0),
  }), { rx: 0, tx: 0 });
}

function buildNetworkRates(samples: MetricSample[]) {
  const rx: number[] = [];
  const tx: number[] = [];
  for (let index = 1; index < samples.length; index += 1) {
    const previous = samples[index - 1];
    const current = samples[index];
    const seconds = Math.max(1, (current.at - previous.at) / 1000);
    rx.push(Math.max(0, current.rx - previous.rx) / seconds);
    tx.push(Math.max(0, current.tx - previous.tx) / seconds);
  }
  return { rx, tx };
}

function sparklinePoints(values: number[], fixedMax?: number) {
  if (!values.length) return "0,28 100,28";
  const items = values.length === 1 ? [values[0], values[0]] : values.slice(-MAX_SYSTEM_SAMPLES);
  const max = Math.max(fixedMax || 0, ...items, 1);
  return items.map((value, index) => {
    const x = items.length === 1 ? 100 : (index / (items.length - 1)) * 100;
    const y = 28 - (clampNumber((value / max) * 100) / 100) * 26;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  if (value < 1024) return `${value.toFixed(0)} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function clampNumber(value: number) {
  if (!Number.isFinite(value) || value < 0) return 0;
  if (value > 100) return 100;
  return value;
}
