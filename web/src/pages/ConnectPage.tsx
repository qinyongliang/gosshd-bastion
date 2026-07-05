import { useQuery } from "@tanstack/react-query";
import Editor, { loader } from "@monaco-editor/react";
import type { BeforeMount, Monaco, OnMount } from "@monaco-editor/react";
import * as monaco from "monaco-editor";
import "monaco-editor-nginx";
import { Terminal } from "@xterm/xterm";
import { Activity, ArrowLeft, ChevronDown, ChevronLeft, ChevronRight, Cpu, Folder, Globe, GripVertical, HardDrive, Maximize, Minimize, Monitor, Network, RefreshCw, Save, Search, Server, SplitSquareHorizontal, SplitSquareVertical, X } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { ManualReviewPoller } from "../components/ManualReviewPoller";
import { Segmented } from "../components/ui";
import { useI18n } from "../i18n";
import { appDescription, appName, documentTitle } from "../lib/branding";
import { useTheme } from "../theme";
import type { ConsoleData, Target, TargetSystemSnapshot, TargetSystemUsage } from "../types";
import { tagColor, targetEndpoint } from "../utils";
import { FileManager } from "./FileManager";
import aiCollaborationIcon from "../assets/ai-collaboration.png";

let monacoConfigured = false;
loader.config({ monaco });

const FALLBACK_EDITOR_LANGUAGE = "gosshd-config-fallback";

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
type ConnectionTab = {
  id: string;
  targetID: string;
  layout: PaneNode;
  activePaneID: string;
  pendingCommand?: string;
};

type PaneNode = TerminalPaneNode | EditorPaneNode | SplitPaneNode;
type PaneDirection = "row" | "column";
type PaneSide = "left" | "right" | "up" | "down";
type TerminalPaneNode = {
  type: "terminal";
  id: string;
  targetID: string;
};
type EditorPaneNode = {
  type: "editor";
  id: string;
  targetID: string;
  path: string;
};
type SplitPaneNode = {
  type: "split";
  id: string;
  direction: PaneDirection;
  ratio: number;
  first: PaneNode;
  second: PaneNode;
};

const DEFAULT_COLS = 120;
const DEFAULT_ROWS = 32;
const SYSTEM_METRICS_REFRESH_MS = 5000;
const SYSTEM_DISK_REFRESH_MS = 60_000;
const MAX_SYSTEM_SAMPLES = 60;
const EDITOR_PANE_CLOSE_REQUEST = "gosshd-editor-pane-close-request";
type TerminalPanelProps = {
  data: ConsoleData;
  target: Target;
  paneID: string;
  active?: boolean;
  isFullscreen: boolean;
  onFullscreenChange: (value: boolean | ((previous: boolean) => boolean)) => void;
  onClose?: () => void;
  onSplit?: (side: PaneSide) => void;
  pendingCommand?: string;
  onPendingCommandConsumed?: () => void;
  manualReview?: boolean;
};

type TerminalRuntime = {
  paneID: string;
  targetID: string;
  terminal: Terminal | null;
  socket: WebSocket | null;
  heartbeat: number | null;
  status: ConnectionStatus;
  error: string;
  dims: { cols: number; rows: number };
  sessionID: string;
  aiEnabled: boolean;
  pendingCommand: string;
  active: boolean;
  disposed: boolean;
  onClose?: () => void;
  onPendingCommandConsumed?: () => void;
  connect?: () => void;
  fit?: () => void;
  outputQueue: string[];
  outputFrame: number | null;
  listeners: Set<() => void>;
  disposables: Array<{ dispose: () => void }>;
};

