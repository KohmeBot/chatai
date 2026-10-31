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
  // Gentler initial velocity, followed by a long, monotonic settle. Keep in sync
  // with --ease in glass.css; feedback and exits intentionally finish sooner.
  const ease = 'cubic-bezier(.28, .72, .22, 1)';
  const timing = { surface: 520, dialog: 560, feedback: 220, exit: 220 };
  const glowSurfaces = '.stat, .resource-card, .glass-segments, .login, .run-metrics>div, .overview-result, .overview-output, .schema-field, .execution-record, .memory-item, .detail-fields';
  const mountSegments = root => ConsoleControls.mountSegments(root);

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
    ConsoleControls.iconButton(close, 'close', '关闭弹窗');
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
    const animation = node.animate(frames, { duration: timing.surface, easing: ease, ...options });
    active.add(animation);
    animation.finished.catch(() => {}).finally(() => active.delete(animation));
    return animation;
  }
  reduced.addEventListener('change', () => {
    if (reduced.matches) for (const animation of active) animation.cancel();
  });
  function reveal(node, delay = 0, distance = 8, duration = timing.surface) {
    if (!node) return;
    const previous = entrances.get(node);
    const current = previous && previous.playState !== 'finished' ? getComputedStyle(node) : null;
    const from = current ? { opacity: current.opacity, transform: current.transform }
      : { opacity: 0, transform: `translateY(${distance}px)` };
    previous?.cancel();
    const animation = animate(node, [
      from,
      { opacity: 1, transform: 'translateY(0)' }
    ], { delay: current ? 0 : delay, fill: 'backwards', duration });
    if (animation) entrances.set(node, animation);
  }
  function view(node) {
    // Animate the view once: nested entrances multiply opacity and movement.
    reveal(node);
  }
  function catalog(root) {
    // Only explicit navigation/reloads animate; polling never replays the grid.
    [...root.querySelectorAll('.resource-card')].slice(0, 16)
      .forEach((card, i) => reveal(card, Math.min(i * 24, 120)));
  }
  function runs(root) {
    const next = new Set();
    let index = 0;
    for (const row of root.querySelectorAll('[data-run-id]')) {
      next.add(row.dataset.runId);
      if (!previousRuns.has(row.dataset.runId) && index < 12) reveal(row, Math.min(index++ * 20, 120), 6);
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
      if (!graphNodes.has(node.dataset.node) && index < 15) reveal(node, Math.min(index++ * 20, 120), 5);
      graphNodes.add(node.dataset.node);
    }
    detailID = id;
  }
  function stat(node, value) {
    if (node.textContent !== value) node.textContent = value;
    if (counts.has(node.id) && counts.get(node.id) !== value) {
      entrances.get(node)?.cancel();
      const animation = animate(node, [{ opacity: .65 }, { opacity: 1 }], { duration: timing.feedback });
      if (animation) entrances.set(node, animation);
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
    // Closing during entry starts at the visible pose, without snapping to full size.
    const current = getComputedStyle(dialog);
    const from = { opacity: current.opacity, transform: current.transform };
    entrances.get(dialog)?.cancel();
    const animation = animate(dialog, [
      from,
      { opacity: 0, transform: 'translateY(8px)' }
    ], { duration: timing.exit, easing: 'cubic-bezier(.4, 0, .7, 1)', fill: 'forwards' });
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
    reveal(dialog, 0, 16, timing.dialog);
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
      ConsoleControls.iconButton(button, option.value || 'all', option.value ? option.text : '全部状态');
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
    const icon = document.createElement('span');
    icon.append(ConsoleControls.icon('sort'));
    icon.className = 'sort-icon';
    icon.setAttribute('aria-hidden', 'true');
    trigger.append(icon); trigger.classList.add('icon-button');
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
      trigger.title = `排序：${sort.selectedOptions[0].text}`;
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
    const labels = { runs: '运行日志', skills: 'Skills', tools: 'Tools', memory: '记忆中心', schedules: '定时任务' };
    for (const button of nav.querySelectorAll('button')) {
      ConsoleControls.iconButton(button, button.dataset.view, labels[button.dataset.view]);
    }
    for (const [id, icon, label] of [
      ['refresh', 'refresh', '刷新'], ['catalog-refresh', 'refresh', '刷新数据'],
      ['logout', 'logout', '退出'], ['previous', 'left', '上一页'], ['next', 'right', '下一页'],
      ['close-run', 'close', '关闭对话详情']
    ]) ConsoleControls.iconButton(document.getElementById(id), icon, label);
    ConsoleControls.iconButton(document.querySelector('#login-form button'), 'right', '连接并查看');
    mountSegments(nav);
    mountSegments(document.querySelector('.status-segments'));
    // Dispose observers when polling replaces an inspector or a modal closes.
    const discover = () => {
      document.querySelectorAll('.run-tabs').forEach(mountSegments);
      ConsoleControls.prune();
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
      // CSS :active owns press/release, so repeated clicks never stack bounces.
    });
    document.addEventListener('toggle', event => {
      if (!toggledByUser.has(event.target)) return;
      toggledByUser.delete(event.target);
      if (event.target.matches('details[open]')) {
        for (const child of event.target.children) if (child.tagName !== 'SUMMARY') reveal(child, 0, 5);
      }
    }, true);
    reveal(document.getElementById('login'), 0, 12);
  }
  return { reveal, mountSegments, prepareDialog, init, view, catalog, runs, detail, stat, reset, busy, openDialog, closeDialog };
})();
