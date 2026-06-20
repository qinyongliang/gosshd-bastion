/* GOSSHD Bastion — Interactions
 * Handles reveal-on-scroll, terminal typing, LLM review scenario,
 * replay playback, mobile nav, and ambient motion.
 */

(function () {
  'use strict';

  /* ---------- Utilities ---------- */
  const $ = (sel, ctx = document) => ctx.querySelector(sel);
  const $$ = (sel, ctx = document) => Array.from(ctx.querySelectorAll(sel));
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  /* ---------- Nav scroll state ---------- */
  const nav = $('.nav');
  function updateNav() {
    if (!nav) return;
    nav.classList.toggle('scrolled', window.scrollY > 20);
  }
  window.addEventListener('scroll', updateNav, { passive: true });
  updateNav();

  /* ---------- Mobile menu ---------- */
  const mobileBtn = $('.mobile-menu-btn');
  const mobileLinks = $('.mobile-links');
  if (mobileBtn && mobileLinks) {
    mobileBtn.addEventListener('click', () => {
      const open = mobileLinks.style.display === 'flex';
      mobileLinks.style.display = open ? 'none' : 'flex';
    });
    mobileLinks.querySelectorAll('a').forEach((a) =>
      a.addEventListener('click', () => (mobileLinks.style.display = 'none'))
    );
  }

  /* ---------- Reveal on scroll ---------- */
  const revealObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add('in-view');
          revealObserver.unobserve(entry.target);
        }
      });
    },
    { threshold: 0.12, rootMargin: '0px 0px -40px 0px' }
  );
  $$('.reveal').forEach((el) => revealObserver.observe(el));

  /* ---------- Typewriter terminal ---------- */
  class Typewriter {
    constructor(el, lines, options = {}) {
      this.el = el;
      this.lines = lines;
      this.options = options;
      this.speed = options.speed || 22;
      this.delay = options.delay || 600;
      this.loop = options.loop !== false;
      this.running = false;
      this.body = el.querySelector('.terminal-body') || el;
      this.cursor = document.createElement('span');
      this.cursor.className = 'term-cursor';
    }

    async start() {
      if (this.running) return;
      this.running = true;
      while (this.running) {
        this.body.innerHTML = '';
        if (typeof this.options.onReset === 'function') {
          this.options.onReset();
        }
        for (const line of this.lines) {
          if (!this.running) return;
          await this.typeLine(line);
          await sleep(this.delay * 0.6);
        }
        await sleep(this.delay * 3);
        if (!this.loop) break;
      }
    }

    async typeLine(line) {
      const div = document.createElement('span');
      div.className = 'terminal-line';
      if (line.className) div.className += ' ' + line.className;
      this.body.appendChild(div);
      const text = line.text || line;
      let i = 0;
      div.appendChild(this.cursor);
      while (i < text.length) {
        if (!this.running) return;
        const chunk = text.slice(0, i + 1);
        div.innerHTML = chunk;
        div.appendChild(this.cursor);
        i++;
        const isLong = text.length > 80;
        await sleep(isLong ? this.speed * 0.35 : this.speed);
      }
      div.innerHTML = text;
      if (typeof this.options.onLine === 'function') {
        this.options.onLine(line, div);
      }
    }

    stop() { this.running = false; }
  }

  /* ---------- Hero terminal ---------- */
  const heroTerminal = $('#hero-terminal');
  if (heroTerminal) {
    const heroRouteSteps = $$('#hero-route .route-step');
    const showRoute = (idx) => {
      heroRouteSteps.forEach((step, stepIdx) => {
        step.classList.toggle('active', stepIdx === idx);
      });
    };
    const heroLines = [
      { text: '$ ssh aws-ap-sg-billing-db@gosshd.site "psql -c \'select now();\'"', className: '', route: 1 },
      { text: '              now', className: 'term-dim', route: 2 },
      { text: '-------------------------------', className: 'term-dim', route: 2 },
      { text: '2026-06-20 09:14:02.104+00', className: 'term-ok', route: 2 },
      { text: 'exit status 0', className: 'term-ok', route: 2 },
      { text: '$ ssh aws-ap-sg-billing-db@gosshd.site "sudo rm -rf /data"', className: 'term-warn', route: 1 },
      { text: 'command denied: high-risk command blocked by policy', className: 'term-danger', route: 3 },
      { text: 'exit status 126', className: 'term-danger', route: 3 },
    ];
    const tw = new Typewriter(heroTerminal, heroLines, {
      speed: 28,
      delay: 900,
      onReset: () => showRoute(0),
      onLine: (line) => {
        if (typeof line.route === 'number') showRoute(line.route);
      },
    });
    tw.start();
  }

  /* ---------- Review scenario ---------- */
  const reviewTerminal = $('#review-terminal');
  const judgmentCards = $$('.judgment-card');
  if (reviewTerminal && judgmentCards.length) {
    const scenarioLines = [
      { text: '$ ssh aws-ap-sg-billing-db@gosshd.site "psql -c \'REINDEX DATABASE production;\'"', className: '' },
      { text: 'REINDEX', className: 'term-dim' },
      { text: 'exit status 0', className: 'term-ok' },
      { text: '$ ssh aws-ap-sg-billing-db@gosshd.site "psql -c \'DROP TABLE customers;\'"', className: 'term-warn' },
      { text: 'command denied: destructive schema change requires approval', className: 'term-danger' },
      { text: 'exit status 126', className: 'term-danger' },
    ];
    const reviewTw = new Typewriter(reviewTerminal, scenarioLines, { speed: 26, delay: 700, loop: false });

    // Map scenario progress to judgment cards
    const triggerIndex = [1, 3, 4];
    const originalTypeLine = reviewTw.typeLine.bind(reviewTw);
    let lineIndex = 0;
    reviewTw.typeLine = async function (line) {
      await originalTypeLine(line);
      const idx = triggerIndex.indexOf(lineIndex);
      if (idx !== -1) {
        judgmentCards.forEach((c, i) => c.classList.toggle('active', i === idx));
      }
      lineIndex++;
    };

    const reviewObserver = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            reviewTw.start();
            reviewObserver.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.35 }
    );
    reviewObserver.observe(reviewTerminal);
  }

  /* ---------- Replay terminal ---------- */
  const replayTerminal = $('#replay-terminal');
  const replayProgress = $('.replay-progress-bar');
  const replayTime = $('.replay-time');
  const replayPlay = $('.replay-play');
  const replayPause = $('.replay-pause');

  if (replayTerminal) {
    const sessionEvents = [
      { t: 0, text: '$ ssh aliyun-hz-order-api@gosshd.site "kubectl get pods -n production"', className: '' },
      { t: 900, text: 'NAME                        READY   STATUS', className: 'term-dim' },
      { t: 1300, text: 'order-api-7d9f4b8c5-x2v9q   1/1     Running', className: 'term-dim' },
      { t: 2000, text: 'exit status 0', className: 'term-ok' },
      { t: 2800, text: '$ ssh aliyun-hz-order-api@gosshd.site "kubectl logs order-api-7d9f4b8c5-x2v9q -n production --tail=2"', className: 'term-cmd' },
      { t: 4300, text: '2026/06/20 09:18:02 request_id=cf19 path=/health', className: 'term-dim' },
      { t: 5400, text: '2026/06/20 09:18:05 request_id=cf20 status=200', className: 'term-dim' },
      { t: 6500, text: 'exit status 0', className: 'term-ok' },
      { t: 7400, text: '$ ssh aliyun-hz-order-api@gosshd.site "kubectl delete ns production"', className: 'term-warn' },
      { t: 8500, text: 'command denied: destructive Kubernetes operation blocked', className: 'term-danger' },
      { t: 9000, text: 'exit status 126', className: 'term-danger' },
    ];

    const body = replayTerminal.querySelector('.terminal-body') || replayTerminal;
    const duration = sessionEvents[sessionEvents.length - 1].t + 1600;
    let replayReq;
    let startAt = 0;
    let started = false;
    let paused = true;

    function resetReplay() {
      body.innerHTML = '';
      startAt = performance.now();
      sessionEvents.forEach((ev) => (ev.fired = false));
      if (replayProgress) replayProgress.style.width = '0%';
      if (replayTime) replayTime.textContent = formatTime(0) + ' / ' + formatTime(duration);
    }

    function formatTime(ms) {
      const s = Math.max(0, Math.floor(ms / 1000));
      const m = Math.floor(s / 60);
      const rs = s % 60;
      return `${String(m).padStart(2, '0')}:${String(rs).padStart(2, '0')}`;
    }

    function renderEvent(ev) {
      const line = document.createElement('span');
      line.className = 'terminal-line';
      if (ev.className) line.className += ' ' + ev.className;
      line.textContent = ev.text;
      body.appendChild(line);
      body.scrollTop = body.scrollHeight;
    }

    function frame(now) {
      const elapsed = now - startAt;
      if (replayProgress) replayProgress.style.width = Math.min(100, (elapsed / duration) * 100) + '%';
      if (replayTime) replayTime.textContent = `${formatTime(elapsed)} / ${formatTime(duration)}`;

      sessionEvents.forEach((ev) => {
        if (!ev.fired && elapsed >= ev.t) {
          ev.fired = true;
          renderEvent(ev);
        }
      });

      if (elapsed < duration && !paused) {
        replayReq = requestAnimationFrame(frame);
      } else if (elapsed >= duration) {
        paused = true;
        updatePlayIcons();
      }
    }

    function updatePlayIcons() {
      if (replayPlay && replayPause) {
        replayPlay.style.display = paused ? 'grid' : 'none';
        replayPause.style.display = paused ? 'none' : 'grid';
      }
    }

    function play() {
      if (!started) resetReplay();
      started = true;
      paused = false;
      // Adjust startAt to account for pause offset
      startAt = performance.now() - (parseFloat(replayProgress?.style.width || 0) / 100) * duration;
      replayReq = requestAnimationFrame(frame);
      updatePlayIcons();
    }

    function pause() {
      paused = true;
      cancelAnimationFrame(replayReq);
      updatePlayIcons();
    }

    if (replayPlay) replayPlay.addEventListener('click', play);
    if (replayPause) replayPause.addEventListener('click', pause);

    // Auto-start replay when in view
    resetReplay();
    const replayObserver = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting && paused && !started) {
            play();
          }
        });
      },
      { threshold: 0.45 }
    );
    replayObserver.observe(replayTerminal);
  }

  /* ---------- Smooth anchor offset for fixed nav ---------- */
  document.querySelectorAll('a[href^="#"]').forEach((a) => {
    a.addEventListener('click', (e) => {
      const id = a.getAttribute('href').slice(1);
      const target = document.getElementById(id);
      if (target) {
        e.preventDefault();
        const top = target.getBoundingClientRect().top + window.scrollY - 80;
        window.scrollTo({ top, behavior: 'smooth' });
      }
    });
  });
})();