const terminalRuntimes = new Map<string, TerminalRuntime>();

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
  const [tabs, setTabs] = useState<ConnectionTab[]>(() => [newConnectionTab(target.id, getConnectCommandParam())]);
  const [activeTabID, setActiveTabID] = useState(() => tabs[0]?.id || "");
  const [tabMenu, setTabMenu] = useState<{ tabID: string; x: number; y: number } | null>(null);
  const [switcherOpenSignal, setSwitcherOpenSignal] = useState(0);
  const expectedRouteTabIDRef = useRef("");
  const suppressedRouteTargetIDRef = useRef("");
  const seenOpenMessageIDsRef = useRef<Set<string>>(new Set());
  const bodyRef = useRef<HTMLElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const activeTab = tabs.find((item) => item.id === activeTabID) || tabs[0] || null;
  const activePane = activeTab ? findPane(activeTab.layout, activeTab.activePaneID) : null;
  const activeTargetID = activePane && activePane.type !== "split" ? activePane.targetID : activeTab?.targetID;
  const activeTarget = (activeTargetID ? targets.find((item) => item.id === activeTargetID) : null) || target;
  const openTabs = tabs
    .map((tab) => ({ tab, target: targets.find((item) => item.id === tab.targetID) }))
    .filter((item): item is { tab: ConnectionTab; target: Target } => Boolean(item.target));
  const hasOpenTabs = openTabs.length > 0;
  const endpoint = targetEndpoint(activeTarget);
  const name = appName(data.runtime);
  const description = appDescription(data.runtime);
  const routePendingCommand = getConnectCommandParam();
  const system = useQuery({
    queryKey: ["target-system", activeTarget.id],
    queryFn: () => api.targetSystem(activeTarget.id),
    staleTime: Infinity,
    retry: 1,
  });
  const systemMetrics = useQuery({
    queryKey: ["target-system", activeTarget.id, "metrics"],
    queryFn: () => api.targetSystemMetrics(activeTarget.id),
    refetchInterval: SYSTEM_METRICS_REFRESH_MS,
    staleTime: 4000,
    retry: 1,
  });
  const systemFilesystems = useQuery({
    queryKey: ["target-system", activeTarget.id, "filesystems"],
    queryFn: () => api.targetSystemFilesystems(activeTarget.id),
    refetchInterval: SYSTEM_DISK_REFRESH_MS,
    staleTime: SYSTEM_DISK_REFRESH_MS - 1000,
    retry: 1,
  });
  const systemSnapshot = useMemo(
    () => mergeSystemSnapshot(system.data, systemMetrics.data, systemFilesystems.data),
    [system.data, systemMetrics.data, systemFilesystems.data],
  );
  const refreshSystem = () => {
    void system.refetch();
    void systemMetrics.refetch();
    void systemFilesystems.refetch();
  };

  const appendConnectionTab = (nextTargetID: string, command = "") => {
    const next = newConnectionTab(nextTargetID, command);
    expectedRouteTabIDRef.current = next.id;
    suppressedRouteTargetIDRef.current = "";
    setTabs((current) => [...current, next]);
    setActiveTabID(next.id);
    navigate(`/targets/${nextTargetID}/connect`, { replace: true });
  };

  useEffect(() => {
    if (expectedRouteTabIDRef.current) {
      expectedRouteTabIDRef.current = "";
      return;
    }
    if (!activeTab) return;
    if (suppressedRouteTargetIDRef.current === target.id) return;
    if (activeTab.targetID === target.id) return;
    const existing = tabs.find((item) => item.targetID === target.id);
    if (existing) {
      setActiveTabID(existing.id);
      return;
    }
    const next = newConnectionTab(target.id, getConnectCommandParam());
    expectedRouteTabIDRef.current = next.id;
    setActiveTabID(next.id);
    setTabs((current) => [...current, next]);
  }, [activeTab?.targetID, tabs, target.id]);

  useEffect(() => {
    const openFromMessage = (raw: unknown) => {
      const message = raw as { type?: string; targetID?: string; command?: string; messageID?: string } | null;
      if (!message || message.type !== "gosshd-connect-open-target" || !message.targetID) return;
      if (!targets.some((item) => item.id === message.targetID)) return;
      if (message.messageID) {
        if (seenOpenMessageIDsRef.current.has(message.messageID)) return;
        seenOpenMessageIDsRef.current.add(message.messageID);
        if (seenOpenMessageIDsRef.current.size > 100) {
          const recent = Array.from(seenOpenMessageIDsRef.current).slice(-50);
          seenOpenMessageIDsRef.current.clear();
          for (const id of recent) seenOpenMessageIDsRef.current.add(id);
        }
      }
      appendConnectionTab(message.targetID, message.command || "");
    };
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      openFromMessage(event.data);
    };
    const onStorage = (event: StorageEvent) => {
      if (event.key !== "gosshd-connect-open" || !event.newValue) return;
      try {
        openFromMessage(JSON.parse(event.newValue));
      } catch {
        // Ignore malformed messages from stale tabs.
      }
    };
    window.addEventListener("message", onMessage);
    window.addEventListener("storage", onStorage);
    return () => {
      window.removeEventListener("message", onMessage);
      window.removeEventListener("storage", onStorage);
    };
  }, [targets]);

  useEffect(() => {
    document.title = documentTitle(serverTitle(activeTarget), data.runtime);
  }, [activeTarget, data.runtime]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const clearOnline = () => {
      try {
        window.localStorage.removeItem("gosshd-connect-window-online");
      } catch {
        // Ignore storage failures during page teardown.
      }
    };
    const markOnline = () => {
      window.localStorage.setItem("gosshd-connect-window-online", JSON.stringify({ at: Date.now() }));
    };
    markOnline();
    const timer = window.setInterval(markOnline, 1000);
    window.addEventListener("pagehide", clearOnline);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("pagehide", clearOnline);
      clearOnline();
    };
  }, []);

  useEffect(() => {
    if (data.runtime.client_mode) return;
    const promptOnUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", promptOnUnload);
    return () => window.removeEventListener("beforeunload", promptOnUnload);
  }, [data.runtime.client_mode]);

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
    appendConnectionTab(nextTargetID);
  };

  const updateActiveTab = (updater: (tab: ConnectionTab) => ConnectionTab) => {
    setTabs((current) => current.map((tab) => tab.id === activeTabID ? updater(tab) : tab));
  };

  const splitActivePane = (side: PaneSide, targetID?: string) => {
    if (!activeTab) return;
    const basePane = findPane(activeTab.layout, activeTab.activePaneID);
    if (!basePane || basePane.type === "split") return;
    const nextTargetID = targetID || basePane.targetID;
    const nextPane = newTerminalPane(nextTargetID);
    updateActiveTab((tab) => ({
      ...tab,
      layout: splitPane(tab.layout, tab.activePaneID, nextPane, side),
      activePaneID: nextPane.id,
      targetID: nextTargetID,
    }));
    scheduleAllTerminalFits();
  };

  const splitTabIntoActiveTab = (sourceTabID: string, side: PaneSide) => {
    if (!activeTab || sourceTabID === activeTab.id) return;
    setTabMenu(null);
    setTabs((current) => {
      const source = current.find((item) => item.id === sourceTabID);
      const destination = current.find((item) => item.id === activeTabID);
      if (!source || !destination || source.id === destination.id) return current;
      const nextLayout = splitPane(destination.layout, destination.activePaneID, source.layout, side);
      return current
        .filter((item) => item.id !== source.id)
        .map((item) => item.id === destination.id ? {
          ...item,
          layout: nextLayout,
          activePaneID: destination.activePaneID,
        } : item);
    });
    scheduleAllTerminalFits();
  };

  const closeActivePane = () => {
    if (!activeTab) return;
    const pane = findPane(activeTab.layout, activeTab.activePaneID);
    if (pane?.type === "editor") {
      window.dispatchEvent(new CustomEvent(EDITOR_PANE_CLOSE_REQUEST, { detail: { paneID: pane.id } }));
      return;
    }
    closePane(activeTab.id, activeTab.activePaneID);
  };

  const closePane = (tabID: string, paneID: string) => {
    const tab = tabs.find((item) => item.id === tabID);
    if (tab && tab.layout.id === paneID) {
      closeTabs("one", tabID);
      return;
    }
    if (tab) disposePaneRuntime(findPane(tab.layout, paneID));
    setTabs((current) => current.map((tab) => {
      if (tab.id !== tabID) return tab;
      const result = removePane(tab.layout, paneID);
      if (!result.node) return tab;
      const fallbackID = result.activePaneID || firstLeafID(result.node);
      const fallbackPane = findPane(result.node, fallbackID);
      return { ...tab, layout: result.node, activePaneID: fallbackID, targetID: fallbackPane && fallbackPane.type !== "split" ? fallbackPane.targetID : tab.targetID };
    }));
    scheduleAllTerminalFits();
  };

  const openEditorForActiveTarget = (filePath: string) => {
    if (!activeTab) return;
    const paneTargetID = activeTarget.id;
    const editorPane: EditorPaneNode = { type: "editor", id: newPaneID("editor"), targetID: paneTargetID, path: filePath };
    updateActiveTab((tab) => ({
      ...tab,
      layout: splitPane(tab.layout, tab.activePaneID, editorPane, "up"),
      activePaneID: editorPane.id,
    }));
  };

  const closeTabs = (mode: "one" | "left" | "right" | "others" | "all", tabID: string) => {
    setTabMenu(null);
    setTabs((current) => {
      const index = current.findIndex((item) => item.id === tabID);
      if (index < 0) return current;
      const closedRouteTarget = current[index]?.targetID || "";
      const closingActive = activeTabID === tabID || (mode !== "one" && tabSelectionIncludes(current, index, mode, activeTabID));
      let next = current;
      if (mode === "one") next = current.filter((item) => item.id !== tabID);
      if (mode === "left") next = current.slice(index);
      if (mode === "right") next = current.slice(0, index + 1);
      if (mode === "others") next = [current[index]];
      if (mode === "all") next = [];

      for (const tab of current) {
        if (!next.some((item) => item.id === tab.id)) disposePaneRuntime(tab.layout);
      }

      if (!next.length) {
        suppressedRouteTargetIDRef.current = closedRouteTarget || target.id;
        setActiveTabID("");
        setTerminalFullscreen(false);
        return next;
      }
      if (closingActive || !next.some((item) => item.id === activeTabID)) {
        const fallback = next[Math.min(index, next.length - 1)];
        expectedRouteTabIDRef.current = fallback.id;
        suppressedRouteTargetIDRef.current = closedRouteTarget;
        setActiveTabID(fallback.id);
        window.setTimeout(() => navigate(`/targets/${fallback.targetID}/connect`, { replace: true }), 0);
      }
      return next;
    });
    scheduleAllTerminalFits();
  };

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const isClientMode = Boolean(data.runtime.client_mode);
      const openSwitcher = isClientMode
        ? event.ctrlKey && !event.altKey && !event.metaKey && keyMatches(event, "n", "KeyN")
        : event.altKey && !event.ctrlKey && !event.metaKey && keyMatches(event, "n", "KeyN");
      const closeCurrentPane = isClientMode
        ? event.ctrlKey && !event.altKey && !event.metaKey && keyMatches(event, "w", "KeyW")
        : event.altKey && !event.ctrlKey && !event.metaKey && keyMatches(event, "w", "KeyW");
      const closeDisconnectedPane = isClientMode
        ? event.ctrlKey && !event.altKey && !event.metaKey && keyMatches(event, "d", "KeyD")
        : event.altKey && !event.ctrlKey && !event.metaKey && keyMatches(event, "d", "KeyD");

      if (openSwitcher) {
        event.preventDefault();
        event.stopPropagation();
        if (event.repeat) return;
        setTabMenu(null);
        setSwitcherOpenSignal((value) => value + 1);
        return;
      }
      if (closeCurrentPane || closeDisconnectedPane) {
        event.preventDefault();
        event.stopPropagation();
        if (event.repeat) return;
        closeActivePane();
      }
    };
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [activeTab?.id, activeTab?.activePaneID, data.runtime.client_mode]);

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
            <strong>{name}</strong>
            <span>{description}</span>
          </div>
        </div>

        <ServerSwitcher targets={targets} folders={data.targetFolders} currentTargetID={activeTarget.id} openSignal={switcherOpenSignal} onOpenTarget={activateTarget} />

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
        activeTabID={activeTab?.id || ""}
        menu={tabMenu}
        onActivate={(tab) => {
          expectedRouteTabIDRef.current = tab.id;
          setActiveTabID(tab.id);
          navigate(`/targets/${tab.targetID}/connect`, { replace: true });
        }}
        onClose={(tabID) => closeTabs("one", tabID)}
        onMenu={(tabID, point) => setTabMenu({ tabID, ...point })}
        onMenuAction={closeTabs}
        onSplitTab={splitTabIntoActiveTab}
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
              <SystemSnapshotPanel
                targetID={activeTarget.id}
                snapshot={systemSnapshot}
                sampleSnapshot={systemMetrics.data || system.data}
                isLoading={system.isLoading}
                isFetching={system.isFetching || systemMetrics.isFetching || systemFilesystems.isFetching}
                error={system.error || systemMetrics.error || systemFilesystems.error}
                onRefresh={refreshSystem}
              />
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
              {hasOpenTabs ? (
                tabs.map((tab) => (
                  <div key={tab.id} className={`terminal-tab-layer ${tab.id === activeTabID ? "active" : "inactive"}`}>
                    <PaneTree
                      data={data}
                      node={tab.layout}
                      targets={targets}
                      activePaneID={tab.activePaneID}
                      isFullscreen={terminalFullscreen && tab.id === activeTabID}
                      tabActive={tab.id === activeTabID}
                      pendingCommand={tab.id === activeTabID ? (tab.pendingCommand || routePendingCommand) : ""}
                      onPendingCommandConsumed={() => {
                        clearConnectCommandParam();
                        setTabs((current) => current.map((item) => item.id === tab.id ? { ...item, pendingCommand: "" } : item));
                      }}
                      onActivate={(paneID) => {
                        setActiveTabID(tab.id);
                        setTabs((current) => current.map((item) => item.id === tab.id ? { ...item, activePaneID: paneID } : item));
                      }}
                      onClose={(paneID) => closePane(tab.id, paneID)}
                      onFullscreenChange={setTerminalFullscreen}
                      onSplit={(side, targetID) => {
                        setActiveTabID(tab.id);
                        setTabs((current) => current.map((item) => {
                          if (item.id !== tab.id) return item;
                          const basePane = findPane(item.layout, item.activePaneID);
                          if (!basePane || basePane.type === "split") return item;
                          const nextPane = newTerminalPane(targetID || basePane.targetID);
                          return {
                            ...item,
                            layout: splitPane(item.layout, item.activePaneID, nextPane, side),
                            activePaneID: nextPane.id,
                            targetID: nextPane.targetID,
                          };
                        }));
                        scheduleAllTerminalFits();
                      }}
                      onResizeSplit={(splitID, ratio) => {
                        setTabs((current) => current.map((item) => item.id === tab.id ? { ...item, layout: resizeSplit(item.layout, splitID, ratio) } : item));
                        scheduleAllTerminalFits();
                      }}
                    />
                  </div>
                ))
              ) : (
                <div className="connect-zone-empty">
                  <strong>{t("connectNoOpenTabsTitle")}</strong>
                  <span>{t("connectNoOpenTabsBody")}</span>
                </div>
              )}
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
            {hasOpenTabs ? (
              <FileManager target={activeTarget} system={systemSnapshot} nativeOpen={Boolean(data.runtime.client_mode)} onEditFile={openEditorForActiveTarget} />
            ) : (
              <div className="connect-zone-empty">
                <span>{t("connectFilesNoOpenTabs")}</span>
              </div>
            )}
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
  activeTabID,
  menu,
  onActivate,
  onClose,
  onMenu,
  onMenuAction,
  onSplitTab,
}: {
  tabs: Array<{ tab: ConnectionTab; target: Target }>;
  activeTabID: string;
  menu: { tabID: string; x: number; y: number } | null;
  onActivate: (tab: ConnectionTab) => void;
  onClose: (tabID: string) => void;
  onMenu: (tabID: string, point: { x: number; y: number }) => void;
  onMenuAction: (mode: "one" | "left" | "right" | "others" | "all", tabID: string) => void;
  onSplitTab: (tabID: string, side: PaneSide) => void;
}) {
  const tabsRef = useRef<HTMLElement>(null);
  if (!tabs.length) return null;
  const menuTarget = menu ? tabs.find((item) => item.tab.id === menu.tabID) : null;
  return (
    <section ref={tabsRef} className="connection-tabs" aria-label="Connection tabs">
      <div className="connection-tabs-scroll">
        {tabs.map(({ tab, target }, index) => (
          <div
            key={tab.id}
            className={`connection-tab ${tab.id === activeTabID ? "active" : ""}`}
            onContextMenu={(event) => {
              event.preventDefault();
              onMenu(tab.id, contextMenuPointInTabs(event.clientX, event.clientY, tabsRef.current));
            }}
            title={serverTitle(target)}
          >
            <button type="button" className="connection-tab-main" onClick={() => onActivate(tab)}>
              {target.target_type === "agent" ? <Server /> : <HardDrive />}
              <span>
                <strong>{target.name}</strong>
                <code>{target.alias}{sameTargetTabCount(tabs, target.id) > 1 ? ` #${sameTargetTabIndex(tabs, target.id, index)}` : ""}</code>
              </span>
            </button>
            <button type="button" className="connection-tab-close" aria-label="Close tab" onClick={() => onClose(tab.id)}>
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
          <button type="button" role="menuitem" onClick={() => onMenuAction("one", menuTarget.tab.id)}>关闭当前</button>
          {menuTarget.tab.id !== activeTabID && <button type="button" role="menuitem" onClick={() => onSplitTab(menuTarget.tab.id, "left")}>拆分到左侧</button>}
          {menuTarget.tab.id !== activeTabID && <button type="button" role="menuitem" onClick={() => onSplitTab(menuTarget.tab.id, "down")}>拆分到下方</button>}
          <button type="button" role="menuitem" onClick={() => onMenuAction("left", menuTarget.tab.id)}>关闭左侧</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("right", menuTarget.tab.id)}>关闭右侧</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("others", menuTarget.tab.id)}>关闭其他</button>
          <button type="button" role="menuitem" onClick={() => onMenuAction("all", menuTarget.tab.id)}>关闭全部</button>
        </div>
      )}
    </section>
  );
}

