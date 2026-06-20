import { t } from "./i18n.js";

const XTERM_CSS = "https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css";
const XTERM_JS = "https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js";

let xtermPromise = null;

export function mountTerminalReplay() {
  const root = document.querySelector("[data-terminal-replay]");
  if (!root || root.dataset.mounted === "true") return;
  root.dataset.mounted = "true";
  const lines = parseLines(root.dataset.lines || "[]");
  const events = lines.slice(1).filter((line) => Array.isArray(line) && line[1] === "o");
  const text = events.map((event) => event[2] || "").join("");
  const output = root.querySelector("[data-terminal-output]");
  const progress = root.querySelector("[data-terminal-progress]");
  const play = root.querySelector("[data-terminal-play]");
  const speed = root.querySelector("[data-terminal-speed]");
  if (!output) return;
  let terminal = null;
  let pre = null;
  let index = 0;
  let playing = false;
  let timer = 0;

  loadXTerm().then((Terminal) => {
    if (Terminal) {
      terminal = new Terminal({ convertEol: true, cursorBlink: false, fontSize: 13, theme: terminalTheme() });
      terminal.open(output);
    } else {
      pre = document.createElement("pre");
      pre.className = "terminal-fallback";
      output.appendChild(pre);
    }
    reset();
  });

  play?.addEventListener("click", () => {
    playing = !playing;
    play.textContent = playing ? t("audit.pause") : t("audit.play");
    if (playing) tick();
  });

  progress?.addEventListener("input", () => {
    const value = Number(progress.value || 0);
    reset();
    while (index < events.length && eventPercent(index) <= value) writeEvent(events[index++]);
  });

  function reset() {
    index = 0;
    window.clearTimeout(timer);
    terminal?.reset();
    if (pre) pre.textContent = "";
    if (progress) progress.value = "0";
  }

  function tick() {
    if (!playing) return;
    if (index >= events.length) {
      playing = false;
      if (play) play.textContent = t("audit.play");
      return;
    }
    writeEvent(events[index]);
    index += 1;
    if (progress) progress.value = String(eventPercent(index));
    const delay = nextDelay(index) / Number(speed?.value || 1);
    timer = window.setTimeout(tick, Math.max(8, Math.min(delay, 1200)));
  }

  function writeEvent(event) {
    const data = event?.[2] || "";
    if (terminal) terminal.write(data);
    if (pre) pre.textContent += data;
  }

  function nextDelay(nextIndex) {
    if (nextIndex <= 0 || nextIndex >= events.length) return 80;
    const previous = Number(events[nextIndex - 1]?.[0] || 0);
    const next = Number(events[nextIndex]?.[0] || previous);
    return Math.max(0, (next - previous) * 1000);
  }

  function eventPercent(nextIndex) {
    if (!events.length) return 0;
    return Math.round((nextIndex / events.length) * 100);
  }
}

function parseLines(raw) {
  try {
    return JSON.parse(raw);
  } catch {
    return [];
  }
}

function loadXTerm() {
  if (window.Terminal) return Promise.resolve(window.Terminal);
  if (!xtermPromise) {
    xtermPromise = new Promise((resolve) => {
      ensureStylesheet(XTERM_CSS);
      const script = document.createElement("script");
      script.src = XTERM_JS;
      script.async = true;
      script.onload = () => resolve(window.Terminal || null);
      script.onerror = () => resolve(null);
      document.head.appendChild(script);
    });
  }
  return xtermPromise;
}

function ensureStylesheet(href) {
  if ([...document.styleSheets].some((sheet) => sheet.href === href)) return;
  const link = document.createElement("link");
  link.rel = "stylesheet";
  link.href = href;
  document.head.appendChild(link);
}

function terminalTheme() {
  const dark = document.documentElement.dataset.theme !== "light";
  return dark
    ? { background: "#08111e", foreground: "#dbeafe", cursor: "#67e8f9" }
    : { background: "#f8fcff", foreground: "#102033", cursor: "#0ea5b7" };
}
