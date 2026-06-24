import { useQuery } from "@tanstack/react-query";
import { Terminal } from "@xterm/xterm";
import { Activity, ArrowLeft, ChevronLeft, ChevronRight, Cpu, Globe, GripVertical, HardDrive, Maximize, Minimize, Monitor, Network, RefreshCw, Search, Server, X } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { ManualReviewPoller } from "../components/ManualReviewPoller";
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
  network: Record<string, { rx: number; tx: number }>;
};

const DEFAULT_COLS = 120;
const DEFAULT_ROWS = 32;
const SYSTEM_REFRESH_MS = 5000;
const MAX_SYSTEM_SAMPLES = 60;
type TerminalPanelProps = {
  data: ConsoleData;
  target: Target;
  active?: boolean;
  isFullscreen: boolean;
  onFullscreenChange: (value: boolean | ((previous: boolean) => boolean)) => void;
  manualReview?: boolean;
};

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

  return <ConnectWorkspace data={data} target={target} targets={data.targets} />;
}

export function ConnectWorkspace({ data, target, targets }: { data: ConsoleData; target: Target; targets: Target[] }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const navigate = useNavigate();
  const terminalFocusedByDefault = shouldFocusTerminalByDefault();
  const [hostOpen, setHostOpen] = useState(() => !terminalFocusedByDefault);
  const [filesOpen, setFilesOpen] = useState(() => !terminalFocusedByDefault);
  const [terminalFullscreen, setTerminalFullscreen] = useState(false);
  const [hostWidth, setHostWidth] = useState(248);
  const [filesWidth, setFilesWidth] = useState(330);
  const [tabs, setTabs] = useState<string[]>(() => [target.id]);
  const [activeTargetID, setActiveTargetID] = useState(target.id);
  const [tabMenu, setTabMenu] = useState<{ targetID: string; x: number; y: number } | null>(null);
  const bodyRef = useRef<HTMLElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const activeTarget = targets.find((item) => item.id === activeTargetID) || target;
  const openTabs = tabs.map((id) => targets.find((item) => item.id === id)).filter((item): item is Target => Boolean(item));
  const endpoint = targetEndpoint(activeTarget);

  useEffect(() => {
    setActiveTargetID(target.id);
    setTabs((current) => current.includes(target.id) ? current : [...current, target.id]);
  }, [target.id]);

  useEffect(() => {
    document.title = `${serverTitle(activeTarget)} · gosshd Bastion`;
  }, [activeTarget]);

  useEffect(() => {
    const onPointerDown = () => setTabMenu(null);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setTabMenu(null);
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  const activateTarget = (nextTargetID: string) => {
    setTabs((current) => current.includes(nextTargetID) ? current : [...current, nextTargetID]);
    setActiveTargetID(nextTargetID);
    navigate(`/targets/${nextTargetID}/connect`, { replace: true });
  };

  const closeTabs = (mode: "one" | "left" | "right" | "others" | "all", targetID: string) => {
    setTabMenu(null);
    setTabs((current) => {
      const index = current.indexOf(targetID);
      if (index < 0) return current;
      let next = current;
      if (mode === "one") next = current.filter((id) => id !== targetID);
      if (mode === "left") next = current.slice(index);
      if (mode === "right") next = current.slice(0, index + 1);
      if (mode === "others") next = [targetID];
      if (mode === "all") next = [];

      if (!next.length) {
        window.setTimeout(() => navigate("/targets", { replace: true }), 0);
        return next;
      }
      if (!next.includes(activeTargetID)) {
        const fallback = next[Math.min(index, next.length - 1)];
        setActiveTargetID(fallback);
        window.setTimeout(() => navigate(`/targets/${fallback}/connect`, { replace: true }), 0);
      }
      return next;
    });
  };

  const startResize = (area: "host" | "files", event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    const bodyRect = bodyRef.current?.getBoundingClientRect();
    const mainRect = mainRef.current?.getBoundingClientRect();
    if (!bodyRect || !mainRect) return;

    const onPointerMove = (moveEvent: PointerEvent) => {
      if (area === "host") {
        setHostWidth(clampNumber(moveEvent.clientX - bodyRect.left, 190, Math.min(360, bodyRect.width * 0.28)));
      } else {
        setFilesWidth(clampNumber(mainRect.right - moveEvent.clientX, 260, Math.min(480, mainRect.width * 0.38)));
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

        <ServerSwitcher targets={targets} currentTargetID={activeTarget.id} onOpenTarget={activateTarget} />

        <div className="connect-appbar-host">
          <Server />
          <div>
            <strong>{activeTarget.name}</strong>
            <code>{activeTarget.alias}</code>
          </div>
        </div>

        <div className="connect-appbar-meta">
          <span className="connect-appbar-endpoint"><Globe />{endpoint}</span>
          <span className="connect-appbar-type">
            {activeTarget.target_type === "agent" ? <Monitor /> : <HardDrive />}
            {activeTarget.target_type === "agent" ? t("privateNode") : t("serviceDirect")}
          </span>
        </div>

        <div className="connect-appbar-actions">
          <Segmented value={locale} items={[["en", "EN"], ["zh-CN", t("languageChinese")]]} onChange={(value) => setLocale(value as "en" | "zh-CN")} />
          <Segmented value={theme} items={[["dark", t("themeDark")], ["light", t("themeLight")]]} onChange={(value) => setTheme(value as "light" | "dark")} />
        </div>
      </header>
      <ConnectionTabs
        tabs={openTabs}
        activeTargetID={activeTarget.id}
        menu={tabMenu}
        onActivate={activateTarget}
        onClose={(targetID) => closeTabs("one", targetID)}
        onMenu={(targetID, point) => setTabMenu({ targetID, ...point })}
        onMenuAction={closeTabs}
      />

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
                  <div><dt>{t("serviceName")}</dt><dd>{activeTarget.name}</dd></div>
                  <div><dt>{t("serviceAlias")}</dt><dd><code>{activeTarget.alias}</code></dd></div>
                  <div><dt>{t("targetHost")}</dt><dd>{activeTarget.host || "-"}</dd></div>
                  <div><dt>{t("targetPort")}</dt><dd>{activeTarget.port || 22}</dd></div>
                  <div><dt>{t("serviceRemoteUser")}</dt><dd>{activeTarget.remote_username}</dd></div>
                  <div><dt>{t("commonTag")}</dt><dd>{(activeTarget.tags || []).join(", ") || "-"}</dd></div>
                </dl>
              </section>
              <SystemSnapshotPanel targetID={activeTarget.id} />
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
            <div className="terminal-tab-stack">
              {openTabs.map((tabTarget) => (
                <TerminalPanel
                  key={tabTarget.id}
                  data={data}
                  target={tabTarget}
                  active={tabTarget.id === activeTarget.id}
                  isFullscreen={terminalFullscreen && tabTarget.id === activeTarget.id}
                  onFullscreenChange={setTerminalFullscreen}
                />
              ))}
            </div>
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
            <FileManager target={activeTarget} nativeOpen={Boolean(data.runtime.client_mode)} />
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

function ConnectionTabs({
  tabs,
  activeTargetID,
  menu,
  onActivate,
  onClose,
  onMenu,
  onMenuAction,
}: {
  tabs: Target[];
  activeTargetID: string;
  menu: { targetID: string; x: number; y: number } | null;
  onActivate: (targetID: string) => void;
  onClose: (targetID: string) => void;
  onMenu: (targetID: string, point: { x: number; y: number }) => void;
  onMenuAction: (mode: "one" | "left" | "right" | "others" | "all", targetID: string) => void;
}) {
  const tabsRef = useRef<HTMLElement>(null);
  if (!tabs.length) return null;
  const menuTarget = menu ? tabs.find((item) => item.id === menu.targetID) : null;
  return (
    <section ref={tabsRef} className="connection-tabs" aria-label="Connection tabs">
      <div className="connection-tabs-scroll">
        {tabs.map((target) => (
          <div
            key={target.id}
            className={`connection-tab ${target.id === activeTargetID ? "active" : ""}`}
            onContextMenu={(event) => {
              event.preventDefault();
              onMenu(target.id, contextMenuPointInTabs(event.clientX, event.clientY, tabsRef.current));
            }}
            title={serverTitle(target)}
          >
            <button type="button" className="connection-tab-main" onClick={() => onActivate(target.id)}>
              {target.target_type === "agent" ? <Server /> : <HardDrive />}
              <span>
                <strong>{target.name}</strong>
                <code>{target.alias}</code>
              </span>
            </button>
            <button type="button" className="connection-tab-close" aria-label="Close tab" onClick={() => onClose(target.id)}>
              <X />
            </button>
          </div>
        ))}
      </div>
      {menu && menuTarget && (
        <div
          className="connection-tab-menu"
          style={{ left: menu.x, top: menu.y } as CSSProperties}
          onPointerDown={(event) => event.stopPropagation()}
          role="menu"
        >
          <button type="button" role="menuitem" onClick={() => onMenuAction("one", menuTarget.id)}>关闭当前</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("left", menuTarget.id)}>关闭左侧</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("right", menuTarget.id)}>关闭右侧</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("others", menuTarget.id)}>关闭其他</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("all", menuTarget.id)}>关闭全部</button>
        </div>
      )}
    </section>
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
  const interfaceRates = buildInterfaceNetworkRates(samples);

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
          {system.error && <span>{String((system.error as Error).message || "")}</span>}
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
                  <strong title={networkInterfaceName(item.interface)}>{networkInterfaceName(item.interface)}</strong>
                  <span><b>↓</b>{formatBytesPerSecond(interfaceRates[item.interface]?.rx)}</span>
                  <span><b>↑</b>{formatBytesPerSecond(interfaceRates[item.interface]?.tx)}</span>
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

export function TerminalPanel({ data, target, active = true, isFullscreen, onFullscreenChange, manualReview = true }: TerminalPanelProps) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const heartbeatRef = useRef<number | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [error, setError] = useState("");
  const [dims, setDims] = useState({ cols: DEFAULT_COLS, rows: DEFAULT_ROWS });
  const [sessionID, setSessionID] = useState("");
  const fitRetryRef = useRef<number | null>(null);
  const fitFrameRef = useRef<number | null>(null);
  const activeRef = useRef(active);
  const statusRef = useRef<ConnectionStatus>("connecting");

  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  const updateStatus = (next: ConnectionStatus) => {
    statusRef.current = next;
    setStatus(next);
  };

  const connect = () => {
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
    updateStatus("connecting");
    setError("");
    setSessionID("");
    const terminal = terminalRef.current;
    if (!terminal) return;
    fitTerminal(terminal);

    const cols = terminal.cols || DEFAULT_COLS;
    const rows = terminal.rows || DEFAULT_ROWS;
    const url = api.targetTerminalURL(target.id, cols, rows);
    const socket = new WebSocket(url);
    socketRef.current = socket;
    const isCurrentSocket = () => socketRef.current === socket;

    socket.onopen = () => {
      if (!isCurrentSocket()) return;
      updateStatus("connected");
      terminal.focus();
      const currentCols = terminal.cols || cols;
      const currentRows = terminal.rows || rows;
      setDims({ cols: currentCols, rows: currentRows });
      socket.send(JSON.stringify({ type: "resize", cols: currentCols, rows: currentRows }));
      if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
      heartbeatRef.current = window.setInterval(() => {
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "heartbeat" }));
        }
      }, 10_000);
    };

    socket.onmessage = (event) => {
      if (!isCurrentSocket()) return;
      try {
        const message = JSON.parse(event.data) as { type: string; data?: string; code?: number; cols?: number; rows?: number; session_id?: string };
        if (message.type === "output" && message.data !== undefined) {
          terminal.write(message.data);
        } else if (message.type === "error" && message.data !== undefined) {
          terminal.write(`\r\n\x1b[1;31m${message.data}\x1b[0m\r\n`);
          updateStatus("error");
          setError(message.data);
        } else if (message.type === "exit") {
          terminal.write(`\r\n\x1b[2;37mSession ended (exit ${message.code ?? "-"})\x1b[0m\r\n`);
          updateStatus("disconnected");
          setSessionID("");
          socket.close();
        } else if (message.type === "session" && message.session_id) {
          setSessionID(message.session_id);
        }
      } catch {
        terminal.write(event.data);
      }
    };

    socket.onerror = () => {
      if (!isCurrentSocket()) return;
      updateStatus("error");
      setError(t("connectStatusError"));
    };

    socket.onclose = () => {
      if (!isCurrentSocket()) return;
      if (heartbeatRef.current) {
        window.clearInterval(heartbeatRef.current);
        heartbeatRef.current = null;
      }
      updateStatus(statusRef.current === "connected" ? "disconnected" : statusRef.current);
      setSessionID("");
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
    const focusTerminal = () => terminal.focus();
    if (activeRef.current) terminal.focus();
    container.addEventListener("pointerdown", focusTerminal);

    terminal.onData((value) => {
      const socket = socketRef.current;
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "input", data: value }));
      } else if ((value === "\r" || value === "\n") && (statusRef.current === "disconnected" || statusRef.current === "error")) {
        connect();
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
      scheduleTerminalFit(terminal);
    });
    resizeObserver.observe(container);
    scheduleTerminalFit(terminal);

    connect();

    const closeTerminalSession = () => {
      const socket = socketRef.current;
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "close" }));
      }
    };
    const focusTerminalOnWindowFocus = () => {
      if (activeRef.current) terminal.focus();
    };
    window.addEventListener("beforeunload", closeTerminalSession);
    window.addEventListener("focus", focusTerminalOnWindowFocus);

    return () => {
      if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
      if (fitFrameRef.current) window.cancelAnimationFrame(fitFrameRef.current);
      if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
      closeTerminalSession();
      window.removeEventListener("beforeunload", closeTerminalSession);
      window.removeEventListener("focus", focusTerminalOnWindowFocus);
      resizeObserver.disconnect();
      container.removeEventListener("pointerdown", focusTerminal);
      socketRef.current?.close();
      socketRef.current = null;
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [target.id]);

  const scheduleTerminalFit = (terminal: Terminal) => {
    if (fitFrameRef.current) window.cancelAnimationFrame(fitFrameRef.current);
    fitFrameRef.current = window.requestAnimationFrame(() => {
      fitFrameRef.current = null;
      fitTerminal(terminal);
    });
  };

  const fitTerminal = (terminal: Terminal) => {
    if (!containerRef.current) return;
    const container = containerRef.current;
    const width = container.clientWidth;
    const height = container.clientHeight;
    if (width <= 0 || height <= 0) return;

    if (terminal.element) {
      terminal.element.style.width = `${width}px`;
      terminal.element.style.height = `${height}px`;
      const screen = terminal.element.querySelector<HTMLElement>(".xterm-screen");
      const viewport = terminal.element.querySelector<HTMLElement>(".xterm-viewport");
      const rows = terminal.element.querySelector<HTMLElement>(".xterm-rows");
      if (screen) screen.style.height = `${height}px`;
      if (viewport) viewport.style.height = `${height}px`;
      if (rows) rows.style.height = `${height}px`;
    }
    const dims = estimateTerminalDimensions(container, width, height, terminal);
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
    scheduleTerminalFit(terminal);
    fitRetryRef.current = window.setTimeout(() => scheduleTerminalFit(terminal), 120);
    if (active) terminal.focus();
  }, [active, isFullscreen, target.id]);

  return (
    <section className={`terminal-panel ${active ? "active" : "inactive"} ${isFullscreen ? "fullscreen" : ""}`} aria-hidden={!active}>
      {manualReview && sessionID && <ManualReviewPoller data={data} sessionID={sessionID} />}
      <button type="button" className="terminal-fullscreen-button icon-button" onClick={() => onFullscreenChange((prev) => !prev)} aria-label={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")} title={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")}>
        {isFullscreen ? <Minimize /> : <Maximize />}
      </button>
      {(status === "disconnected" || status === "error") && <button type="button" className="terminal-reconnect-button" onClick={connect}><RefreshCw />{t("connectReconnect")}</button>}
      <div className="terminal-viewport" ref={containerRef} />
    </section>
  );
}