function PaneTree({
  data,
  node,
  targets,
  activePaneID,
  isFullscreen,
  onActivate,
  onClose,
  onFullscreenChange,
  onSplit,
  onResizeSplit,
  tabActive,
  pendingCommand,
  onPendingCommandConsumed,
}: {
  data: ConsoleData;
  node: PaneNode;
  targets: Target[];
  activePaneID: string;
  isFullscreen: boolean;
  onActivate: (paneID: string) => void;
  onClose: (paneID: string) => void;
  onFullscreenChange: (value: boolean | ((previous: boolean) => boolean)) => void;
  onSplit: (side: PaneSide, targetID?: string) => void;
  onResizeSplit: (splitID: string, ratio: number) => void;
  tabActive: boolean;
  pendingCommand?: string;
  onPendingCommandConsumed?: () => void;
}) {
  if (node.type === "split") {
    return <SplitPaneView data={data} node={node} targets={targets} activePaneID={activePaneID} isFullscreen={isFullscreen} tabActive={tabActive} pendingCommand={pendingCommand} onPendingCommandConsumed={onPendingCommandConsumed} onActivate={onActivate} onClose={onClose} onFullscreenChange={onFullscreenChange} onSplit={onSplit} onResizeSplit={onResizeSplit} />;
  }
  const target = targets.find((item) => item.id === node.targetID);
  if (!target) return null;
  const active = tabActive && node.id === activePaneID;
  const activate = () => onActivate(node.id);
  if (node.type === "editor") {
    return <EditorPane paneID={node.id} target={target} filePath={node.path} active={active} onActivate={activate} onClose={() => onClose(node.id)} />;
  }
  return (
    <div className={`pane-leaf ${active ? "active" : ""}`} onPointerDown={activate}>
      <TerminalPanel
        data={data}
        target={target}
        active={active}
        isFullscreen={isFullscreen && active}
        onFullscreenChange={onFullscreenChange}
        onClose={() => onClose(node.id)}
        onSplit={onSplit}
        paneID={node.id}
        pendingCommand={pendingCommand}
        onPendingCommandConsumed={onPendingCommandConsumed}
      />
    </div>
  );
}

