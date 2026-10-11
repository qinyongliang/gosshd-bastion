/* Static-first marketing interactions. No connections to live targets. */
(() => {
  'use strict';
  document.documentElement.classList.add('js');
  const zh = document.documentElement.lang === 'zh-CN';
  const menuButton = document.querySelector('.mobile-menu-btn');
  const menu = document.querySelector('.mobile-links');
  const setMenu = (open) => {
    menu.classList.toggle('open', open);
    menuButton.setAttribute('aria-expanded', String(open));
  };
  if (menuButton && menu) {
    menuButton.addEventListener('click', () => setMenu(menuButton.getAttribute('aria-expanded') !== 'true'));
    menu.addEventListener('click', (event) => { if (event.target.closest('a')) setMenu(false); });
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && menuButton.getAttribute('aria-expanded') === 'true') {
        setMenu(false);
        menuButton.focus();
      }
    });
    document.addEventListener('click', (event) => {
      if (!event.target.closest('.nav')) setMenu(false);
    });
    window.addEventListener('resize', () => { if (window.innerWidth > 900) setMenu(false); });
  }
  const tabs = Array.from(document.querySelectorAll('[data-tab]'));
  const panels = Array.from(document.querySelectorAll('.workspace-panel'));
  const tabList = document.querySelector('.workspace-tabs');
  if (tabList) tabList.setAttribute('role', 'tablist');
  const selectTab = (index, focus = false) => {
    tabs.forEach((tab, i) => {
      const selected = i === index;
      tab.classList.toggle('selected', selected);
      tab.setAttribute('aria-selected', String(selected));
      tab.tabIndex = selected ? 0 : -1;
      panels[i].hidden = !selected;
    });
    if (focus) tabs[index].focus();
  };
  tabs.forEach((tab, index) => {
    tab.setAttribute('role', 'tab');
    panels[index].setAttribute('role', 'tabpanel');
    panels[index].tabIndex = 0;
    tab.addEventListener('click', () => selectTab(index));
    tab.addEventListener('keydown', (event) => {
      let next;
      if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
      if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length;
      if (event.key === 'Home') next = 0;
      if (event.key === 'End') next = tabs.length - 1;
      if (next !== undefined) { event.preventDefault(); selectTab(next, true); }
    });
  });
  if (tabs.length) selectTab(0);
  const scenarios = zh ? [
    ['uptime', '允许 · 白名单规则', '命中只读命令白名单，转发到目标。'],
    ['rm -rf /data', '拒绝 · 黑名单规则', '命中删除命令黑名单，不转发到目标。'],
  ] : [
    ['uptime', 'Allowed · allowlist rule', 'Read-only allowlist matched. Forward to target.'],
    ['rm -rf /data', 'Denied · blocklist rule', 'Deletion blocklist matched. Do not forward.'],
  ];
  const scenarioButtons = Array.from(document.querySelectorAll('[data-scenario]'));
  scenarioButtons.forEach((button, index) => {
    button.addEventListener('click', () => {
      scenarioButtons.forEach((item, i) => item.setAttribute('aria-pressed', String(i === index)));
      const [command, decision, reason] = scenarios[index];
      document.getElementById('review-command').textContent = command;
      document.getElementById('review-decision').textContent = decision;
      document.getElementById('review-reason').textContent = reason;
      document.getElementById('review-exit').textContent = index === 0 ? 'exit 0' : 'exit 126';
      const result = document.querySelector('.review-result');
      result.dataset.state = index === 0 ? 'allow' : 'deny';
      result.querySelector('.result-icon').textContent = index === 0 ? '✓' : '×';
    });
  });
  const copyButton = document.querySelector('[data-copy]');
  if (copyButton) {
    copyButton.addEventListener('click', async () => {
      const text = document.getElementById(copyButton.dataset.copy).textContent;
      const status = document.getElementById('copy-status');
      try {
        if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(text);
        else {
          const field = document.createElement('textarea');
          field.value = text;
          field.style.position = 'fixed';
          field.style.opacity = '0';
          document.body.append(field);
          let copied;
          try { field.select(); copied = document.execCommand('copy'); }
          finally { field.remove(); copyButton.focus(); }
          if (!copied) throw new Error('Clipboard unavailable');
        }
        status.textContent = zh ? '已复制' : 'Copied';
      } catch {
        status.textContent = zh ? '复制失败，请手动选择命令' : 'Could not copy. Please select the command manually.';
      }
    });
  }
})();
