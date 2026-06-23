import { useQuery } from "@tanstack/react-query";
import { Terminal } from "@xterm/xterm";
import { Activity, ArrowLeft, ChevronLeft, ChevronRight, Cpu, Globe, GripVertical, HardDrive, Maximize, Minimize, Monitor, Network, RefreshCw, Search, Server } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api";
import { Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { useTheme } from "../theme";
import type { ConsoleData, Target, TargetSystemSnapshot, TargetSystemUsage } from "../types";
import { tagColor, targetEndpoint } from "../utils";
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

  return <ConnectWorkspace target={target} targets={data.targets} />;
}

function ConnectWorkspace({ target, targets }: { target: Target; targets: Target[] }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const [hostOpen, setHostOpen] = useState(true);
  const [filesOpen, setFilesOpen] = useState(true);
  const [terminalFullscreen, setTerminalFullscreen] = useState(false);
  const [hostWidth, setHostWidth] = useState(330);
  const [filesWidth, setFilesWidth] = useState(480);
  const bodyRef = useRef<HTMLElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const endpoint = targetEndpoint(target);

  useEffect(() => {
    document.title = `${serverTitle(target)} · gosshd Bastion`;
  }, [target]);

  const startResize = (area: "host" | "files", event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    const bodyRect = bodyRef.current?.getBoundingClientRect();
    const mainRect = mainRef.current?.getBoundingClientRect();
    if (!bodyRect || !mainRect) return;

    const onPointerMove = (moveEvent: PointerEvent) => {
      if (area === "host") {
        setHostWidth(clampNumber(moveEvent.clientX - bodyRect.left, 240, Math.min(520, bodyRect.width * 0.42)));
      } else {
        setFilesWidth(clampNumber(mainRect.right - moveEvent.clientX, 320, Math.min(720, mainRect.width * 0.58)));
      }
    };
    const onPointerUp = () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      document.body.classList.remove("is-resizing-connect");
    };

    document.body.classList.add("is-resizing-connect");
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
  };

  return (
    <main className={`connect-workspace ${terminalFullscreen ? "terminal-fullscreen-active" : ""}`}>
      <header className="connect-appbar">
        <div className="connect-appbar-brand">
          <div className="connect-appbar-mark">g</div>
          <div className="connect-appbar-title">
            <strong>gosshd</strong>
            <span>{t("shellProduct")}</span>
          </div>
        </div>

        <ServerSwitcher targets={targets} currentTargetID={target.id} />

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
        </div>

        <div className="connect-appbar-actions">
          <Segmented value={locale} items={[["en", "EN"], ["zh-CN", t("languageChinese")]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          <Segmented value={theme} items={[["dark", t("themeDark")], ["light", t("themeLight")]]} onChange={(value) => setTheme(value as "light" | "dark")} />
        </div>
      </header>

      <section
        ref={bodyRef}
        className={`connect-body ${hostOpen ? "" : "host-collapsed"}`}
        style={{ "--host-width": `${hostWidth}px` } as CSSProperties}
      >
        <aside className={`connect-host-panel ${hostOpen ? "" : "collapsed"}`}>
          {hostOpen ? (
            <>
              <section className="connect-panel compact">
                <header className="connect-panel-title">
                  <h3><Monitor />{t("connectHostInfo")}</h3>
                  <button type="button" className="icon-button" onClick={() => setHostOpen(false)} title={t("connectCollapseSidebar")}>
                    <ChevronLeft />
                  </button>
                </header>
                <dl className="connect-host-list">
                  <div><dt>{t("serviceName")}</dt><dd>{target.name}</dd></div>
                  <div><dt>{t("serviceAlias")}</dt><dd><code>{target.alias}</code></dd></div>
                  <div><dt>{t("targetHost")}</dt><dd>{target.host || "-"}</dd></div>
                  <div><dt>{t("targetPort")}</dt><dd>{target.port || 22}</dd></div>
                  <div><dt>{t("serviceRemoteUser")}</dt><dd>{target.remote_username}</dd></div>
                  <div><dt>{t("commonTag")}</dt><dd>{(target.tags || []).join(", ") || "-"}</dd></div>
                </dl>
              </section>
              <SystemSnapshotPanel targetID={target.id} />
            </>
          ) : (
            <button type="button" className="collapsed-zone-button" onClick={() => setHostOpen(true)} title={t("connectExpandSidebar")}>
              <ChevronRight />
              <span>{t("connectHostInfo")}</span>
            </button>
          )}
        </aside>
        {hostOpen && !terminalFullscreen && (
          <button type="button" className="connect-resizer host-resizer" onPointerDown={(event) => startResize("host", event)} aria-label={t("connectHostInfo")}>
            <GripVertical />
          </button>
        )}

        <section
          ref={mainRef}
          className={`connect-main ${filesOpen ? "" : "files-collapsed"}`}
          style={{ "--files-width": `${filesWidth}px` } as CSSProperties}
        >
          <div className="connect-zone terminal-zone">
            <TerminalPanel target={target} isFullscreen={terminalFullscreen} onFullscreenChange={setTerminalFullscreen} />
          </div>
          {filesOpen && !terminalFullscreen && (
            <button type="button" className="connect-resizer files-resizer" onPointerDown={(event) => startResize("files", event)} aria-label={t("connectFilesTitle")}>
              <GripVertical />
            </button>
          )}
          <div className={`connect-zone files-zone ${filesOpen ? "" : "collapsed"}`}>
            {filesOpen ? (
              <>
            <div className="connect-zone-head">
              <span><HardDrive />{t("connectFilesTitle")}</span>
              <button type="button" className="icon-button" onClick={() => setFilesOpen(false)} title={t("connectCollapseSidebar")}>
                <ChevronRight />
              </button>
            </div>
            <FileManager target={target} />
              </>
            ) : (
              <button type="button" className="collapsed-zone-button" onClick={() => setFilesOpen(true)} title={t("connectFilesTitle")}>
                <ChevronLeft />
                <span>{t("connectFilesTitle")}</span>
              </button>
            )}
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

function TerminalPanel({ target, isFullscreen, onFullscreenChange }: { target: Target; isFullscreen: boolean; onFullscreenChange: (value: boolean | ((previous: boolean) => boolean)) => void }) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const heartbeatRef = useRef<number | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [error, setError] = useState("");
  const [dims, setDims] = useState({ cols: DEFAULT_COLS, rows: DEFAULT_ROWS });
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
      if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
      heartbeatRef.current = window.setInterval(() => {
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "heartbeat" }));
        }
      }, 10_000);
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
      if (heartbeatRef.current) {
        window.clearInterval(heartbeatRef.current);
        heartbeatRef.current = null;
      }
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

    const closeTerminalSession = () => {
      const socket = socketRef.current;
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "close" }));
      }
    };
    window.addEventListener("beforeunload", closeTerminalSession);

    return () => {
      if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
      if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
      closeTerminalSession();
      window.removeEventListener("beforeunload", closeTerminalSession);
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
        onFullscreenChange((prev) => !prev);
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

  return (
    <section className={`terminal-panel ${isFullscreen ? "fullscreen" : ""}`}>
      <button type="button" className="terminal-fullscreen-button icon-button" onClick={() => onFullscreenChange((prev) => !prev)} aria-label={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")} title={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")}>
        {isFullscreen ? <Minimize /> : <Maximize />}
      </button>
      {error && <button type="button" className="terminal-reconnect-button" onClick={connect}><RefreshCw />{t("connectReconnect")}</button>}
      <div className="terminal-viewport" ref={containerRef} />
    </section>
  );
}