function SplitPaneView({
  data,
  node,
  targets,
  activePaneID,
  isFullscreen,
  onActivate,
  onClose,
  onFullscreenChange,
  onSplit,
  onResizeSplit,
  tabActive,
  pendingCommand,
  onPendingCommandConsumed,
}: {
  data: ConsoleData;
  node: SplitPaneNode;
  targets: Target[];
  activePaneID: string;
  isFullscreen: boolean;
  onActivate: (paneID: string) => void;
  onClose: (paneID: string) => void;
  onFullscreenChange: (value: boolean | ((previous: boolean) => boolean)) => void;
  onSplit: (side: PaneSide, targetID?: string) => void;
  onResizeSplit: (splitID: string, ratio: number) => void;
  tabActive: boolean;
  pendingCommand?: string;
  onPendingCommandConsumed?: () => void;
}) {
  const splitRef = useRef<HTMLDivElement>(null);
  const startResize = (event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    const rect = splitRef.current?.getBoundingClientRect();
    if (!rect) return;
    const onPointerMove = (moveEvent: PointerEvent) => {
      const raw = node.direction === "row" ? (moveEvent.clientX - rect.left) / rect.width : (moveEvent.clientY - rect.top) / rect.height;
      onResizeSplit(node.id, clampNumber(raw, 0.18, 0.82));
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
    <div ref={splitRef} className={`pane-split ${node.direction}`} style={{ "--split-ratio": `${node.ratio * 100}%` } as CSSProperties}>
      <PaneTree data={data} node={node.first} targets={targets} activePaneID={activePaneID} isFullscreen={isFullscreen} tabActive={tabActive} pendingCommand={pendingCommand} onPendingCommandConsumed={onPendingCommandConsumed} onActivate={onActivate} onClose={onClose} onFullscreenChange={onFullscreenChange} onSplit={onSplit} onResizeSplit={onResizeSplit} />
      <button type="button" className="pane-splitter" onPointerDown={startResize} aria-label="Resize pane" />
      <PaneTree data={data} node={node.second} targets={targets} activePaneID={activePaneID} isFullscreen={isFullscreen} tabActive={tabActive} pendingCommand={pendingCommand} onPendingCommandConsumed={onPendingCommandConsumed} onActivate={onActivate} onClose={onClose} onFullscreenChange={onFullscreenChange} onSplit={onSplit} onResizeSplit={onResizeSplit} />
    </div>
  );
}

function EditorPane({ paneID, target, filePath, active, onActivate, onClose }: { paneID: string; target: Target; filePath: string; active: boolean; onActivate: () => void; onClose: () => void }) {
  const { t } = useI18n();
  const { theme } = useTheme();
  const editorHostRef = useRef<HTMLDivElement>(null);
  const editorLayoutRef = useRef<Parameters<OnMount>[0] | null>(null);
  const editorLayoutFrameRef = useRef<number | null>(null);
  const editorLayoutRetryRef = useRef<number | null>(null);
  const file = useQuery({
    queryKey: ["target-file-read", target.id, filePath],
    queryFn: () => api.readFile(target.id, filePath),
  });
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [error, setError] = useState("");
  const dirty = content !== savedContent;
  const editorLanguage = inferEditorLanguage(filePath);
  const editorTheme = editorLanguage === "nginx" ? (theme === "dark" ? "nginx-theme-dark" : "nginx-theme") : (theme === "dark" ? "vs-dark" : "light");

  useEffect(() => {
    if (!file.data) return;
    setContent(file.data.content);
    setSavedContent(file.data.content);
    setError("");
  }, [file.data?.content, file.data?.path]);

  const save = async () => {
    try {
      await api.writeFile(target.id, filePath, content);
      setSavedContent(content);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const close = () => {
    if (dirty && !window.confirm(t("connectEditorCloseDirtyConfirm"))) return;
    onClose();
  };

  useEffect(() => {
    if (!active) return;
    const onKeyDown = (event: KeyboardEvent) => {
      const saveShortcut = (event.ctrlKey || event.altKey) && !event.metaKey && keyMatches(event, "s", "KeyS");
      if (saveShortcut) {
        event.preventDefault();
        event.stopPropagation();
        if (!event.repeat) void save();
      }
    };
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [active, content, dirty, filePath, target.id]);

  useEffect(() => {
    if (!active) return;
    const onCloseRequest = (event: Event) => {
      const paneIDToClose = (event as CustomEvent<{ paneID?: string }>).detail?.paneID || "";
      if (paneIDToClose === paneID) close();
    };
    window.addEventListener(EDITOR_PANE_CLOSE_REQUEST, onCloseRequest);
    return () => window.removeEventListener(EDITOR_PANE_CLOSE_REQUEST, onCloseRequest);
  }, [active, content, dirty, filePath, paneID, target.id]);

  const layoutEditor = () => {
    const host = editorHostRef.current;
    const editor = editorLayoutRef.current;
    if (!host || !editor) return;
    const rect = host.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;
    editor.layout({ width: Math.floor(rect.width), height: Math.floor(rect.height) });
  };

  const scheduleEditorLayout = () => {
    if (editorLayoutFrameRef.current) window.cancelAnimationFrame(editorLayoutFrameRef.current);
    editorLayoutFrameRef.current = window.requestAnimationFrame(() => {
      editorLayoutFrameRef.current = null;
      layoutEditor();
    });
  };

  useEffect(() => {
    const host = editorHostRef.current;
    if (!host) return;
    const resizeObserver = new ResizeObserver(scheduleEditorLayout);
    resizeObserver.observe(host);
    scheduleEditorLayout();
    return () => {
      resizeObserver.disconnect();
      if (editorLayoutFrameRef.current) window.cancelAnimationFrame(editorLayoutFrameRef.current);
      if (editorLayoutRetryRef.current) window.clearTimeout(editorLayoutRetryRef.current);
      editorLayoutFrameRef.current = null;
      editorLayoutRetryRef.current = null;
    };
  }, [file.data?.path]);

  useEffect(() => {
    scheduleEditorLayout();
    const retry = window.setTimeout(scheduleEditorLayout, 80);
    return () => window.clearTimeout(retry);
  }, [active, filePath, theme, file.isLoading, Boolean(error)]);

  const beforeMountEditor: BeforeMount = (monaco) => {
    configureMonaco(monaco);
  };

  const mountEditor: OnMount = (editor, monaco) => {
    configureMonaco(monaco);
    editorLayoutRef.current = editor;
    editor.addAction({
      id: "gosshd-toggle-line-comment",
      label: "Toggle line comment",
      keybindings: [
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Slash,
        monaco.KeyMod.Alt | monaco.KeyCode.Slash,
      ],
      run: (instance) => {
        instance.trigger("gosshd", "editor.action.commentLine", null);
      },
    });
    scheduleEditorLayout();
    if (editorLayoutRetryRef.current) window.clearTimeout(editorLayoutRetryRef.current);
    editorLayoutRetryRef.current = window.setTimeout(scheduleEditorLayout, 80);
  };

  return (
    <section className={`editor-pane pane-leaf ${active ? "active" : ""}`} onPointerDown={onActivate}>
      <header className="editor-pane-head">
        <span title={filePath}>{dirty ? "* " : ""}{filePath}</span>
        <div className="editor-pane-actions">
          <button type="button" className="icon-button" onClick={() => void save()} title={t("save")} disabled={file.isLoading}>
            <Save />
          </button>
          <button type="button" className="icon-button" onClick={close} title={t("commonClose")}>
            <X />
          </button>
        </div>
      </header>
      {error && <div className="editor-pane-error">{error}</div>}
      {file.isLoading ? (
        <div className="connect-zone-empty"><span>{t("loading")}</span></div>
      ) : file.error ? (
        <div className="connect-zone-empty"><span>{file.error instanceof Error ? file.error.message : String(file.error)}</span></div>
      ) : (
        <div className="editor-monaco-host" ref={editorHostRef}>
          <Editor
            className="editor-pane-monaco"
            height="100%"
            width="100%"
            theme={editorTheme}
            path={filePath}
            language={editorLanguage}
            value={content}
            wrapperProps={{ className: "editor-monaco-wrapper" }}
            beforeMount={beforeMountEditor}
            onMount={mountEditor}
            onChange={(value) => setContent(value ?? "")}
            options={{ minimap: { enabled: false }, fontSize: 13, wordWrap: "on", scrollBeyondLastLine: false, automaticLayout: true }}
          />
        </div>
      )}
    </section>
  );
}

function inferEditorLanguage(path: string) {
  const normalized = path.replace(/\\/g, "/").toLowerCase();
  const filename = normalized.split("/").pop() || normalized;
  if (/(^|\/)nginx(\/|$)/.test(normalized) || filename === "nginx.conf" || filename.endsWith(".nginx") || filename.endsWith(".nginx.conf")) return "nginx";
  if (filename === "dockerfile" || filename.startsWith("dockerfile.")) return "dockerfile";
  if (filename === "compose.yaml" || filename === "compose.yml" || filename === "docker-compose.yaml" || filename === "docker-compose.yml") return "yaml";
  if (filename === "makefile" || filename === "gnumakefile") return "shell";
  if (filename === "crontab" || filename === "hosts" || filename === "sudoers") return "shell";
  if (filename === ".gitconfig" || filename === ".gitmodules" || filename === "gitconfig") return "ini";
  if (filename.endsWith(".service") || filename.endsWith(".timer") || filename.endsWith(".socket") || filename.endsWith(".mount") || filename.endsWith(".target")) return "ini";
  if (filename === ".env" || filename.endsWith(".env") || filename.startsWith(".env.")) return "shell";
  if (filename.endsWith(".properties") || filename.endsWith(".propertis") || filename.endsWith(".props")) return "ini";
  if (filename.endsWith(".conf") || filename.endsWith(".config") || filename.endsWith(".cfg") || filename.endsWith(".cnf") || filename.endsWith(".ini")) return "ini";
  if (filename.endsWith(".yaml") || filename.endsWith(".yml")) return "yaml";
  if (filename.endsWith(".json") || filename.endsWith(".jsonc")) return "json";
  if (filename.endsWith(".toml")) return "ini";
  if (filename.endsWith(".hcl") || filename.endsWith(".tf") || filename.endsWith(".tfvars")) return "hcl";
  if (filename.endsWith(".xml") || filename.endsWith(".svg")) return "xml";
  if (filename.endsWith(".html") || filename.endsWith(".htm")) return "html";
  if (filename.endsWith(".vue") || filename.endsWith(".svelte")) return "html";
  if (filename.endsWith(".css")) return "css";
  if (filename.endsWith(".scss")) return "scss";
  if (filename.endsWith(".less")) return "less";
  if (filename.endsWith(".js") || filename.endsWith(".mjs") || filename.endsWith(".cjs")) return "javascript";
  if (filename.endsWith(".ts") || filename.endsWith(".tsx")) return "typescript";
  if (filename.endsWith(".jsx")) return "javascript";
  if (filename.endsWith(".sh") || filename.endsWith(".bash") || filename.endsWith(".zsh") || filename.endsWith(".profile") || filename.endsWith(".bashrc") || filename.endsWith(".zshrc")) return "shell";
  if (filename.endsWith(".ps1") || filename.endsWith(".psm1")) return "powershell";
  if (filename.endsWith(".py")) return "python";
  if (filename.endsWith(".go")) return "go";
  if (filename.endsWith(".rs")) return "rust";
  if (filename.endsWith(".java")) return "java";
  if (filename.endsWith(".kt") || filename.endsWith(".kts")) return "kotlin";
  if (filename.endsWith(".c") || filename.endsWith(".h") || filename.endsWith(".cc") || filename.endsWith(".cpp") || filename.endsWith(".cxx") || filename.endsWith(".hpp")) return "cpp";
  if (filename.endsWith(".cs")) return "csharp";
  if (filename.endsWith(".php")) return "php";
  if (filename.endsWith(".rb")) return "ruby";
  if (filename.endsWith(".lua")) return "lua";
  if (filename.endsWith(".pl") || filename.endsWith(".pm")) return "perl";
  if (filename.endsWith(".r")) return "r";
  if (filename.endsWith(".swift")) return "swift";
  if (filename.endsWith(".scala")) return "scala";
  if (filename.endsWith(".dart")) return "dart";
  if (filename.endsWith(".ex") || filename.endsWith(".exs")) return "elixir";
  if (filename.endsWith(".sol")) return "solidity";
  if (filename.endsWith(".proto")) return "protobuf";
  if (filename.endsWith(".graphql") || filename.endsWith(".gql")) return "graphql";
  if (filename.endsWith(".sql")) return "sql";
  if (filename.endsWith(".md") || filename.endsWith(".markdown")) return "markdown";
  return FALLBACK_EDITOR_LANGUAGE;
}

function configureMonaco(instance: Monaco) {
  if (monacoConfigured) return;
  monacoConfigured = true;
  const fallbackLanguageRegistered = instance.languages.getLanguages().some((language: { id: string }) => language.id === FALLBACK_EDITOR_LANGUAGE);
  if (!fallbackLanguageRegistered) {
    instance.languages.register({ id: FALLBACK_EDITOR_LANGUAGE });
  }
  instance.languages.setLanguageConfiguration(FALLBACK_EDITOR_LANGUAGE, {
    comments: { lineComment: "#" },
    brackets: [["{", "}"], ["(", ")"], ["[", "]"]],
    autoClosingPairs: [
      { open: "{", close: "}" },
      { open: "(", close: ")" },
      { open: "[", close: "]" },
      { open: "\"", close: "\"" },
      { open: "'", close: "'" },
    ],
  });
  instance.languages.setLanguageConfiguration("dockerfile", {
    comments: { lineComment: "#" },
    brackets: [["{", "}"], ["(", ")"], ["[", "]"]],
    autoClosingPairs: [
      { open: "{", close: "}" },
      { open: "(", close: ")" },
      { open: "[", close: "]" },
      { open: "\"", close: "\"" },
      { open: "'", close: "'" },
    ],
  });
  instance.languages.setLanguageConfiguration("nginx", {
    comments: { lineComment: "#" },
    brackets: [["{", "}"], ["(", ")"], ["[", "]"]],
    autoClosingPairs: [
      { open: "{", close: "}" },
      { open: "(", close: ")" },
      { open: "[", close: "]" },
      { open: "\"", close: "\"" },
      { open: "'", close: "'" },
    ],
  });
}

function SystemSnapshotPanel({
  targetID,
  snapshot,
  sampleSnapshot,
  isLoading,
  isFetching,
  error,
  onRefresh,
}: {
  targetID: string;
  snapshot?: TargetSystemSnapshot;
  sampleSnapshot?: TargetSystemSnapshot;
  isLoading: boolean;
  isFetching: boolean;
  error: Error | null;
  onRefresh: () => void;
}) {
  const { t } = useI18n();
  const [samples, setSamples] = useState<MetricSample[]>([]);
  const networkTrend = buildNetworkRates(samples);
  const interfaceRates = buildInterfaceNetworkRates(samples);

  useEffect(() => {
    setSamples([]);
  }, [targetID]);

  useEffect(() => {
    if (!sampleSnapshot) return;
    const sample = snapshotToSample(sampleSnapshot);
    setSamples((current) => [...current, sample].slice(-MAX_SYSTEM_SAMPLES));
  }, [sampleSnapshot]);

  return (
    <section className="connect-panel compact telemetry-panel">
      <header className="telemetry-head">
        <h3><Activity />{t("connectSystemInfo")}</h3>
        <button type="button" className="icon-button" onClick={onRefresh} disabled={isFetching} title={t("commonRefresh")}>
          <RefreshCw />
        </button>
      </header>

      {!snapshot && (
        <div className="telemetry-empty">
          <strong>{isLoading ? t("loading") : t("connectSystemUnavailable")}</strong>
          {error && <span>{String(error.message || "")}</span>}
        </div>
      )}

      {snapshot && (
        <>
          <div className="telemetry-ip-card">
            <span>{t("connectSystemIP")}</span>
            <strong title={snapshot.ip || snapshot.hostname || "-"}>{snapshot.ip || snapshot.hostname || "-"}</strong>
            {snapshot.public_ipv4 && <div><b>{t("connectSystemPublicIPv4")}</b><code title={snapshot.public_ipv4}>{snapshot.public_ipv4}</code></div>}
            {snapshot.public_ipv6 && <div><b>{t("connectSystemPublicIPv6")}</b><code title={snapshot.public_ipv6}>{snapshot.public_ipv6}</code></div>}
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

export function TerminalPanel({ data, target, paneID, active = true, isFullscreen, onFullscreenChange, onClose, onSplit, pendingCommand = "", onPendingCommandConsumed, manualReview = true }: TerminalPanelProps) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement>(null);
  const runtime = useMemo(() => getTerminalRuntime(paneID, target.id), [paneID, target.id]);
  const [status, setStatus] = useState<ConnectionStatus>(runtime.status);
  const [error, setError] = useState(runtime.error);
  const [dims, setDims] = useState(runtime.dims);
  const [sessionID, setSessionID] = useState(runtime.sessionID);
  const [aiEnabled, setAIEnabled] = useState(runtime.aiEnabled);
  const fitRetryRef = useRef<number | null>(null);
  const fitFrameRef = useRef<number | null>(null);

  useEffect(() => {
    const sync = () => {
      setStatus(runtime.status);
      setError(runtime.error);
      setDims(runtime.dims);
      setSessionID(runtime.sessionID);
      setAIEnabled(runtime.aiEnabled);
    };
    runtime.listeners.add(sync);
    sync();
    return () => {
      runtime.listeners.delete(sync);
    };
  }, [runtime]);

  useEffect(() => {
    runtime.active = active;
    runtime.onClose = onClose;
    runtime.onPendingCommandConsumed = onPendingCommandConsumed;
  }, [active, onClose, onPendingCommandConsumed, runtime]);

  const updateStatus = (next: ConnectionStatus) => {
    setRuntimeSnapshot(runtime, { status: next });
  };

  const sendAICollaborationState = (enabled: boolean) => {
    const socket = runtime.socket;
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "ai-collaboration", enabled }));
    }
  };

  const toggleAI = () => {
    const next = !runtime.aiEnabled;
    setRuntimeSnapshot(runtime, { aiEnabled: next });
    sendAICollaborationState(next);
  };

  const sendTerminalInput = (value: string) => {
    const socket = runtime.socket;
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "input", data: value }));
      return true;
    }
    return false;
  };

  const runPendingCommand = () => {
    const command = runtime.pendingCommand.trim();
    if (!command || runtime.status !== "connected") return;
    if (!sendTerminalInput(`${command}\r`)) return;
    runtime.pendingCommand = "";
    runtime.onPendingCommandConsumed?.();
  };

  const connect = () => {
    if (runtime.socket) {
      runtime.socket.close();
      runtime.socket = null;
    }
    clearQueuedTerminalOutput(runtime);
    updateStatus("connecting");
    setRuntimeSnapshot(runtime, { error: "", sessionID: "" });
    const terminal = runtime.terminal;
    if (!terminal) return;
    fitTerminal(terminal);

    const cols = terminal.cols || DEFAULT_COLS;
    const rows = terminal.rows || DEFAULT_ROWS;
    const url = api.targetTerminalURL(target.id, cols, rows);
    const socket = new WebSocket(url);
    runtime.socket = socket;
    runtime.targetID = target.id;
    const isCurrentSocket = () => runtime.socket === socket;

    socket.onopen = () => {
      if (!isCurrentSocket()) return;
      updateStatus("connected");
      terminal.focus();
      const currentCols = terminal.cols || cols;
      const currentRows = terminal.rows || rows;
      setRuntimeSnapshot(runtime, { dims: { cols: currentCols, rows: currentRows } });
      socket.send(JSON.stringify({ type: "resize", cols: currentCols, rows: currentRows }));
      socket.send(JSON.stringify({ type: "ai-collaboration", enabled: runtime.aiEnabled }));
      window.setTimeout(runPendingCommand, 80);
      if (runtime.heartbeat) window.clearInterval(runtime.heartbeat);
      runtime.heartbeat = window.setInterval(() => {
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
          enqueueTerminalOutput(runtime, terminal, message.data);
          if (isTerminalSessionClosedOutput(message.data)) {
            updateStatus("disconnected");
            setRuntimeSnapshot(runtime, { sessionID: "" });
          }
        } else if (message.type === "error" && message.data !== undefined) {
          enqueueTerminalOutput(runtime, terminal, `\r\n\x1b[1;31m${message.data}\x1b[0m\r\n`);
          updateStatus("error");
          setRuntimeSnapshot(runtime, { error: message.data });
        } else if (message.type === "exit") {
          enqueueTerminalOutput(runtime, terminal, `\r\n\x1b[2;37mSession ended (exit ${message.code ?? "-"})\x1b[0m\r\n`);
          updateStatus("disconnected");
          setRuntimeSnapshot(runtime, { sessionID: "" });
          socket.close();
        } else if (message.type === "session" && message.session_id) {
          setRuntimeSnapshot(runtime, { sessionID: message.session_id });
        }
      } catch {
        enqueueTerminalOutput(runtime, terminal, event.data);
      }
    };

    socket.onerror = () => {
      if (!isCurrentSocket()) return;
      updateStatus("error");
      setRuntimeSnapshot(runtime, { error: t("connectStatusError") });
    };

    socket.onclose = () => {
      if (!isCurrentSocket()) return;
      if (runtime.heartbeat) {
        window.clearInterval(runtime.heartbeat);
        runtime.heartbeat = null;
      }
      updateStatus(runtime.status === "connected" ? "disconnected" : runtime.status);
      setRuntimeSnapshot(runtime, { sessionID: "" });
    };
  };
  runtime.connect = connect;

  useEffect(() => {
    if (!active || !pendingCommand.trim()) return;
    runtime.pendingCommand = pendingCommand;
    runPendingCommand();
  }, [active, pendingCommand, runtime]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let terminal = runtime.terminal;
    if (!terminal) {
      terminal = new Terminal({
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
      runtime.terminal = terminal;

      terminal.attachCustomKeyEventHandler((event) => {
        if (event.type !== "keydown") return true;
        const pasteShortcut = event.ctrlKey && !event.altKey && !event.metaKey && keyMatches(event, "v", "KeyV");
        if (pasteShortcut && runtime.status === "connected") {
          if (!event.repeat) {
            void readClipboardText().then((text) => {
              if (!text) return;
              sendTerminalInput(text);
            });
          }
          return false;
        }
        if (runtime.status !== "disconnected" && runtime.status !== "error") return true;
        const ctrlD = event.ctrlKey && !event.altKey && !event.metaKey && keyMatches(event, "d", "KeyD");
        if (ctrlD) {
          if (!event.repeat && runtime.active) runtime.onClose?.();
          return false;
        }
        if (event.key === "Enter") {
          if (!event.repeat) runtime.connect?.();
          return false;
        }
        return true;
      });

      runtime.disposables.push(terminal.onData((value) => {
        if (runtime.status === "disconnected" || runtime.status === "error") {
          if (value === "\x04") {
            if (runtime.active) runtime.onClose?.();
            return;
          }
          if (value === "\r" || value === "\n") {
            runtime.connect?.();
            return;
          }
        }
        sendTerminalInput(value);
      }));

      runtime.disposables.push(terminal.onResize(({ cols, rows }) => {
        setRuntimeSnapshot(runtime, { dims: { cols, rows } });
        const socket = runtime.socket;
        if (socket && socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "resize", cols, rows }));
        }
      }));
      connect();
    } else if (terminal.element && terminal.element.parentElement !== container) {
      container.appendChild(terminal.element);
    }
    const focusTerminal = () => terminal.focus();
    if (runtime.active) terminal.focus();
    container.addEventListener("pointerdown", focusTerminal);

    const reconnectIfInactive = () => {
      if (runtime.status !== "disconnected" && runtime.status !== "error") return false;
      runtime.connect?.();
      return true;
    };

    const onTerminalKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Enter") return;
      if (reconnectIfInactive()) event.preventDefault();
    };
    container.addEventListener("keydown", onTerminalKeyDown);

    const resizeObserver = new ResizeObserver(() => {
      scheduleTerminalFit(terminal);
    });
    resizeObserver.observe(container);
    scheduleTerminalFit(terminal);

    const closeTerminalSession = () => {
      const socket = runtime.socket;
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "close" }));
      }
    };
    const focusTerminalOnWindowFocus = () => {
      if (runtime.active) terminal.focus();
    };
    window.addEventListener("pagehide", closeTerminalSession);
    window.addEventListener("focus", focusTerminalOnWindowFocus);

    return () => {
      if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
      if (fitFrameRef.current) window.cancelAnimationFrame(fitFrameRef.current);
      window.removeEventListener("pagehide", closeTerminalSession);
      window.removeEventListener("focus", focusTerminalOnWindowFocus);
      resizeObserver.disconnect();
      container.removeEventListener("pointerdown", focusTerminal);
      container.removeEventListener("keydown", onTerminalKeyDown);
    };
  }, [runtime, target.id]);

  const scheduleTerminalFit = (terminal: Terminal) => {
    if (fitFrameRef.current) window.cancelAnimationFrame(fitFrameRef.current);
    fitFrameRef.current = window.requestAnimationFrame(() => {
      fitFrameRef.current = null;
      fitTerminal(terminal);
    });
  };
  runtime.fit = () => {
    const terminal = runtime.terminal;
    if (!terminal) return;
    scheduleTerminalFit(terminal);
    window.setTimeout(() => scheduleTerminalFit(terminal), 80);
    window.setTimeout(() => scheduleTerminalFit(terminal), 220);
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
    const terminal = runtime.terminal;
    if (!terminal) return;
    if (fitRetryRef.current) window.clearTimeout(fitRetryRef.current);
    scheduleTerminalFit(terminal);
    fitRetryRef.current = window.setTimeout(() => scheduleTerminalFit(terminal), 120);
    if (active) terminal.focus();
  }, [active, isFullscreen, runtime, target.id]);

  useEffect(() => {
    if (!active) return;
    const onKeyDown = (event: KeyboardEvent) => {
      const inactive = runtime.status === "disconnected" || runtime.status === "error";
      if (!inactive) return;
      const targetElement = event.target as HTMLElement | null;
      if (isEditableElementOutsideTerminal(targetElement, containerRef.current)) return;
      if (event.ctrlKey && !event.altKey && !event.metaKey && keyMatches(event, "d", "KeyD")) {
        event.preventDefault();
        event.stopPropagation();
        if (!event.repeat) runtime.onClose?.();
        return;
      }
      if (event.key !== "Enter") return;
      event.preventDefault();
      runtime.connect?.();
    };
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [active, runtime, target.id]);

  return (
    <section className={`terminal-panel ${active ? "active" : "inactive"} ${isFullscreen ? "fullscreen" : ""}`} aria-hidden={!active}>
      {manualReview && sessionID && <ManualReviewPoller data={data} sessionID={sessionID} />}
      <div className="terminal-pane-toolbar">
        <button type="button" className={`terminal-ai-button icon-button ${aiEnabled ? "active" : ""}`} onClick={toggleAI} aria-pressed={aiEnabled} title={aiEnabled ? t("connectAICollaborationOn") : t("connectAICollaborationOff")}>
          <img src={aiCollaborationIcon} alt="" />
        </button>
        <button type="button" className="icon-button" onClick={() => onSplit?.("right")} title={t("connectSplitRight")}>
          <SplitSquareHorizontal />
        </button>
        <button type="button" className="icon-button" onClick={() => onSplit?.("down")} title={t("connectSplitDown")}>
          <SplitSquareVertical />
        </button>
        <button type="button" className="icon-button" onClick={() => onFullscreenChange((prev) => !prev)} aria-label={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")} title={isFullscreen ? t("connectExitFullscreen") : t("connectFullscreen")}>
          {isFullscreen ? <Minimize /> : <Maximize />}
        </button>
      </div>
      {(status === "disconnected" || status === "error") && <button type="button" className="terminal-reconnect-button" onClick={connect}><RefreshCw />{t("connectReconnect")}</button>}
      <div className="terminal-viewport" ref={containerRef} />
    </section>
  );
}

function ServerSwitcher({
  targets,
  folders,
  currentTargetID,
  openSignal,
  onOpenTarget,
}: {
  targets: Target[];
  folders: ConsoleData["targetFolders"];
  currentTargetID: string;
  openSignal: number;
  onOpenTarget: (targetID: string) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [collapsedFolders, setCollapsedFolders] = useState<Set<string>>(() => new Set());
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const currentTarget = targets.find((item) => item.id === currentTargetID);
  const currentTitle = currentTarget ? serverTitle(currentTarget) : t("connectSwitchServer");
  const folderPathByTarget = useMemo(() => Object.fromEntries(targets.map((item) => [item.id, targetFolderPath(item, folders)])), [targets, folders]);
  const filteredTargets = useMemo(() => {
    const text = query.trim().toLowerCase();
    if (!text) return targets;
    return targets.filter((item) => [
      folderPathByTarget[item.id],
      folderPathByTarget[item.id] ? `${folderPathByTarget[item.id]}/${item.alias}` : item.alias,
      folderPathByTarget[item.id] ? `${folderPathByTarget[item.id]}/${item.name}` : item.name,
      item.name,
      item.alias,
      targetEndpoint(item),
      item.remote_username,
      ...(item.tags || []),
    ].join(" ").toLowerCase().includes(text));
  }, [folderPathByTarget, query, targets]);
  const toggleFolder = (folderID: string) => {
    setCollapsedFolders((current) => {
      const next = new Set(current);
      if (next.has(folderID)) next.delete(folderID); else next.add(folderID);
      return next;
    });
  };
  const treeItems = useMemo(
    () => buildSwitcherTree(filteredTargets, folders, folderPathByTarget, query.trim() ? new Set() : collapsedFolders, toggleFolder),
    [collapsedFolders, filteredTargets, folderPathByTarget, folders, query],
  );

  useEffect(() => {
    if (openSignal <= 0) return;
    setOpen(true);
    setSelectedIndex(0);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [openSignal]);

  useEffect(() => {
    if (!open) return;
    setSelectedIndex(0);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  useEffect(() => {
    if (!treeItems.length) {
      setSelectedIndex(0);
      return;
    }
    setSelectedIndex((index) => nextSelectableSwitcherIndex(treeItems, clampNumber(index, 0, treeItems.length - 1), 1));
  }, [treeItems]);

  useEffect(() => {
    if (!open) return;
    itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
  }, [open, selectedIndex]);

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

  const onMenuKeyDown = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelectedIndex((index) => nextSelectableSwitcherIndex(treeItems, index + 1, 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelectedIndex((index) => nextSelectableSwitcherIndex(treeItems, index - 1, -1));
    } else if (event.key === "Home") {
      event.preventDefault();
      setSelectedIndex(nextSelectableSwitcherIndex(treeItems, 0, 1));
    } else if (event.key === "End") {
      event.preventDefault();
      setSelectedIndex(nextSelectableSwitcherIndex(treeItems, treeItems.length - 1, -1));
    } else if (event.key === "Enter") {
      const selected = treeItems[selectedIndex];
      if (!selected || selected.type !== "target") return;
      event.preventDefault();
      openTarget(selected.target);
    } else if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
    }
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
        <section className="server-switcher-menu" role="menu" aria-label={t("connectSwitchServer")} onKeyDown={onMenuKeyDown}>
          <label className="server-switcher-search">
            <Search />
            <input
              ref={inputRef}
              autoFocus
              data-connect-switcher-search
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("connectSwitchSearchPlaceholder")}
            />
          </label>
          <div className="server-switcher-list">
            {treeItems.map((treeItem, index) => treeItem.type === "folder" ? (
              <button
                type="button"
                key={treeItem.id}
                className="server-switcher-folder"
                style={{ "--tree-depth": treeItem.depth } as CSSProperties}
                onClick={() => treeItem.onToggle()}
              >
                {treeItem.collapsed ? <ChevronRight /> : <ChevronDown />}
                <FolderIcon />
                <strong>{treeItem.name}</strong>
              </button>
            ) : (
              <button
                type="button"
                key={treeItem.target.id}
                ref={(element) => { itemRefs.current[index] = element; }}
                className={`server-switcher-item ${treeItem.target.id === currentTargetID ? "active" : ""} ${index === selectedIndex ? "selected" : ""}`}
                style={{ "--tree-depth": treeItem.depth } as CSSProperties}
                onClick={() => openTarget(treeItem.target)}
                onPointerMove={() => setSelectedIndex(index)}
                role="menuitem"
                title={serverTitle(treeItem.target)}
              >
                <span className="server-switcher-icon">{treeItem.target.target_type === "agent" ? <Server /> : <HardDrive />}</span>
                <span className="server-switcher-main">
                  <strong>{treeItem.target.name}</strong>
                  <code>{treeItem.target.alias}</code>
                  <small>{targetEndpoint(treeItem.target)}</small>
                  <span className="server-switcher-tags">
                    {(treeItem.target.tags || []).map((tag) => (
                      <span key={tag} className={`tag-chip tag-color-${tagColor(tag, treeItem.target.tag_colors)}`}>{tag}</span>
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

function keyMatches(event: KeyboardEvent, key: string, code: string) {
  return event.key.toLowerCase() === key || event.code === code;
}

function isEditableElementOutsideTerminal(element: HTMLElement | null, terminalContainer: HTMLElement | null) {
  if (!element) return false;
  if (terminalContainer?.contains(element)) return false;
  return Boolean(element.closest("button,a,input,textarea,select,[contenteditable='true']"));
}

type SwitcherTreeItem =
  | { type: "folder"; id: string; name: string; depth: number; collapsed: boolean; onToggle: () => void }
  | { type: "target"; target: Target; depth: number };

function buildSwitcherTree(targets: Target[], folders: ConsoleData["targetFolders"], folderPathByTarget: Record<string, string>, collapsedFolders: Set<string>, onToggleFolder: (folderID: string) => void): SwitcherTreeItem[] {
  const out: SwitcherTreeItem[] = [];
  const targetIDs = new Set(targets.map((item) => item.id));
  const children = new Map<string, typeof folders>();
  for (const folder of folders) {
    const key = folder.parent_id || "";
    children.set(key, [...(children.get(key) || []), folder]);
  }
  const walkFolder = (parentID: string, depth: number) => {
    for (const folder of (children.get(parentID) || []).sort((a, b) => a.name.localeCompare(b.name))) {
      const folderPath = folderPathFromFolder(folder, folders);
      const descendants = targets.filter((target) => target.folder_id === folder.id || folderPathByTarget[target.id]?.startsWith(`${folderPath}/`));
      if (!descendants.length) continue;
      const collapsed = collapsedFolders.has(folder.id);
      out.push({ type: "folder", id: folder.id, name: folder.name, depth, collapsed, onToggle: () => onToggleFolder(folder.id) });
      if (collapsed) continue;
      for (const target of targets.filter((item) => item.folder_id === folder.id && targetIDs.has(item.id)).sort((a, b) => a.name.localeCompare(b.name))) {
        out.push({ type: "target", target, depth: depth + 1 });
      }
      walkFolder(folder.id, depth + 1);
    }
  };
  for (const target of targets.filter((item) => !item.folder_id).sort((a, b) => a.name.localeCompare(b.name))) {
    out.push({ type: "target", target, depth: 0 });
  }
  walkFolder("", 0);
  return out;
}

function nextSelectableSwitcherIndex(items: SwitcherTreeItem[], start: number, direction: 1 | -1) {
  if (!items.length) return 0;
  let index = wrapIndex(start, items.length);
  for (let i = 0; i < items.length; i += 1) {
    if (items[index]?.type === "target") return index;
    index = wrapIndex(index + direction, items.length);
  }
  return 0;
}

function FolderIcon() {
  return <Folder />;
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

function newConnectionTab(targetID: string, pendingCommand = ""): ConnectionTab {
  const pane = newTerminalPane(targetID);
  return { id: `${targetID}:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`, targetID, layout: pane, activePaneID: pane.id, pendingCommand };
}

function newPaneID(prefix: string) {
  return `${prefix}:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
}

function newTerminalPane(targetID: string): TerminalPaneNode {
  return { type: "terminal", id: newPaneID("terminal"), targetID };
}

function getTerminalRuntime(paneID: string, targetID: string): TerminalRuntime {
  const current = terminalRuntimes.get(paneID);
  if (current && current.targetID === targetID && !current.disposed) return current;
  const runtime: TerminalRuntime = {
    paneID,
    targetID,
    terminal: null,
    socket: null,
    heartbeat: null,
    status: "connecting",
    error: "",
    dims: { cols: DEFAULT_COLS, rows: DEFAULT_ROWS },
    sessionID: "",
    aiEnabled: true,
    pendingCommand: "",
    active: false,
    disposed: false,
    outputQueue: [],
    outputFrame: null,
    listeners: new Set(),
    disposables: [],
  };
  terminalRuntimes.set(paneID, runtime);
  return runtime;
}

function setRuntimeSnapshot(runtime: TerminalRuntime, patch: Partial<Pick<TerminalRuntime, "status" | "error" | "dims" | "sessionID" | "aiEnabled">>) {
  Object.assign(runtime, patch);
  for (const listener of runtime.listeners) listener();
}

function isTerminalSessionClosedOutput(data: string) {
  return data.includes("Session closed:");
}

function enqueueTerminalOutput(runtime: TerminalRuntime, terminal: Terminal, data: string) {
  if (!data || runtime.disposed) return;
  runtime.outputQueue.push(data);
  if (runtime.outputFrame !== null) return;
  runtime.outputFrame = window.requestAnimationFrame(() => flushQueuedTerminalOutput(runtime, terminal));
}

function flushQueuedTerminalOutput(runtime: TerminalRuntime, terminal: Terminal) {
  runtime.outputFrame = null;
  if (runtime.disposed || !runtime.outputQueue.length) return;
  const output = runtime.outputQueue.join("");
  runtime.outputQueue = [];
  terminal.write(output);
  if (runtime.outputQueue.length && runtime.outputFrame === null) {
    runtime.outputFrame = window.requestAnimationFrame(() => flushQueuedTerminalOutput(runtime, terminal));
  }
}

function clearQueuedTerminalOutput(runtime: TerminalRuntime) {
  runtime.outputQueue = [];
  if (runtime.outputFrame !== null) {
    window.cancelAnimationFrame(runtime.outputFrame);
    runtime.outputFrame = null;
  }
}

async function readClipboardText() {
  try {
    return await navigator.clipboard.readText();
  } catch {
    return "";
  }
}

function scheduleAllTerminalFits() {
  window.requestAnimationFrame(() => {
    for (const runtime of terminalRuntimes.values()) runtime.fit?.();
  });
}

function disposePaneRuntime(node: PaneNode | null) {
  if (!node) return;
  if (node.type === "split") {
    disposePaneRuntime(node.first);
    disposePaneRuntime(node.second);
    return;
  }
  if (node.type !== "terminal") return;
  const runtime = terminalRuntimes.get(node.id);
  if (!runtime || runtime.disposed) return;
  runtime.disposed = true;
  if (runtime.heartbeat) window.clearInterval(runtime.heartbeat);
  runtime.heartbeat = null;
  const socket = runtime.socket;
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify({ type: "close" }));
  }
  socket?.close();
  runtime.socket = null;
  clearQueuedTerminalOutput(runtime);
  for (const disposable of runtime.disposables) disposable.dispose();
  runtime.disposables = [];
  runtime.terminal?.dispose();
  runtime.terminal = null;
  terminalRuntimes.delete(node.id);
}

function findPane(node: PaneNode, paneID: string): PaneNode | null {
  if (node.id === paneID) return node;
  if (node.type !== "split") return null;
  return findPane(node.first, paneID) || findPane(node.second, paneID);
}

function splitPane(node: PaneNode, paneID: string, nextPane: PaneNode, side: PaneSide): PaneNode {
  if (node.id === paneID) {
    return {
      type: "split",
      id: newPaneID("split"),
      direction: side === "up" || side === "down" ? "column" : "row",
      ratio: 0.5,
      first: side === "left" || side === "up" ? nextPane : node,
      second: side === "left" || side === "up" ? node : nextPane,
    };
  }
  if (node.type !== "split") return node;
  return { ...node, first: splitPane(node.first, paneID, nextPane, side), second: splitPane(node.second, paneID, nextPane, side) };
}

function removePane(node: PaneNode, paneID: string): { node: PaneNode | null; activePaneID: string } {
  if (node.id === paneID) return { node: null, activePaneID: "" };
  if (node.type !== "split") return { node, activePaneID: node.id };
  const first = removePane(node.first, paneID);
  if (!first.node) return { node: node.second, activePaneID: firstLeafID(node.second) };
  const second = removePane(node.second, paneID);
  if (!second.node) return { node: first.node, activePaneID: firstLeafID(first.node) };
  return { node: { ...node, first: first.node, second: second.node }, activePaneID: first.activePaneID || second.activePaneID };
}

function resizeSplit(node: PaneNode, splitID: string, ratio: number): PaneNode {
  if (node.type !== "split") return node;
  if (node.id === splitID) return { ...node, ratio };
  return { ...node, first: resizeSplit(node.first, splitID, ratio), second: resizeSplit(node.second, splitID, ratio) };
}

function firstLeafID(node: PaneNode): string {
  if (node.type !== "split") return node.id;
  return firstLeafID(node.first);
}

function tabSelectionIncludes(tabs: ConnectionTab[], index: number, mode: "one" | "left" | "right" | "others" | "all", tabID: string) {
  if (mode === "all") return true;
  if (mode === "others") return tabs[index]?.id !== tabID;
  if (mode === "left") return tabs.slice(0, index).some((item) => item.id === tabID);
  if (mode === "right") return tabs.slice(index + 1).some((item) => item.id === tabID);
  return tabs[index]?.id === tabID;
}

function sameTargetTabCount(tabs: Array<{ tab: ConnectionTab; target: Target }>, targetID: string) {
  return tabs.filter((item) => item.target.id === targetID).length;
}

function sameTargetTabIndex(tabs: Array<{ tab: ConnectionTab; target: Target }>, targetID: string, absoluteIndex: number) {
  let count = 0;
  for (let index = 0; index <= absoluteIndex; index += 1) {
    if (tabs[index]?.target.id === targetID) count += 1;
  }
  return count;
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

function getConnectCommandParam() {
  if (typeof window === "undefined") return "";
  return new URLSearchParams(window.location.search).get("command") || "";
}

function clearConnectCommandParam() {
  if (typeof window === "undefined") return;
  const url = new URL(window.location.href);
  if (!url.searchParams.has("command")) return;
  url.searchParams.delete("command");
  window.history.replaceState(null, "", `${url.pathname}${url.search}${url.hash}`);
}

function mergeSystemSnapshot(base?: TargetSystemSnapshot, metrics?: TargetSystemSnapshot, filesystems?: TargetSystemSnapshot): TargetSystemSnapshot | undefined {
  if (!base) return undefined;
  const metricSource = metrics || base;
  return {
    ...base,
    uptime: metricSource.uptime || base.uptime,
    load: metricSource.load || base.load,
    cpu_percent: metricSource.cpu_percent,
    memory: metricSource.memory || base.memory,
    swap: metricSource.swap || base.swap,
    processes: metricSource.processes || base.processes,
    network: metricSource.network || base.network,
    filesystems: filesystems?.filesystems || base.filesystems,
    collected_at: metricSource.collected_at || base.collected_at,
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

function wrapIndex(value: number, length: number) {
  if (length <= 0) return 0;
  return (value + length) % length;
}

function serverTitle(target: Target) {
  const endpoint = targetEndpoint(target);
  const tags = (target.tags || []).join(", ");
  return [target.name, target.alias, endpoint, tags].filter(Boolean).join(" · ");
}

function targetFolderPath(target: Target, folders: ConsoleData["targetFolders"]) {
  const byID = new Map(folders.map((folder) => [folder.id, folder]));
  const names: string[] = [];
  const seen = new Set<string>();
  for (let folderID = target.folder_id || ""; folderID;) {
    if (seen.has(folderID)) break;
    seen.add(folderID);
    const folder = byID.get(folderID);
    if (!folder) break;
    names.unshift(folder.name);
    folderID = folder.parent_id || "";
  }
  return names.join("/");
}

function folderPathFromFolder(folder: ConsoleData["targetFolders"][number], folders: ConsoleData["targetFolders"]) {
  const byID = new Map(folders.map((item) => [item.id, item]));
  const names: string[] = [];
  const seen = new Set<string>();
  for (let current: typeof folder | undefined = folder; current;) {
    if (seen.has(current.id)) break;
    seen.add(current.id);
    names.unshift(current.name);
    current = current.parent_id ? byID.get(current.parent_id) : undefined;
  }
  return names.join("/");
}
