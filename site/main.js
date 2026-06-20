const localeStorageKey = "gosshd_locale";
const canvas = document.querySelector("#spaceCanvas");
const ctx = canvas?.getContext("2d");
const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

applyLocaleRouting();
bindLanguageLinks();
bindSmoothAnchors();
activateReveal();
playTerminalReplays();

let width = 0;
let height = 0;
let dpr = 1;
let nodes = [];
let tick = 0;

function resizeCanvas() {
  if (!canvas || !ctx) return;
  dpr = Math.min(window.devicePixelRatio || 1, 2);
  width = window.innerWidth;
  height = window.innerHeight;
  canvas.width = Math.floor(width * dpr);
  canvas.height = Math.floor(height * dpr);
  canvas.style.width = `${width}px`;
  canvas.style.height = `${height}px`;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  seedNodes();
  drawScene();
}

function seedNodes() {
  const count = Math.max(28, Math.min(74, Math.floor((width * height) / 21000)));
  nodes = Array.from({ length: count }, (_, index) => ({
    x: ((index * 137.5) % 360) / 360 * width,
    y: (0.12 + (((index * 91.7) % 280) / 280) * 0.78) * height,
    r: 1.1 + (index % 5) * 0.38,
    vx: ((index % 7) - 3) * 0.022,
    vy: ((index % 5) - 2) * 0.015,
    hue: index % 5,
  }));
}

function nodeColor(node, alpha = 1) {
  const palette = [
    `rgba(103, 232, 249, ${alpha})`,
    `rgba(118, 242, 174, ${alpha})`,
    `rgba(255, 209, 102, ${alpha})`,
    `rgba(255, 107, 154, ${alpha})`,
    `rgba(185, 164, 255, ${alpha})`,
  ];
  return palette[node.hue];
}

function drawScene() {
  if (!ctx) return;
  ctx.clearRect(0, 0, width, height);

  nodes.forEach((node, index) => {
    node.x += node.vx;
    node.y += node.vy;
    if (node.x < -30) node.x = width + 30;
    if (node.x > width + 30) node.x = -30;
    if (node.y < -30) node.y = height + 30;
    if (node.y > height + 30) node.y = -30;

    for (let next = index + 1; next < nodes.length; next += 1) {
      const other = nodes[next];
      const dx = node.x - other.x;
      const dy = node.y - other.y;
      const distance = Math.hypot(dx, dy);
      if (distance < 150) {
        ctx.strokeStyle = `rgba(142, 164, 197, ${0.2 - distance / 880})`;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(node.x, node.y);
        ctx.lineTo(other.x, other.y);
        ctx.stroke();
      }
    }

    const pulse = 0.62 + Math.sin(tick * 0.019 + index) * 0.38;
    ctx.fillStyle = nodeColor(node, 0.58 + pulse * 0.32);
    ctx.beginPath();
    ctx.arc(node.x, node.y, node.r + pulse * 0.78, 0, Math.PI * 2);
    ctx.fill();
  });

  drawTrafficLanes();

  if (!prefersReducedMotion) {
    tick += 1;
    window.requestAnimationFrame(drawScene);
  }
}

function drawTrafficLanes() {
  const lanes = [
    { y: height * 0.22, color: "rgba(103, 232, 249, 0.72)", speed: 1.25 },
    { y: height * 0.58, color: "rgba(118, 242, 174, 0.62)", speed: 1.06 },
    { y: height * 0.76, color: "rgba(255, 209, 102, 0.52)", speed: 0.82 },
  ];

  lanes.forEach((lane, index) => {
    const start = -60;
    const end = width + 60;
    const pulse = ((tick * lane.speed + index * 180) % 1000) / 1000;
    const x = start + (end - start) * pulse;
    const wave = Math.sin(index + tick * 0.012) * 20;
    ctx.strokeStyle = "rgba(142, 164, 197, 0.16)";
    ctx.beginPath();
    ctx.moveTo(start, lane.y);
    ctx.bezierCurveTo(width * 0.28, lane.y + wave, width * 0.68, lane.y - wave, end, lane.y);
    ctx.stroke();
    ctx.fillStyle = lane.color;
    ctx.beginPath();
    ctx.arc(x, lane.y + Math.sin(pulse * Math.PI) * wave, 3.6, 0, Math.PI * 2);
    ctx.fill();
  });
}

resizeCanvas();
window.addEventListener("resize", resizeCanvas);

function bindSmoothAnchors() {
  document.querySelectorAll('a[href^="#"]').forEach((link) => {
    link.addEventListener("click", (event) => {
      const id = link.getAttribute("href");
      const target = id ? document.querySelector(id) : null;
      if (!target) return;
      event.preventDefault();
      target.scrollIntoView({ behavior: prefersReducedMotion ? "auto" : "smooth", block: "start" });
    });
  });
}

function activateReveal() {
  const items = Array.from(document.querySelectorAll(".reveal"));
  if (!items.length) return;
  if (prefersReducedMotion || !("IntersectionObserver" in window)) {
    items.forEach((item) => item.classList.add("is-visible"));
    return;
  }

  const observer = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        entry.target.classList.add("is-visible");
        observer.unobserve(entry.target);
      });
    },
    { threshold: 0.18 }
  );
  items.forEach((item) => observer.observe(item));
}

function playTerminalReplays() {
  const terminals = Array.from(document.querySelectorAll("[data-terminal-replay]"));
  terminals.forEach((terminal) => {
    const player = createTerminalReplay(terminal);
    if (prefersReducedMotion || !("IntersectionObserver" in window)) {
      player.renderFinalState();
      return;
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        player.start();
        observer.disconnect();
      },
      { threshold: 0.32 }
    );
    observer.observe(terminal);
  });
}

