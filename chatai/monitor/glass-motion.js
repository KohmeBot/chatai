// Original, dependency-free motion for the embedded console.
// References: rdev/liquid-glass-react (specular light / elastic response),
// motiondivision/motion (interruptible transforms / staggered entrances).
window.ConsoleMotion = (() => {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const fine = matchMedia('(hover: hover) and (pointer: fine)');
  const active = new Set();
  const entrances = new WeakMap();
  const counts = new Map();
  const dialogs = new WeakSet();
  let graphNodes = new Set();
  let previousRuns = new Set(), detailID = '';
  const ease = 'cubic-bezier(.16, 1, .3, 1)';
  const glowSurfaces = '.stat, .resource-card, .glass-segments, .login, .run-metrics>div, .overview-result, .overview-output, .schema-field, .execution-record, .memory-item, .detail-fields';
  const segmentInstances = new Map();

  // One lens component for navigation, filters and dynamically mounted tabs.
  // Selection stays owned by the caller; this component only presents it.
  function mountSegments(root) {
    if (!root || segmentInstances.has(root)) return;
    root.classList.add('glass-segments');
    const lens = document.createElement('span');
    lens.className = 'glass-lens';
    lens.setAttribute('aria-hidden', 'true');
    root.append(lens);
    let frame = 0;
    const position = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const buttons = [...root.querySelectorAll(':scope > button')];
        const selected = buttons.find(b => b.matches('.nav-active,[aria-selected="true"],[aria-pressed="true"]'));
        if (!selected || !root.offsetWidth) return;
        lens.style.width = `${selected.offsetWidth}px`;
        lens.style.height = `${selected.offsetHeight}px`;
        lens.style.transform = `translate(${selected.offsetLeft}px, ${selected.offsetTop}px)`;
        if (root.id === 'nav') for (const button of buttons) {
          if (button === selected) button.setAttribute('aria-current', 'page');
          else button.removeAttribute('aria-current');
        }
        root.classList.add('motion-ready');
      });
    };
    const resize = new ResizeObserver(position);
    resize.observe(root);
    root.querySelectorAll('button').forEach(button => resize.observe(button));
    const selection = new MutationObserver(records => {
      if (records.some(record => record.target.tagName === 'BUTTON')) position();
    });
    selection.observe(root, { subtree: true, attributes: true, attributeFilter: ['class', 'aria-selected', 'aria-pressed'] });
    const keyboard = event => {
      if (root.getAttribute('role') === 'tablist') return; // Tab owner supplies roving focus.
      const buttons = [...root.querySelectorAll(':scope > button:not(:disabled)')];
      const index = buttons.indexOf(event.target);
      if (index < 0) return;
      const next = event.key === 'ArrowRight' ? (index + 1) % buttons.length : event.key === 'ArrowLeft' ? (index + buttons.length - 1) % buttons.length : event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : -1;
      if (next < 0) return;
      event.preventDefault(); buttons[next].focus(); buttons[next].click();
    };
    root.addEventListener('keydown', keyboard);
    document.fonts?.ready.then(position);
    position();
    segmentInstances.set(root, () => {
      cancelAnimationFrame(frame); resize.disconnect(); selection.disconnect();
      root.removeEventListener('keydown', keyboard); segmentInstances.delete(root);
    });
  }

  // Every modal has one fixed heading and exactly one scrollable content area.
  function prepareDialog(dialog) {
    if (dialog.classList.contains('glass-dialog')) return;
    const heading = dialog.querySelector('h2');
    if (!heading) return;
    let head = heading.closest('.row, .run-dialog-toolbar');
    if (!head || head === dialog) {
      head = document.createElement('div'); head.append(heading); dialog.prepend(head);
    }
    head.classList.add('dialog-heading');
    let close = head.querySelector('button');
    if (!close) { close = document.createElement('button'); close.type = 'button'; head.append(close); }
    close.classList.add('dialog-close');
    close.textContent = '×'; close.setAttribute('aria-label', '关闭弹窗'); close.title = '关闭';
    // Route the header button through the same cancellation guard as Escape.
    close.onclick = () => {
      if (dialog.dispatchEvent(new Event('cancel', { cancelable: true }))) closeDialog(dialog);
    };
    const content = [...dialog.children].filter(node => node !== head);
    let body;
    if (content.length === 1 && content[0].matches('form, .resource-body, #detail')) body = content[0];
    else { body = document.createElement('div'); body.append(...content); dialog.append(body); }
    body.classList.add('dialog-body');
    dialog.classList.add('glass-dialog');
  }

  function animate(node, frames, options = {}) {
    if (!node || reduced.matches || !node.animate) return null;
    const animation = node.animate(frames, { duration: 420, easing: ease, ...options });
    active.add(animation);
    animation.finished.catch(() => {}).finally(() => active.delete(animation));
    return animation;
  }
  reduced.addEventListener('change', () => {
    if (reduced.matches) for (const animation of active) animation.cancel();
  });
  function reveal(node, delay = 0, distance = 12) {
    entrances.get(node)?.cancel();
    const animation = animate(node, [
      { opacity: 0, transform: `translateY(${distance}px) scale(.988)` },
      { opacity: 1, transform: 'translateY(0) scale(1)' }
    ], { delay, fill: 'backwards', duration: 480 });
    if (animation) entrances.set(node, animation);
  }
  function view(node) {
    reveal(node, 0, 10);
    if (node.id === 'run-view') {
      node.querySelectorAll('.stat').forEach((card, i) => reveal(card, 45 + i * 45));
    }
  }
  function catalog(root) {
    // Only explicit navigation/reloads animate; polling never replays the grid.
    [...root.querySelectorAll('.resource-card')].slice(0, 16)
      .forEach((card, i) => reveal(card, Math.min(i * 35, 210)));
  }
  function runs(root) {
    const next = new Set();
    let index = 0;
    for (const row of root.querySelectorAll('[data-run-id]')) {
      next.add(row.dataset.runId);
      if (!previousRuns.has(row.dataset.runId) && index < 12) reveal(row, index++ * 28, 8);
    }
    previousRuns = next;
  }
  function detail(root, id) {
    if (id !== detailID) {
      graphNodes.clear();
      reveal(root.querySelector('.detail-body'), 0, 8);
    }
    let index = 0;
    for (const node of root.querySelectorAll('.dag-node')) {
      if (!graphNodes.has(node.dataset.node) && index < 15) reveal(node, index++ * 30, 7);
      graphNodes.add(node.dataset.node);
    }
    detailID = id;
  }
  function stat(node, value) {
    if (node.textContent !== value) node.textContent = value;
    if (counts.has(node.id) && counts.get(node.id) !== value) {
      reveal(node, 0, 4);
    }
    counts.set(node.id, value);
  }
  function reset() { previousRuns.clear(); graphNodes.clear(); detailID = ''; counts.clear(); }

  async function busy(button, work) {
    if (button.disabled) return;
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    try { return await work(); }
    finally { button.disabled = false; button.removeAttribute('aria-busy'); }
  }
  function closeDialog(dialog) {
    if (!dialog.open || dialog.dataset.closing) return;
    dialog.dataset.closing = 'true';
    dialog.inert = true;
    const animation = animate(dialog, [
      { opacity: 1, transform: 'translateY(0) scale(1)' },
      { opacity: 0, transform: 'translateY(12px) scale(.97)' }
    ], { duration: 200, easing: 'cubic-bezier(.4, 0, 1, 1)', fill: 'forwards' });
    const finish = () => { delete dialog.dataset.closing; if (dialog.open) dialog.close(); animation?.cancel(); dialog.inert = false; };
    if (animation) animation.finished.then(finish, finish); else finish();
  }
  function openDialog(dialog) {
    if (dialog.open || dialog.dataset.closing) return;
    if (!dialogs.has(dialog)) dialog.addEventListener('cancel', event => {
      // Respect the form's existing in-flight-save guard.
      if (event.defaultPrevented) return;
      event.preventDefault();
      closeDialog(dialog);
    });
    dialogs.add(dialog);
    prepareDialog(dialog);
    dialog.showModal();
  }

  // Keep the existing selects as the single source of truth for filtering.
  function initFilters() {
    const status = document.getElementById('status');
    const sort = document.getElementById('run-sort');
    const group = document.createElement('div');
    group.className = 'status-segments';
    group.setAttribute('role', 'group');
    group.setAttribute('aria-label', '运行状态');
    for (const option of status.options) {
      const button = document.createElement('button');
      button.type = 'button';
      button.dataset.status = option.value || 'all';
      const dot = document.createElement('span');
      dot.className = 'status-dot';
      dot.setAttribute('aria-hidden', 'true');
      button.append(dot, option.value ? option.text : '全部');
      button.onclick = () => {
        status.value = option.value;
        status.dispatchEvent(new Event('change', { bubbles: true }));
      };
      group.append(button);
    }
    const syncStatus = () => {
      for (const button of group.querySelectorAll('button')) button.setAttribute('aria-pressed', String(button.dataset.status === (status.value || 'all')));
    };
    status.addEventListener('change', syncStatus);
    syncStatus();
    status.hidden = true;
    status.after(group);

    const picker = document.createElement('details');
    picker.className = 'sort-picker';
    const trigger = document.createElement('summary');
    const label = document.createElement('span');
    const icon = document.createElement('span');
    icon.textContent = '↓';
    icon.className = 'sort-icon';
    icon.setAttribute('aria-hidden', 'true');
    trigger.append(icon, label);
    const panel = document.createElement('div');
    panel.className = 'sort-options';
    panel.setAttribute('role', 'group');
    panel.setAttribute('aria-label', '对话记录排序');
    const names = ['最新优先', '模型调用', '工具 / Skills', 'Token 用量'];
    for (const [index, option] of [...sort.options].entries()) {
      const button = document.createElement('button');
      button.type = 'button';
      button.dataset.value = option.value;
      const name = document.createElement('span');
      name.textContent = names[index];
      const hint = document.createElement('small');
      hint.textContent = index ? '从多到少' : '从新到旧';
      button.append(name, hint);
      button.onclick = () => {
        sort.value = option.value;
        sort.dispatchEvent(new Event('change', { bubbles: true }));
        picker.open = false;
        trigger.focus();
      };
      panel.append(button);
    }
    const syncSort = () => {
      label.textContent = names[sort.selectedIndex];
      trigger.setAttribute('aria-label', `排序：${sort.selectedOptions[0].text}`);
      for (const button of panel.children) button.setAttribute('aria-pressed', String(button.dataset.value === sort.value));
    };
    sort.addEventListener('change', syncSort);
    syncSort();
    picker.append(trigger, panel);
    sort.hidden = true;
    sort.after(picker);
    document.addEventListener('click', event => { if (!picker.contains(event.target)) picker.open = false; });
    picker.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); picker.open = false; trigger.focus(); }
    });
    picker.addEventListener('focusout', event => { if (!picker.contains(event.relatedTarget)) picker.open = false; });
  }

  function init() {
    initFilters();
    const nav = document.getElementById('nav');
    const icons = {
      runs: ['M4 5h16v14H4z', 'M8 9h8M8 13h5'],
      skills: ['m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5Z'],
      tools: ['m8 7-5 5 5 5m8-10 5 5-5 5m-3-14-2 18'],
      memory: ['M9 4H7a3 3 0 0 0-3 3v2m11-5h2a3 3 0 0 1 3 3v2M4 15v2a3 3 0 0 0 3 3h2m6 0h2a3 3 0 0 0 3-3v-2', 'M8 9h8M8 13h8M8 17h4'],
      schedules: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18', 'M12 7v5l3 2']
    };
    const labels = { runs: '运行日志', skills: 'Skills', tools: 'Tools', memory: '记忆中心', schedules: '定时任务' };
    for (const button of nav.querySelectorAll('button')) {
      const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
      icon.setAttribute('viewBox', '0 0 24 24');
      icon.setAttribute('class', 'nav-icon');
      icon.setAttribute('aria-hidden', 'true');
      for (const data of icons[button.dataset.view]) {
        const path = document.createElementNS(icon.namespaceURI, 'path');
        path.setAttribute('d', data);
        icon.append(path);
      }
      const label = document.createElement('span');
      label.textContent = labels[button.dataset.view];
      button.replaceChildren(icon, label);
    }
    mountSegments(nav);
    mountSegments(document.querySelector('.status-segments'));
    // Dispose observers when polling replaces an inspector or a modal closes.
    const discover = () => {
      document.querySelectorAll('.run-tabs').forEach(mountSegments);
      for (const [root, dispose] of segmentInstances) if (!root.isConnected) dispose();
    };
    new MutationObserver(discover).observe(document.body, { childList: true, subtree: true });
    discover();

    // A single delegated handler also covers newly rendered cards and dialogs.
    let lightTarget = null, lightFrame = 0, pointerX = 0, pointerY = 0;
    function clearLight() {
      lightTarget?.classList.remove('is-lit');
      lightTarget = null;
    }
    document.addEventListener('pointermove', event => {
      if (!fine.matches || reduced.matches || event.pointerType === 'touch') return;
      const target = event.target.closest(glowSurfaces);
      if (target !== lightTarget) { clearLight(); lightTarget = target; }
      if (!target) return;
      pointerX = event.clientX; pointerY = event.clientY;
      if (lightFrame) return;
      lightFrame = requestAnimationFrame(() => {
        lightFrame = 0;
        if (!lightTarget?.isConnected) return;
        const rect = lightTarget.getBoundingClientRect();
        lightTarget.style.setProperty('--light-x', `${pointerX - rect.left}px`);
        lightTarget.style.setProperty('--light-y', `${pointerY - rect.top}px`);
        lightTarget.classList.add('is-lit');
      });
    }, { passive: true });
    document.documentElement.addEventListener('pointerleave', clearLight);
    window.addEventListener('blur', clearLight);
    reduced.addEventListener('change', clearLight);
    const toggledByUser = new WeakSet();
    document.addEventListener('click', event => {
      const summary = event.target.closest('summary');
      if (summary) toggledByUser.add(summary.parentElement);
      const button = event.target.closest('button');
      if (!button || button.disabled || button.classList.contains('run')) return;
      animate(button, [
        { scale: '.96' }, { scale: '1.025', offset: .6 }, { scale: '1' }
      ], { duration: 340 });
    });
    document.addEventListener('toggle', event => {
      if (!toggledByUser.has(event.target)) return;
      toggledByUser.delete(event.target);
      if (event.target.matches('details[open]')) {
        for (const child of event.target.children) if (child.tagName !== 'SUMMARY') reveal(child, 0, 5);
      }
    }, true);
    reveal(document.getElementById('login'), 50, 18);
  }
  return { mountSegments, prepareDialog, init, view, catalog, runs, detail, stat, reset, busy, openDialog, closeDialog };
})();
