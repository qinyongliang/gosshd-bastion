const localeStorageKey = "gosshd_locale";
const canvas = document.querySelector("#spaceCanvas");
const ctx = canvas?.getContext("2d");
const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

applyLocaleRouting();
bindLanguageLinks();
bindSmoothAnchors();
activateReveal();
playTerminalCast();

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

function playTerminalCast() {
  const terminals = Array.from(document.querySelectorAll("[data-terminal]"));
  terminals.forEach((terminal) => {
    const lines = Array.from(terminal.querySelectorAll("p"));
    const reveal = () => {
      lines.forEach((line) => line.classList.remove("is-visible"));
      lines.forEach((line, index) => {
        window.setTimeout(() => line.classList.add("is-visible"), prefersReducedMotion ? 0 : 420 * index);
      });
    };
    reveal();
    if (!prefersReducedMotion) window.setInterval(reveal, 7000);
  });
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
