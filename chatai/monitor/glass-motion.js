// Original, dependency-free motion for the embedded console.
// References: rdev/liquid-glass-react (specular light / elastic response),
// motiondivision/motion (interruptible transforms / staggered entrances).
window.ConsoleMotion = (() => {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const fine = matchMedia('(hover: hover) and (pointer: fine)');
  const active = new Set();
  const entrances = new WeakMap();
  const counts = new Map();
  let previousRuns = new Set(), detailID = '', navFrame = 0;
  const ease = 'cubic-bezier(.22, 1, .36, 1)';

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
    if (id !== detailID) reveal(root.querySelector('.detail-body'), 0, 8);
    detailID = id;
  }
  function stat(node, value) {
    if (node.textContent !== value) node.textContent = value;
    if (counts.has(node.id) && counts.get(node.id) !== value) {
      reveal(node, 0, 4);
    }
    counts.set(node.id, value);
  }
  function reset() { previousRuns.clear(); detailID = ''; counts.clear(); }

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
    ], { duration: 160, easing: 'ease-in', fill: 'forwards' });
    const finish = () => { delete dialog.dataset.closing; if (dialog.open) dialog.close(); dialog.inert = false; };
    if (animation) animation.finished.then(finish, finish); else finish();
  }
  function openDialog(dialog) {
    dialog.addEventListener('cancel', event => {
      // Respect the form's existing in-flight-save guard.
      if (event.defaultPrevented) return;
      event.preventDefault();
      closeDialog(dialog);
    });
    dialog.showModal();
  }

  function init() {
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
    const lens = document.createElement('span');
    lens.className = 'nav-lens';
    lens.setAttribute('aria-hidden', 'true');
    nav.prepend(lens);
    function positionLens() {
      cancelAnimationFrame(navFrame);
      navFrame = requestAnimationFrame(() => {
        const selected = nav.querySelector('.nav-active');
        if (!selected || !nav.offsetWidth) return;
        lens.style.width = `${selected.offsetWidth}px`;
        lens.style.height = `${selected.offsetHeight}px`;
        lens.style.transform = `translate(${selected.offsetLeft}px, ${selected.offsetTop}px)`;
        nav.classList.add('motion-ready');
        nav.querySelectorAll('button').forEach(button => {
          if (button === selected) button.setAttribute('aria-current', 'page');
          else button.removeAttribute('aria-current');
        });
      });
    }
    new ResizeObserver(positionLens).observe(nav);
    nav.addEventListener('click', positionLens);
    document.fonts?.ready.then(positionLens);
    positionLens();

    // A single delegated handler also covers newly rendered cards and dialogs.
    let lightTarget = null, lightFrame = 0, pointerX = 0, pointerY = 0;
    function clearLight() {
      lightTarget?.classList.remove('is-lit');
      lightTarget = null;
    }
    document.addEventListener('pointermove', event => {
      if (!fine.matches || reduced.matches || event.pointerType === 'touch') return;
      const target = event.target.closest('.stat, .resource-card, #nav, .login');
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
  return { init, view, catalog, runs, detail, stat, reset, busy, openDialog, closeDialog };
})();