function createTerminalReplay(root) {
  const prompt = "mia@bastion:~$";
  const events = [
    { at: "00:00.000", type: "type", prompt, text: "ssh -p 22022 inference-gpu@bastion.example.com", speed: 28 },
    { at: "00:00.924", type: "output", label: "gosshd", text: "public key accepted for mia@ops" },
    { at: "00:01.188", type: "output", label: "route", text: "alias inference-gpu -> private node / ai-rack-07" },
    { at: "00:01.612", type: "output", label: "policy", text: "readonly-production matched by user group" },
    { at: "00:02.304", type: "type", prompt, text: "docker ps --format '{{.Names}} {{.Status}}' | head", speed: 34 },
    { at: "00:03.741", type: "output", label: "llm", text: "allow, read-only container inspection (1.4s)" },
    { at: "00:04.118", type: "output", label: "docker", text: "redis-vector     Up 18 hours" },
    { at: "00:04.286", type: "output", label: "docker", text: "model-gateway    Up 18 hours (healthy)" },
    { at: "00:04.554", type: "output", label: "audit", text: "session sealed, replay frames compressed" },
  ];
  const timeline = root.closest(".xterm-stage")?.querySelectorAll(".replay-timeline i") || [];
  const timers = [];
  let running = false;

  function clearTimers() {
    while (timers.length) window.clearTimeout(timers.pop());
  }

  function reset() {
    clearTimers();
    root.replaceChildren();
    timeline.forEach((item) => {
      item.classList.remove("is-done", "is-active");
      item.style.removeProperty("--terminal-progress-delay");
    });
  }

  function appendLine(event, options = {}) {
    const line = document.createElement("div");
    line.className = `terminal-line ${event.type === "type" ? "is-command" : "is-output"}`;
    const time = document.createElement("span");
    time.className = "terminal-time";
    time.textContent = event.at;
    const label = document.createElement("span");
    label.className = "terminal-label";
    label.textContent = event.type === "type" ? event.prompt : event.label;
    const text = document.createElement("span");
    text.className = "terminal-text";
    const cursor = document.createElement("span");
    cursor.className = "terminal-cursor";
    line.append(time, label, text);
    if (options.cursor) line.append(cursor);
    root.append(line);
    window.requestAnimationFrame(() => line.classList.add("is-visible"));
    root.scrollTop = root.scrollHeight;
    return { line, text, cursor };
  }

  function typeText(target, text, speed, done) {
    let index = 0;
    const writeNext = () => {
      target.textContent = text.slice(0, index);
      index += 1;
      if (index <= text.length) {
        timers.push(window.setTimeout(writeNext, speed));
        return;
      }
      done?.();
    };
    writeNext();
  }

  function markTimeline(index, state) {
    const segment = timeline[Math.min(index, timeline.length - 1)];
    if (!segment) return;
    segment.classList.toggle("is-active", state === "active");
    segment.classList.toggle("is-done", state === "done");
  }

  function renderFinalState() {
    reset();
    events.forEach((event, index) => {
      const { text } = appendLine(event);
      text.textContent = event.text;
      markTimeline(index, "done");
    });
  }

  function start() {
    if (running) return;
    running = true;
    runOnce();
  }

  function runOnce() {
    reset();
    let delay = 240;
    events.forEach((event, index) => {
      timers.push(
        window.setTimeout(() => {
          markTimeline(index, "active");
          if (event.type === "type") {
            const { text, cursor } = appendLine(event, { cursor: true });
            typeText(text, event.text, event.speed, () => {
              cursor.remove();
              markTimeline(index, "done");
            });
            return;
          }
          const { line, text } = appendLine(event);
          text.textContent = event.text;
          markTimeline(index, "done");
        }, delay)
      );
      delay += event.type === "type" ? event.text.length * event.speed + 420 : 520;
    });
    timers.push(window.setTimeout(runOnce, delay + 2200));
  }

  return { renderFinalState, start };
}

function applyLocaleRouting() {
  const page = document.body?.dataset.page;
  if (!page || !["home", "docs"].includes(page)) return;
  const desired = storedLocale() || browserLocale();
  const current = document.documentElement.lang === "zh-CN" ? "zh-CN" : "en";
  if (desired === current) return;
  const target = localizedPath(page, desired);
  if (!target) return;
  window.location.replace(`${target}${window.location.hash || ""}`);
}

function bindLanguageLinks() {
  document.querySelectorAll("[data-locale]").forEach((link) => {
    link.addEventListener("click", () => {
      const locale = normalizeLocale(link.getAttribute("data-locale"));
      if (locale) writeLocale(locale);
      const href = link.getAttribute("href") || "";
      if (window.location.hash && !href.includes("#")) {
        link.setAttribute("href", `${href}${window.location.hash}`);
      }
    });
  });
}

function localizedPath(page, locale) {
  if (page === "docs") return locale === "zh-CN" ? "./docs.zh-CN.html" : "./docs.html";
  return locale === "zh-CN" ? "./index.zh-CN.html" : "./index.html";
}

function browserLocale() {
  const languages = navigator.languages?.length ? navigator.languages : [navigator.language];
  return languages.some((language) => normalizeLocale(language) === "zh-CN") ? "zh-CN" : "en";
}

function storedLocale() {
  try {
    return normalizeLocale(window.localStorage.getItem(localeStorageKey));
  } catch {
    return "";
  }
}

function writeLocale(locale) {
  try {
    window.localStorage.setItem(localeStorageKey, locale);
  } catch {
    // Language navigation still works when storage is unavailable.
  }
}

function normalizeLocale(value) {
  const text = String(value || "").trim().toLowerCase();
  if (text === "zh-cn" || text.startsWith("zh")) return "zh-CN";
  if (text === "en" || text.startsWith("en-")) return "en";
  return "";
}
