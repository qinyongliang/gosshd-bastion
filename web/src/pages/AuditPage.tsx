import { useQuery } from "@tanstack/react-query";
import { Terminal } from "@xterm/xterm";
import { RefreshCw, Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { AuditTable, Empty, Modal, Panel } from "../components/ui";
import { useI18n } from "../i18n";
import { formatDate, formSubmit } from "../lib/forms";
import type { AuditLog, AuditRecording, ConsoleData } from "../types";

export function AuditPage({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const isClientMode = Boolean(data.runtime.client_mode);
  const [filters, setFilters] = useState({ query: "", decision: "", request_type: "", started_from: "", started_to: "", page: 1, page_size: 20 });
  const [replayID, setReplayID] = useState("");
  const audit = useQuery({
    queryKey: ["audit-page", data.activeOrg.id, isClientMode, filters],
    queryFn: () => api.audit(isClientMode ? filters : { ...filters, organization_id: data.activeOrg.id }),
  });
  const replay = useQuery({ queryKey: ["audit-recording", replayID], queryFn: () => api.auditRecording(replayID), enabled: Boolean(replayID) });
  const logs = audit.data?.logs || data.auditPage.logs;
  return (
    <div className="audit-page">
      <section className="resource-head">
        <div><small>{t("auditPageEyebrow")}</small><h2>{t("auditPageTitle")}</h2><p>{t("auditPageBody")}</p></div>
      </section>
      <form className="toolbar" onSubmit={(event) => formSubmit(event, (body) => setFilters({
        query: body.query || "",
        decision: body.decision || "",
        request_type: body.request_type || "",
        started_from: body.started_from || "",
        started_to: body.started_to || "",
        page: 1,
        page_size: 20,
      }))}>
        <Search />
        <input name="query" defaultValue={filters.query} placeholder={t(isClientMode ? "auditClientSearchPlaceholder" : "auditSearchPlaceholder")} />
        <select name="decision" defaultValue={filters.decision} aria-label={t("auditDecisionFilter")}>
          <option value="">{t("commonAll")}</option>
          <option value="allow">{t("commonAllow")}</option>
          <option value="deny">{t("commonDeny")}</option>
        </select>
        <select name="request_type" defaultValue={filters.request_type} aria-label={t("auditTypeFilter")}>
          <option value="">{t("commonAll")}</option>
          <option value="exec">{t("auditTypeExec")}</option>
          <option value="shell">{t("auditTypeInteractive")}</option>
        </select>
        <input name="started_from" type="datetime-local" defaultValue={filters.started_from} />
        <input name="started_to" type="datetime-local" defaultValue={filters.started_to} />
        <button type="submit">{t("search")}</button>
        <button type="button" onClick={() => void audit.refetch()} disabled={audit.isFetching}>
          <RefreshCw />
          {t("commonRefresh")}
        </button>
      </form>
      <Panel title={t("auditList")} subtitle="">
        {logs.length ? <AuditTable logs={logs} compact={isClientMode} onReplay={(log) => setReplayID(log.id)} /> : <Empty title={t("auditEmptyTitle")} body={t("auditEmptyBody")} />}
      </Panel>
      <div className="pager">
        <button type="button" disabled={filters.page <= 1} onClick={() => setFilters({ ...filters, page: filters.page - 1 })}>{t("commonPrevious")}</button>
        <span>{t("commonPage")} {audit.data?.page || 1}</span>
        <button type="button" disabled={(audit.data?.total || 0) <= filters.page * filters.page_size} onClick={() => setFilters({ ...filters, page: filters.page + 1 })}>{t("commonNext")}</button>
      </div>
      {replayID && <AuditReplayModal recording={replay.data} fallbackLog={logs.find((item) => item.id === replayID)} loading={replay.isLoading} onClose={() => setReplayID("")} />}
    </div>
  );
}

function AuditReplayModal({ recording, fallbackLog, loading, onClose }: { recording?: AuditRecording; fallbackLog?: AuditLog; loading: boolean; onClose: () => void }) {
  const { t } = useI18n();
  const log = recording?.log || fallbackLog;
  return <Modal title={t("auditReplayTitle")} onClose={onClose} wide>
    <div className="terminal-player">
      <div className="terminal-meta">
        <span><b>{t("auditTableTarget")}</b>{log?.target_name || log?.target_alias || "-"}</span>
        <span><b>{t("auditTableStarted")}</b>{formatDate(log?.started_at)}</span>
        <span><b>{t("auditReplayDuration")}</b>{durationText(log?.recording_duration_ms)}</span>
      </div>
      {loading && <div className="policy-empty-line">{t("loading")}</div>}
      {!loading && recording && <TerminalReplay lines={recording.lines} />}
      {!loading && !recording && <Empty title={t("auditReplayUnavailable")} body={t("auditReplayUnavailableBody")} />}
    </div>
  </Modal>;
}

function TerminalReplay({ lines }: { lines: unknown[] }) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const renderedIndexRef = useRef(0);
  const positionRef = useRef(0);
  const animationRef = useRef(0);
  const anchorRef = useRef({ clock: 0, position: 0 });
  const [speed, setSpeed] = useState(1);
  const [isPlaying, setIsPlaying] = useState(true);
  const [position, setPosition] = useState(0);
  const header = useMemo(() => parseHeader(lines), [lines]);
  const events = useMemo(() => parseEvents(lines), [lines]);
  const totalTime = useMemo(() => (events.length ? events[events.length - 1].time : 0), [events]);

  const clampPosition = useCallback((next: number) => Math.max(0, Math.min(totalTime, next)), [totalTime]);

  const writeToPosition = useCallback((next: number, force = false) => {
    const terminal = terminalRef.current;
    if (!terminal || !events.length) return;
    if (force) {
      terminal.reset();
      renderedIndexRef.current = 0;
    }
    let index = renderedIndexRef.current;
    while (index < events.length && events[index].time <= next + 0.001) {
      terminal.write(events[index].data);
      index += 1;
    }
    renderedIndexRef.current = index;
  }, [events]);

  const syncPosition = useCallback((next: number, forceWrite = false) => {
    const clamped = clampPosition(next);
    positionRef.current = clamped;
    setPosition(clamped);
    writeToPosition(clamped, forceWrite);
    return clamped;
  }, [clampPosition, writeToPosition]);

  const seekTo = useCallback((next: number) => {
    const clamped = syncPosition(next, true);
    anchorRef.current = { clock: performance.now(), position: clamped };
  }, [syncPosition]);

  useEffect(() => {
    if (animationRef.current) cancelAnimationFrame(animationRef.current);
    positionRef.current = 0;
    renderedIndexRef.current = 0;
    setPosition(0);
    setIsPlaying(Boolean(events.length));
    const container = containerRef.current;
    if (!container) return;
    container.innerHTML = "";
    const terminal = new Terminal({
      cols: header.width || 100,
      rows: Math.max(18, Math.min(header.height || 28, 42)),
      convertEol: true,
      cursorBlink: false,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      theme: { background: "#08111e", foreground: "#dbeafe", cursor: "#67e8f9" },
    });
    terminal.open(container);
    terminalRef.current = terminal;
    if (!events.length) {
      terminal.write(t("auditReplayEmpty"));
      return () => {
        terminalRef.current = null;
        terminal.dispose();
      };
    }
    writeToPosition(0, true);
    return () => {
      if (animationRef.current) cancelAnimationFrame(animationRef.current);
      animationRef.current = 0;
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [events, header.height, header.width, t, writeToPosition]);

  useEffect(() => {
    if (animationRef.current) cancelAnimationFrame(animationRef.current);
    animationRef.current = 0;
    if (!isPlaying || !events.length || !totalTime) return;
    anchorRef.current = { clock: performance.now(), position: positionRef.current };
    const tick = (now: number) => {
      const anchor = anchorRef.current;
      const next = anchor.position + ((now - anchor.clock) / 1000) * speed;
      const clamped = syncPosition(next);
      if (clamped < totalTime) {
        animationRef.current = requestAnimationFrame(tick);
      } else {
        animationRef.current = 0;
        setIsPlaying(false);
      }
    };
    animationRef.current = requestAnimationFrame(tick);
    return () => {
      if (animationRef.current) cancelAnimationFrame(animationRef.current);
      animationRef.current = 0;
    };
  }, [events.length, isPlaying, speed, syncPosition, totalTime]);

  const togglePlaying = useCallback(() => {
    if (!events.length || !totalTime) return;
    if (!isPlaying && positionRef.current >= totalTime) seekTo(0);
    setIsPlaying(!isPlaying);
  }, [events.length, isPlaying, seekTo, totalTime]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== " " && event.key !== "Spacebar") return;
      if (isInteractiveReplayTarget(event.target)) return;
      event.preventDefault();
      togglePlaying();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [togglePlaying]);

  const elapsedMs = Math.round(position * 1000);
  const totalMs = Math.round(totalTime * 1000);
  const progressRatio = totalTime ? position / totalTime : 0;

  return <>
    <div className="terminal-controls">
      <button type="button" onClick={togglePlaying}>{isPlaying ? t("auditReplayPause") : t("auditReplayPlay")}</button>
      <label>{t("auditReplaySpeed")}
        <select value={speed} onChange={(event) => setSpeed(Number(event.target.value))}>
          <option value={1}>1x</option>
          <option value={2}>2x</option>
          <option value={4}>4x</option>
        </select>
      </label>
      <div className="terminal-progress">
        <div
          className="terminal-progress-track"
          role="slider"
          tabIndex={0}
          aria-label={t("auditReplaySeek")}
          aria-valuemin={0}
          aria-valuemax={totalMs}
          aria-valuenow={elapsedMs}
          onClick={(event) => {
            if (!totalTime) return;
            const rect = event.currentTarget.getBoundingClientRect();
            seekTo(((event.clientX - rect.left) / rect.width) * totalTime);
          }}
          onKeyDown={(event) => {
            if (!totalTime) return;
            if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
              event.preventDefault();
              const step = event.shiftKey ? 10 : 1;
              seekTo(positionRef.current + (event.key === "ArrowRight" ? step : -step));
            }
            if (event.key === "Home") {
              event.preventDefault();
              seekTo(0);
            }
            if (event.key === "End") {
              event.preventDefault();
              seekTo(totalTime);
            }
          }}
        >
          <div className="terminal-progress-bar" style={{ width: `${progressRatio * 100}%`, minWidth: position > 0 ? 2 : 0 }} />
        </div>
        <span className="terminal-progress-time">{durationText(elapsedMs)} / {durationText(totalMs)}</span>
      </div>
      <button type="button" onClick={() => {
        seekTo(0);
        setIsPlaying(true);
      }}>{t("auditReplayRestart")}</button>
    </div>
    <div className="terminal-output" ref={containerRef} />
  </>;
}

function parseHeader(lines: unknown[]) {
  const first = lines[0];
  if (!first || Array.isArray(first) || typeof first !== "object") return { width: 100, height: 28 };
  const record = first as Record<string, unknown>;
  return { width: Number(record.width || 100), height: Number(record.height || 28) };
}

function parseEvents(lines: unknown[]) {
  return lines.flatMap((line) => {
    if (!Array.isArray(line) || line.length < 3 || line[1] !== "o" || typeof line[2] !== "string") return [];
    return [{ time: Number(line[0] || 0), data: line[2] }];
  });
}

function durationText(value?: number) {
  if (value === undefined || value === null || Number.isNaN(value)) return "-";
  if (value < 1000) return `${value}ms`;
  return `${Math.round(value / 100) / 10}s`;
}

function isInteractiveReplayTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return Boolean(target.closest("button, input, select, textarea, [role='button'], [role='combobox']"));
}