function ServerSwitcher({ targets, currentTargetID }: { targets: Target[]; currentTargetID: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const currentTarget = targets.find((item) => item.id === currentTargetID);
  const currentTitle = currentTarget ? serverTitle(currentTarget) : t("connectSwitchServer");
  const filteredTargets = useMemo(() => {
    const text = query.trim().toLowerCase();
    if (!text) return targets;
    return targets.filter((item) => [
      item.name,
      item.alias,
      targetEndpoint(item),
      item.remote_username,
      ...(item.tags || []),
    ].join(" ").toLowerCase().includes(text));
  }, [query, targets]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const openTarget = (target: Target) => {
    const features = "noopener,noreferrer,width=1440,height=900";
    window.open(`/targets/${target.id}/connect`, `connect-${target.id}`, features);
    setOpen(false);
    setQuery("");
  };

  return (
    <div className="server-switcher" ref={rootRef}>
      <button
        type="button"
        className={`icon-button connect-server-switcher ${open ? "active" : ""}`}
        onClick={() => setOpen((prev) => !prev)}
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label={t("connectSwitchServer")}
        title={currentTitle}
      >
        <Server />
      </button>
      {open && (
        <section className="server-switcher-menu" role="menu" aria-label={t("connectSwitchServer")}>
          <label className="server-switcher-search">
            <Search />
            <input
              autoFocus
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("connectSwitchSearchPlaceholder")}
            />
          </label>
          <div className="server-switcher-list">
            {filteredTargets.map((item) => (
              <button
                type="button"
                key={item.id}
                className={`server-switcher-item ${item.id === currentTargetID ? "active" : ""}`}
                onClick={() => openTarget(item)}
                role="menuitem"
                title={serverTitle(item)}
              >
                <span className="server-switcher-icon">{item.target_type === "agent" ? <Server /> : <HardDrive />}</span>
                <span className="server-switcher-main">
                  <strong>{item.name}</strong>
                  <code>{item.alias}</code>
                  <small>{targetEndpoint(item)}</small>
                  <span className="server-switcher-tags">
                    {(item.tags || []).map((tag) => (
                      <span key={tag} className={`tag-chip tag-color-${tagColor(tag, item.tag_colors)}`}>{tag}</span>
                    ))}
                  </span>
                </span>
              </button>
            ))}
            {!filteredTargets.length && <div className="server-switcher-empty">{t("serviceEmptyTitle")}</div>}
          </div>
        </section>
      )}
    </div>
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

function clampNumber(value: number, min = 0, max = 100) {
  if (!Number.isFinite(value)) return min;
  if (value < min) return min;
  if (value > max) return max;
  return value;
}

function serverTitle(target: Target) {
  const endpoint = targetEndpoint(target);
  const tags = (target.tags || []).join(", ");
  return [target.name, target.alias, endpoint, tags].filter(Boolean).join(" · ");
}
