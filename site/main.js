const canvas = document.querySelector("#spaceCanvas");
const ctx = canvas?.getContext("2d");
const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

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
  const count = Math.max(26, Math.min(68, Math.floor((width * height) / 22000)));
  nodes = Array.from({ length: count }, (_, index) => ({
    x: ((index * 137.5) % 360) / 360 * width,
    y: (0.14 + (((index * 91.7) % 260) / 260) * 0.78) * height,
    r: 1.2 + (index % 5) * 0.36,
    vx: ((index % 7) - 3) * 0.018,
    vy: ((index % 5) - 2) * 0.014,
    hue: index % 4,
  }));
}

function nodeColor(node, alpha = 1) {
  const palette = [
    `rgba(103, 232, 249, ${alpha})`,
    `rgba(114, 242, 166, ${alpha})`,
    `rgba(255, 209, 102, ${alpha})`,
    `rgba(255, 107, 154, ${alpha})`,
  ];
  return palette[node.hue];
}

function drawScene() {
  if (!ctx) return;
  ctx.clearRect(0, 0, width, height);
  ctx.fillStyle = "#04070c";
  ctx.fillRect(0, 0, width, height);

  const centerX = width * 0.62;
  const centerY = height * 0.43;
  const radius = Math.min(width, height) * 0.32;

  ctx.save();
  ctx.globalAlpha = 0.34;
  ctx.strokeStyle = "rgba(103, 232, 249, 0.28)";
  ctx.lineWidth = 1;
  for (let ring = 1; ring <= 4; ring += 1) {
    ctx.beginPath();
    ctx.arc(centerX, centerY, (radius * ring) / 4, 0, Math.PI * 2);
    ctx.stroke();
  }
  ctx.restore();

  nodes.forEach((node, index) => {
    node.x += node.vx;
    node.y += node.vy;
    if (node.x < -20) node.x = width + 20;
    if (node.x > width + 20) node.x = -20;
    if (node.y < -20) node.y = height + 20;
    if (node.y > height + 20) node.y = -20;

    for (let next = index + 1; next < nodes.length; next += 1) {
      const other = nodes[next];
      const dx = node.x - other.x;
      const dy = node.y - other.y;
      const distance = Math.hypot(dx, dy);
      if (distance < 156) {
        ctx.strokeStyle = `rgba(142, 164, 197, ${0.2 - distance / 900})`;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(node.x, node.y);
        ctx.lineTo(other.x, other.y);
        ctx.stroke();
      }
    }

    const pulse = 0.6 + Math.sin(tick * 0.018 + index) * 0.4;
    ctx.fillStyle = nodeColor(node, 0.7 + pulse * 0.3);
    ctx.beginPath();
    ctx.arc(node.x, node.y, node.r + pulse * 0.8, 0, Math.PI * 2);
    ctx.fill();
  });

  const lanes = [
    { y: height * 0.35, color: "rgba(103, 232, 249, 0.68)" },
    { y: height * 0.48, color: "rgba(114, 242, 166, 0.58)" },
    { y: height * 0.61, color: "rgba(255, 209, 102, 0.52)" },
  ];
  lanes.forEach((lane, index) => {
    const start = width * 0.1;
    const end = width * 0.9;
    const pulse = ((tick * (1.4 + index * 0.2)) % 1000) / 1000;
    const x = start + (end - start) * pulse;
    ctx.strokeStyle = "rgba(142, 164, 197, 0.18)";
    ctx.beginPath();
    ctx.moveTo(start, lane.y);
    ctx.lineTo(end, lane.y + Math.sin(index + tick * 0.01) * 18);
    ctx.stroke();
    ctx.fillStyle = lane.color;
    ctx.beginPath();
    ctx.arc(x, lane.y + Math.sin(index + tick * 0.01) * 18, 3.6, 0, Math.PI * 2);
    ctx.fill();
  });

  if (!prefersReducedMotion) {
    tick += 1;
    window.requestAnimationFrame(drawScene);
  }
}

resizeCanvas();
window.addEventListener("resize", resizeCanvas);

document.querySelectorAll('a[href^="#"]').forEach((link) => {
  link.addEventListener("click", (event) => {
    const id = link.getAttribute("href");
    const target = id ? document.querySelector(id) : null;
    if (!target) return;
    event.preventDefault();
    target.scrollIntoView({ behavior: prefersReducedMotion ? "auto" : "smooth", block: "start" });
  });
});
