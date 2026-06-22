import { Terminal } from "@xterm/xterm";
import { ArrowLeft, Maximize, RefreshCw, Unplug } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { ConsoleData, Target } from "../types";
import { targetEndpoint } from "../utils";
import { FileManager } from "./FileManager";

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error";

const DEFAULT_COLS = 120;
const DEFAULT_ROWS = 32;

export function ConnectPage({ data }: { data: ConsoleData }) {
  const { targetID } = useParams<{ targetID: string }>();
  const { t } = useI18n();
  const target = data.targets.find((item) => item.id === targetID);

  if (!target) {
    return (
      <section className="connect-page">
        <section className="resource-head">
          <div>
            <small>{t("services")}</small>
            <h2>{t("connect")}</h2>
            <p>{t("serviceEmptyBody")}</p>
          </div>
          <Link className="button-link" to="/targets">
            <ArrowLeft />{t("connectBack")}
          </Link>
        </section>
        <div className="status error">{t("serviceEmptyTitle")}</div>
      </section>
    );
  }

  return <ConnectWorkspace target={target} />;
}

function ConnectWorkspace({ target }: { target: Target }) {
  const { t } = useI18n();

  return (
    <section className="connect-page">
      <section className="resource-head">
        <div>
          <small>{t("services")} / {target.name}</small>
          <h2>{t("connectTerminal")}</h2>
          <p>{targetEndpoint(target)}</p>
        </div>
        <div className="top-actions">
          <Link className="button-link" to="/targets">
            <ArrowLeft />{t("connectBack")}
          </Link>
        </div>
      </section>
      <div className="connect-layout">
        <HostSummary target={target} />
        <div className="connect-main">
          <TerminalPanel target={target} />
          <FileManager target={target} />
        </div>
      </div>
    </section>
  );
}

function HostSummary({ target }: { target: Target }) {
  const { t } = useI18n();
  return (
    <aside className="connect-sidebar">
      <section className="section-block">
        <h3>{t("connectHostSummary")}</h3>
        <div className="detail-list compact">
          <div><dt>{t("serviceName")}</dt><dd>{target.name}</dd></div>
          <div><dt>{t("serviceAlias")}</dt><dd>{target.alias}</dd></div>
          <div><dt>{t("targetHost")}</dt><dd>{target.host || "-"}</dd></div>
          <div><dt>{t("targetPort")}</dt><dd>{target.port || 22}</dd></div>
          <div><dt>{t("serviceRemoteUser")}</dt><dd>{target.remote_username}</dd></div>
          <div><dt>{t("serviceAuthType")}</dt><dd>{target.auth_type === "private_key" ? t("serviceAuthPrivateKey") : t("serviceAuthPassword")}</dd></div>
          <div><dt>{t("commonTag")}</dt><dd>{(target.tags || []).join(", ") || "-"}</dd></div>
        </div>
      </section>
      <section className="section-block">
        <h3>{t("dashboardControlTitle")}</h3>
        <div className="ops-grid">
          <span><strong>CPU</strong><small>{t("connectResourcePlaceholder")}</small></span>
          <span><strong>Memory</strong><small>{t("connectResourcePlaceholder")}</small></span>
          <span><strong>Load</strong><small>{t("connectResourcePlaceholder")}</small></span>
        </div>
      </section>
    </aside>
  );
}

function TerminalPanel({ target }: { target: Target }) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const fitRetryRef = useRef<number | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [error, setError] = useState("");
  const [dims, setDims] = useState({ cols: DEFAULT_COLS, rows: DEFAULT_ROWS });
  const [isFullscreen, setIsFullscreen] = useState(false);

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
          <button type="button" onClick={() => setIsFullscreen((prev) => !prev)} aria-label={t("connectTerminalSize")}>
            <Maximize />
          </button>
        </div>
      </header>
      {error && <div className="status error">{error}</div>}
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
