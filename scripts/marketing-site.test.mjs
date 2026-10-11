import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile, stat, mkdir } from 'node:fs/promises';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const site = path.join(root, 'site');
const pages = ['index.html', 'index.zh-CN.html'];

for (const page of pages) {
  test(page + ': static content, metadata, assets and links', async () => {
    const html = await readFile(path.join(site, page), 'utf8');
    const zh = page.includes('zh-CN');
    assert.match(html, zh ? /<html lang="zh-CN">/ : /<html lang="en">/);
    assert.equal((html.match(/<h1[ >]/g) || []).length, 1);
    assert.match(html, /<meta name="description" content=".{50,}"/);
    assert.match(html, /rel="canonical"/);
    assert.match(html, /hreflang="en"/);
    assert.match(html, /hreflang="zh-CN"/);
    assert.match(html, /<noscript>/);
    assert.match(html, /SSH exec/);
    assert.match(html, /1 GiB/);
    assert.match(html, /MCP/);
    assert.match(html, zh ? /完整 Shell 沙箱/ : /complete shell sandbox/);
    assert.match(html, zh ? /非实时连接/ : /not a live connection/);
    assert.match(html, /bastion.example.com/);
    assert.doesNotMatch(html, /fonts.googleapis|aws-ap-sg-billing-db|REINDEX|2026-06-20|没有盲目信任/);
    const ids = [...html.matchAll(/\bid="([^"]+)"/g)].map(match => match[1]);
    assert.equal(ids.length, new Set(ids).size, 'IDs must be unique');
    for (const match of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
      const value = match[1];
      if (value.startsWith('#')) assert.ok(ids.includes(value.slice(1)), 'Missing anchor ' + value);
      else if (!/^(https?:|mailto:)/.test(value)) assert.ok((await stat(path.join(site, value))).isFile(), 'Missing file ' + value);
    }
    for (const match of html.matchAll(/aria-controls="([^"]+)"/g)) assert.ok(ids.includes(match[1]), 'Missing controlled element');
    const brief = await readFile(path.join(site, zh ? 'brief.zh-CN.md' : 'brief.en.md'), 'utf8');
    assert.match(brief, zh ? /可直接使用的发布文案/ : /Ready-to-share announcement/);
    assert.match(brief, /1 GiB/);
    assert.match(brief, /22022 prod-api@bastion.example.com/);
    assert.equal((brief.match(/```/g) || []).length, 2, 'One fenced SSH example');
  });
}

test('Documentation preserves operational instructions and links to briefs', async () => {
  for (const file of ['README.md', 'README.zh-CN.md']) {
    const text = await readFile(path.join(root, file), 'utf8');
    assert.match(text, /--secret-key-path/);
    assert.match(text, /--audit-database-path/);
    assert.match(text, /1 GiB/);
    assert.match(text, /256 MiB/);
    assert.ok(text.includes('go test ./...'));
    assert.ok(text.includes('site/brief.'));
    assert.ok(!text.includes('animated terminal/replay') && !text.includes('动态终端和回放演示'));
  }
});

test('Reduced-motion support and no external font requirement', async () => {
  const css = await readFile(path.join(site, 'styles.css'), 'utf8');
  assert.match(css, /prefers-reduced-motion:reduce/);
  assert.match(css, /focus-visible/);
  assert.ok(!css.includes('@import') && !css.includes('https://'));
});

test('Chromium responsive, interaction and progressive enhancement checks', {
  skip: !process.env.GOSSHD_UI_E2E_PLAYWRIGHT || !process.env.GOSSHD_UI_E2E_BROWSER,
  timeout: 120000,
}, async () => {
  const require = createRequire(import.meta.url);
  const { chromium } = require(process.env.GOSSHD_UI_E2E_PLAYWRIGHT);
  const prefix = '/gosshd-bastion/';
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, 'http://localhost');
      if (!url.pathname.startsWith(prefix)) { response.writeHead(404); response.end(); return; }
      const name = decodeURIComponent(url.pathname.slice(prefix.length)) || 'index.html';
      const file = path.resolve(site, name);
      if (!file.startsWith(site + path.sep)) { response.writeHead(403); response.end(); return; }
      const body = await readFile(file);
      const mime = { '.html':'text/html; charset=utf-8', '.css':'text/css', '.js':'text/javascript', '.md':'text/markdown; charset=utf-8', '.svg':'image/svg+xml' }[path.extname(file)];
      response.writeHead(200, { 'Content-Type': mime || 'application/octet-stream' });
      response.end(body);
    } catch { response.writeHead(404); response.end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = 'http://127.0.0.1:' + server.address().port + prefix;
  const output = path.join(root, 'build', 'marketing-verification');
  await mkdir(output, { recursive:true });
  let browser;
  try {
    browser = await chromium.launch({ executablePath:process.env.GOSSHD_UI_E2E_BROWSER, headless:true });
    for (const file of pages) {
      const zh = file.includes('zh-CN');
      for (const width of [1440, 768, 390]) {
        const context = await browser.newContext({ viewport:{ width, height:1000 }, permissions:['clipboard-read', 'clipboard-write'], reducedMotion:'reduce' });
        const page = await context.newPage();
        const errors = [];
        page.on('pageerror', error => errors.push(error.message));
        page.on('requestfailed', request => errors.push(request.url() + ': ' + request.failure()?.errorText));
        page.on('response', response => { if (response.status() >= 400) errors.push(response.url() + ': ' + response.status()); });
        await page.goto(base + file);
        assert.equal(await page.locator('h1').count(), 1);
        assert.ok(await page.locator('h1').isVisible());
        const noOverflow = async () => assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), file + ' overflows at ' + width);
        await noOverflow();
        for (const index of [1, 2, 0]) {
          await page.locator('[data-tab="' + index + '"]').click();
          assert.equal(await page.locator('[data-tab="' + index + '"]').getAttribute('aria-selected'), 'true');
          assert.ok(await page.locator('#panel-' + index).isVisible());
          assert.equal(await page.locator('.workspace-panel:visible').count(), 1);
          await noOverflow();
        }
        await page.locator('#tab-0').focus();
        await page.keyboard.press('ArrowRight');
        assert.equal(await page.locator('#tab-1').getAttribute('aria-selected'), 'true');
        await page.keyboard.press('End');
        assert.equal(await page.locator('#tab-2').getAttribute('aria-selected'), 'true');
        await page.keyboard.press('Home');
        assert.equal(await page.locator('#tab-0').getAttribute('aria-selected'), 'true');
        await page.locator('[data-scenario="1"]').click();
        assert.equal(await page.locator('#review-command').textContent(), 'rm -rf /data');
        assert.equal(await page.locator('#review-exit').textContent(), 'exit 126');
        assert.equal(await page.locator('.review-result').getAttribute('data-state'), 'deny');
        await page.locator('[data-scenario="0"]').click();
        assert.equal(await page.locator('#review-exit').textContent(), 'exit 0');
        await page.locator('[data-copy]').click();
        await page.locator('#copy-status').filter({hasText: zh ? '已复制' : 'Copied'}).waitFor();
        assert.equal(await page.evaluate(() => navigator.clipboard.readText()), await page.locator('#ssh-example').textContent());
        const downloadPromise = page.waitForEvent('download');
        await page.locator('.brief-link').click();
        const download = await downloadPromise;
        assert.equal(download.suggestedFilename(), zh ? 'brief.zh-CN.md' : 'brief.en.md');
        assert.equal(await download.failure(), null);
        await page.locator('details').first().locator('summary').click();
        assert.equal(await page.locator('details').first().getAttribute('open'), '');
        await noOverflow();
        if (width < 901) {
          await page.locator('.mobile-menu-btn').click();
          assert.equal(await page.locator('.mobile-menu-btn').getAttribute('aria-expanded'), 'true');
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('.mobile-menu-btn').getAttribute('aria-expanded'), 'false');
          await page.locator('.mobile-menu-btn').click();
          await page.locator('.mobile-links a').first().click();
          assert.equal(await page.locator('.mobile-menu-btn').getAttribute('aria-expanded'), 'false');
        }
        await page.evaluate(() => window.scrollTo(0, 0));
        await page.screenshot({path:path.join(output, (zh ? 'zh' : 'en') + '-' + width + '.png'), fullPage:true});
        assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).scrollBehavior), 'auto');
        assert.deepEqual(errors, [], file + ' console or request errors');
        await context.close();
      }
      const plain = await browser.newContext({javaScriptEnabled:false, viewport:{width:390,height:844}});
      const page = await plain.newPage();
      await page.goto(base + file);
      assert.ok(await page.locator('h1').isVisible());
      assert.equal(await page.locator('.workspace-panel:visible').count(), 3);
      assert.ok(await page.locator('.mobile-links').isVisible());
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
      await plain.close();
    }
    const context = await browser.newContext();
    const page = await context.newPage();
    await page.goto(base + 'index.html');
    await page.evaluate(() => { Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText:async()=>{throw new Error('denied');}}}); });
    await page.locator('[data-copy]').click();
    await page.locator('#copy-status').filter({hasText:'Could not copy'}).waitFor();
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', {configurable:true,value:undefined}));
    await page.locator('[data-copy]').click();
    await page.locator('#copy-status').filter({hasText:'Copied'}).waitFor();
    await page.goto(base + '404.html');
    assert.ok(await page.locator('h1').isVisible());
    assert.ok(await page.locator('link[rel="stylesheet"]').evaluate(async el => (await fetch(el.href)).ok));
    await page.locator('a[lang="zh-CN"]').click();
    assert.equal(await page.locator('html').getAttribute('lang'), 'zh-CN');
    await context.close();
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
});
