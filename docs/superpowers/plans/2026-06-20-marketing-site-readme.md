# Marketing Site And README Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the GitHub Pages site and README into a polished promotional entry point for an AI-service SSH bastion.

**Architecture:** Keep the static `site/` deployment model. Use generated raster assets in `site/assets/`, CSS/Canvas animation for motion, and static HTML sections for the product story. README becomes a high-signal marketing and quickstart page that points to the website and releases.

**Tech Stack:** Static HTML/CSS/JS, generated PNG/WebP assets, xterm-style HTML/CSS mock replay, existing GitHub Pages workflow, Markdown README files.

---

### Task 1: Promotional Assets

**Files:**
- Create: `site/assets/hero-ai-bastion.png`
- Create: `site/assets/llm-review-panel.png`

- [x] Generate a cinematic hero asset for the first viewport.
- [x] Generate or create a secondary LLM review visual.
- [x] Save both assets inside `site/assets/`.
- [x] Verify the files are local and referenced by the site.

### Task 2: Website Landing Page

**Files:**
- Modify: `site/index.html`
- Modify: `site/index.zh-CN.html`
- Modify: `site/styles.css`
- Modify: `site/main.js`

- [x] Reframe the hero around “AI service bastion”.
- [x] Add animated demo sections for SSH alias routing, xterm-style terminal replay, LLM command review, and audit timeline.
- [x] Keep bilingual navigation and locale persistence.
- [x] Respect reduced-motion preferences.
- [x] Ensure mobile layout keeps the hero and demos readable.

### Task 3: Documentation Page

**Files:**
- Modify: `site/docs.html`
- Modify: `site/docs.zh-CN.html`

- [x] Keep docs practical but more polished.
- [x] Add links back to the promotional demos.
- [x] Keep install, private node, policies, LLM review, and audit sections.

### Task 4: README Rewrite

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [x] Rewrite the top half as a promotional open-source project page.
- [x] Include website link, release link, quickstart, feature grid, and use cases.
- [x] Keep operational details available without making the README feel like a raw checklist.

### Task 5: Verification And Commit

**Files:**
- Test: `site/index.html`
- Test: `site/docs.html`
- Test: `README.md`

- [x] Run static file checks for referenced assets.
- [x] Serve `site/` locally and inspect with Playwright screenshots on desktop and mobile.
- [x] Run `git diff --check`.
- [x] Commit the marketing site and README changes.
