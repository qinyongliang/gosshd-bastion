import { Terminal } from "@xterm/xterm";
import { Maximize, Minimize, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { api } from "../api";
import { useI18n } from "../i18n";

const DEFAULT_COLS = 120;
const DEFAULT_ROWS = 32;

type LocalStatus = "connecting" | "connected" | "disconnected" | "error";

export function LocalTerminalPage() {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const heartbeatRef = useRef<number | null>(null);
  const fitRetryRef = useRef<number | null>(null);
  const [status, setStatus] = useState<LocalStatus>("connecting");
  const [fullscreen, setFullscreen] = useState(false);
  const [error, setError] = useState("");

  const connect = () => {
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
    setStatus("connecting");
    setError("");
    const terminal = terminalRef.current;
    if (!terminal) return;
    terminal.reset();
    fitTerminal(terminal, containerRef.current);

    const socket = new WebSocket(api.localTerminalURL(terminal.cols || DEFAULT_COLS, terminal.rows || DEFAULT_ROWS));
    socketRef.current = socket;

    socket.onopen = () => {
      setStatus("connected");
      terminal.focus();
      socket.send(JSON.stringify({ type: "resize", cols: terminal.cols || DEFAULT_COLS, rows: terminal.rows || DEFAULT_ROWS }));
      if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
      heartbeatRef.current = window.setInterval(() => {
        if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "heartbeat" }));
      }, 10_000);
    };

    socket.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as { type: string; data?: string; code?: number };
        if (message.type === "output" && message.data !== undefined) {
          terminal.write(message.data);
        } else if (message.type === "error" && message.data !== undefined) {
          terminal.write(`\r\n\x1b[1;31m${message.data}\x1b[0m\r\n`);
          setError(message.data);
          setStatus("error");
        } else if (message.type === "exit") {
          terminal.write(`\r\n\x1b[2;37m${t("localTerminalEnded")} ${message.code ?? "-"}\x1b[0m\r\n`);
          setStatus("disconnected");
        }
      } catch {
        terminal.write(event.data);
      }
    };

    socket.onerror = () => {
      setError(t("localTerminalError"));
      setStatus("error");
    };

    socket.onclose = () => {
      if (heartbeatRef.current) {
        window.clearInterval(heartbeatRef.current);
        heartbeatRef.current = null;
      }
      setStatus((current) => (current === "connected" ? "disconnected" : current));
    };
  };

  useEffect(() => {
    const container = containerRef.current;
    if (!container || terminalRef.current) return;

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
    terminal.focus();
    container.addEventListener("pointerdown", focusTerminal);

    terminal.onData((value) => {
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "input", data: value }));
    });

    terminal.onResize(({ cols, rows }) => {
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", cols, rows }));
    });

    const resizeObserver = new ResizeObserver(() => fitTerminal(terminal, container));
    resizeObserver.observe(container);
    fitTerminal(terminal, container);
    connect();
    const focusTerminalOnWindowFocus = () => terminal.focus();

    const closeTerminal = () => {
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "close" }));
    };
    window.addEventListener("beforeunload", closeTerminal);
    window.addEventListener("focus", focusTerminalOnWindowFocus);

    return () => {
      if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
      if (heartbeatRef.current) window.clearInterval(heartbeatRef.current);
      closeTerminal();
      window.removeEventListener("beforeunload", closeTerminal);
      window.removeEventListener("focus", focusTerminalOnWindowFocus);
      resizeObserver.disconnect();
      container.removeEventListener("pointerdown", focusTerminal);
      socketRef.current?.close();
      socketRef.current = null;
      terminalRef.current = null;
      terminal.dispose();
    };
  }, []);

  useEffect(() => {
    const terminal = terminalRef.current;
    const container = containerRef.current;
    if (!terminal || !container) return;
    if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
    fitRetryRef.current = window.setTimeout(() => fitTerminal(terminal, container), 120);
  }, [fullscreen]);

  return (
    <main className={`local-terminal-workspace ${fullscreen ? "fullscreen" : ""}`}>
      <section className={`terminal-panel local-terminal-panel ${fullscreen ? "fullscreen" : ""}`}>
        <header className="local-terminal-toolbar">
          <strong>{t("localTerminalTitle")}</strong>
          <span className={`terminal-status ${status}`}>{t(`localTerminalStatus${status[0].toUpperCase()}${status.slice(1)}`)}</span>
          {error && <button type="button" onClick={connect}><RefreshCw />{t("connectReconnect")}</button>}
          <button type="button" className="icon-button" onClick={() => setFullscreen((value) => !value)} title={fullscreen ? t("connectExitFullscreen") : t("connectFullscreen")}>
            {fullscreen ? <Minimize /> : <Maximize />}
          </button>
        </header>
        <div className="terminal-viewport" ref={containerRef} tabIndex={0} />
      </section>
    </main>
  );
}

function fitTerminal(terminal: Terminal, container: HTMLElement | null) {
  if (!container) return;
  const width = container.clientWidth;
  const height = container.clientHeight;
  if (width <= 0 || height <= 0) return;
  const dims = estimateTerminalDimensions(width, height, terminal.options.fontSize || 13);
  if (dims.cols >= 20 && dims.rows >= 8 && (dims.cols !== terminal.cols || dims.rows !== terminal.rows)) {
    terminal.resize(dims.cols, dims.rows);
  }
}

function estimateTerminalDimensions(width: number, height: number, fontSize: number) {
  const cellWidth = Math.max(7, fontSize * 0.62);
  const cellHeight = Math.max(15, fontSize * 1.35);
  return {
    cols: Math.max(20, Math.floor((width - 18) / cellWidth)),
    rows: Math.max(8, Math.floor((height - 18) / cellHeight)),
  };
}