function ServerSwitcher({ targets, currentTargetID, onOpenTarget }: { targets: Target[]; currentTargetID: string; onOpenTarget: (targetID: string) => void }) {
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
    onOpenTarget(target.id);
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

function contextMenuPointInTabs(clientX: number, clientY: number, container: HTMLElement | null) {
  const menuWidth = 150;
  const menuHeight = 166;
  const margin = 4;
  if (!container) return { x: clientX, y: clientY };
  const rect = container.getBoundingClientRect();
  return {
    x: clampNumber(clientX - rect.left - 2, margin, Math.max(margin, rect.width - menuWidth - margin)),
    y: clampNumber(clientY - rect.top + 2, margin, Math.max(margin, rect.height - menuHeight - margin)),
  };
}

function estimateTerminalDimensions(container: HTMLElement, width: number, height: number, terminal: Terminal) {
  const { charWidth, charHeight } = measureTerminalCell(container, terminal);
  return {
    cols: Math.max(20, Math.floor(width / charWidth)),
    rows: Math.max(8, Math.floor(height / charHeight)),
  };
}

function measureTerminalCell(container: HTMLElement, terminal: Terminal) {
  const fontSize = terminal.options.fontSize || 13;
  const row = terminal.element?.querySelector<HTMLElement>(".xterm-rows > div");
  const rowRect = row?.getBoundingClientRect();
  const helper = terminal.element?.querySelector<HTMLElement>(".xterm-helper-textarea");
  const helperStyle = helper ? window.getComputedStyle(helper) : null;
  const helperLineHeight = helperStyle ? Number.parseFloat(helperStyle.lineHeight) : 0;
  const probe = document.createElement("span");
  probe.textContent = "W".repeat(40);
  probe.style.position = "absolute";
  probe.style.visibility = "hidden";
  probe.style.pointerEvents = "none";
  probe.style.whiteSpace = "pre";
  probe.style.fontFamily = '"SFMono-Regular", Consolas, "Liberation Mono", monospace';
  probe.style.fontSize = `${fontSize}px`;
  probe.style.lineHeight = String(terminal.options.lineHeight || 1);
  container.appendChild(probe);
  const rect = probe.getBoundingClientRect();
  probe.remove();
  return {
    charWidth: Math.max(1, rect.width / 40),
    charHeight: Math.max(1, rowRect?.height || helperLineHeight || fontSize * Number(terminal.options.lineHeight || 1)),
  };
}

function shouldFocusTerminalByDefault() {
  if (typeof window === "undefined") return false;
  return window.innerWidth < 1680;
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
    network: Object.fromEntries((snapshot.network || []).map((item) => [
      item.interface,
      { rx: Math.max(0, item.rx_bytes || 0), tx: Math.max(0, item.tx_bytes || 0) },
    ])),
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

function buildInterfaceNetworkRates(samples: MetricSample[]) {
  if (samples.length < 2) return {};
  const previous = samples[samples.length - 2];
  const current = samples[samples.length - 1];
  const seconds = Math.max(1, (current.at - previous.at) / 1000);
  const rates: Record<string, { rx: number; tx: number }> = {};
  for (const [name, currentCounters] of Object.entries(current.network)) {
    const previousCounters = previous.network[name];
    if (!previousCounters) continue;
    rates[name] = {
      rx: Math.max(0, currentCounters.rx - previousCounters.rx) / seconds,
      tx: Math.max(0, currentCounters.tx - previousCounters.tx) / seconds,
    };
  }
  return rates;
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

function formatBytesPerSecond(value?: number) {
  if (value === undefined) return "-";
  return `${formatBytes(value)}/s`;
}

function networkInterfaceName(value: string) {
  if (!value || /^\d+$/.test(value)) return "网卡";
  return value;
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
