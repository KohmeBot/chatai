// Shared, dependency-free controls for the embedded console.
// Original implementation. References (interaction/material, not copied code):
// https://github.com/rdev/liquid-glass-react
// https://github.com/vwakesahu/liquid-components
// https://developer.apple.com/design/human-interface-guidelines/motion
window.ConsoleControls = (() => {
  const paths = {
    calendar1: 'M6 5h12a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2ZM8 3v4m8-4v4M4 10h16m-9.5 4 1.5-1v6m-1.5 0h3',
    calendar3: 'M6 5h12a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2ZM8 3v4m8-4v4M4 10h16M10 13h4l-2 3h.5a1.5 1.5 0 0 1 0 3H10',
    calendar7: 'M6 5h12a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2ZM8 3v4m8-4v4M4 10h16M10 13h4l-3 6',
    calendarCustom: 'M4 5h10a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2ZM6 3v4m6-4v4M2 10h14M19 10h4m-4 5h4m-4 5h4M20 9v2m2 3v2m-2 3v2',
    calendar: 'M4 5h16v16H4zM8 3v4m8-4v4M4 10h16M8 14h.01M12 14h.01M16 14h.01M8 17h.01M12 17h.01',
    runs: 'M4 5h16v14H4z M8 9h8M8 13h5',
    skills: 'm12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5Z',
    tools: 'm8 7-5 5 5 5m8-10 5 5-5 5m-3-14-2 18',
    memory: 'M9 4H7a3 3 0 0 0-3 3v2m11-5h2a3 3 0 0 1 3 3v2M4 15v2a3 3 0 0 0 3 3h2m6 0h2a3 3 0 0 0 3-3v-2 M8 9h8M8 13h8M8 17h4',
    schedules: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18 M12 7v5l3 2',
    refresh: 'M20 7v5h-5 M4 17v-5h5 M6.1 7a7 7 0 0 1 11.6-1L20 9 M4 15l2.3 3A7 7 0 0 0 18 17',
    close: 'm6 6 12 12M6 18 18 6',
    plus: 'M12 5v14M5 12h14', minus: 'M5 12h14',
    edit: 'm15 4 5 5M4 20l5-1L21 7a2.1 2.1 0 0 0-4-4L5 15Z',
    trash: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 10v7m4-7v7',
    check: 'm5 12 4 4L19 6', detail: 'M8 4H4v16h16v-4M12 4h8v8M20 4 10 14',
    left: 'm14 5-7 7 7 7', right: 'm10 5 7 7-7 7', down: 'm6 9 6 6 6-6',
    logout: 'M9 4H4v16h5M10 12h11m-5-5 5 5-5 5',
    sun: 'M16 12a4 4 0 1 0-8 0 4 4 0 0 0 8 0M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5',
    moon: 'M20 14.5A8.5 8.5 0 0 1 9.5 4 8.5 8.5 0 1 0 20 14.5Z',
    all: 'M9 7a2 2 0 1 0-4 0 2 2 0 0 0 4 0m10 0a2 2 0 1 0-4 0 2 2 0 0 0 4 0M9 17a2 2 0 1 0-4 0 2 2 0 0 0 4 0m10 0a2 2 0 1 0-4 0 2 2 0 0 0 4 0',
    running: 'M12 3a9 9 0 1 0 9 9 M12 7v5l3 2',
    success: 'M21 11v1a9 9 0 1 1-5.3-8.2 M9 11l3 3L22 4',
    error: 'M21 12a9 9 0 1 0-18 0 9 9 0 0 0 18 0M12 7v6m0 3v.1',
    sort: 'M8 4v16m-4-4 4 4 4-4M14 5h6m-6 5h4m-4 5h2',
    fit: 'M9 4H4v5m11-5h5v5M4 15v5h5m11-5v5h-5',
    overview: 'M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z',
    flow: 'M9 3h6v5H9zM2 16h6v5H2zM16 16h6v5h-6zM12 8v4M5 16v-4h14v4',
    outputs: 'M4 7h16v13H4zM3 4h18v3H3zM9 11h6',
    timing: 'M5 20V10m7 10V4m7 16v-7', logs: 'M6 3h12v18H6zM9 7h6M9 11h6M9 15h4',
    pin: 'M8 3.5h8l-1 7 3 3v1H6v-1l3-3Z',
    pinNeedle: 'M12 14.5V21',
    folder: 'M3 7V5h6l2 2h10v13H3z', file: 'M5 3h9l5 5v13H5zM14 3v6h5',
  };
  function icon(name) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 24 24'); svg.setAttribute('class', 'control-icon');
    svg.setAttribute('aria-hidden', 'true'); svg.setAttribute('focusable', 'false');
    if (name === 'pin' || name === 'pinPinned') {
      svg.classList.add('pin-icon');
      if (name === 'pinPinned') svg.classList.add('is-pinned');
      const part = (tag, attrs, parent = svg) => {
        const node = document.createElementNS(svg.namespaceURI, tag);
        for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, value);
        parent.append(node); return node;
      };
      const body = part('g', {class:'pin-body'});
      part('path', {class:'pin-needle', d:paths.pinNeedle}, body);
      part('path', {class:'pin-cap', d:paths.pin}, body);
      return svg;
    }
    const path = document.createElementNS(svg.namespaceURI, 'path');
    path.setAttribute('d', paths[name] || paths.detail); svg.append(path); return svg;
  }
  function iconButton(button, name, label) {
    button.replaceChildren(icon(name)); button.classList.add('icon-button');
    button.setAttribute('aria-label', label); button.title = label;
    return button;
  }
  const actions = {
    '修改': 'edit', '新增结构化记忆': 'plus', '查看与管理 →': 'memory', '查看详情 →': 'detail',
    '清空群记忆': 'trash', '清空长期记忆': 'trash', '编辑简介与关系': 'edit', '编辑群简介': 'edit',
    '详情': 'detail', '查看详情': 'detail', '查看详情 ↗': 'detail', '查看记忆': 'memory', '×': 'close', '关闭': 'close',
    '取消': 'close', '编辑': 'edit', '删除': 'trash', '删除任务': 'trash', '确认删除': 'trash',
    '保存修改': 'check', '创建任务': 'check', '连接并查看': 'right',
    '重试': 'refresh', '重试目录': 'refresh', '＋ 新增任务': 'plus',
    '添加记忆': 'plus', '新增记忆': 'plus', '根目录': 'folder',
  };
  function action(label, handler, danger = false) {
    const button = document.createElement('button'); button.type = 'button'; button.onclick = handler;
    if (danger) button.classList.add('danger');
    if (actions[label]) iconButton(button, actions[label], label.replace(' ↗', '').replace('＋ ', ''));
    else button.textContent = label; // User names / file names remain readable.
    return button;
  }

  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const pinMotions = new WeakMap();
  // One physical progress value drives every part. Reversals retain position
  // AND velocity; a shared near-critical spring gives a long, quiet settle.
  function setResident(button, resident, animate = false) {
    const body = button.querySelector('.pin-body');
    const needle = button.querySelector('.pin-needle');
    const previous = button.getAttribute('aria-checked') === 'true';
    const changed = button.getAttribute('aria-checked') !== String(resident);
    button.setAttribute('aria-checked', String(resident));
    let motion = pinMotions.get(button);
    if (!body || !needle || !animate || reduced.matches) { motion?.stop(); return; }
    if (!changed) return;
    if (motion) { motion.target = resident ? 1 : 0; return; }
    motion = {position:previous ? 1 : 0, velocity:0, target:resident ? 1 : 0, frame:0, last:performance.now()};
    const smooth = value => { const p = clamp(value, 0, 1); return p * p * (3 - 2 * p); };
    function paint() {
      const p = clamp(motion.position, 0, 1);
      // Lift on the shared arc, straighten before insertion. The reverse path
      // extracts the needle before tilting; no unrelated timed transitions.
      const angle = 28 * (1 - smooth(p / .8));
      const y = -1 + p - 2.4 * Math.sin(Math.PI * p);
      body.style.transform = `translateY(${y}px) rotate(${angle}deg)`;
      needle.style.transform = `scaleY(${1 - .22 * smooth((p - .4) / .6)})`;
    }
    function stop() {
      cancelAnimationFrame(motion.frame);
      body.style.removeProperty('transform'); needle.style.removeProperty('transform');
      reduced.removeEventListener('change', preferenceChanged);
      pinMotions.delete(button);
    }
    function preferenceChanged() { if (reduced.matches) stop(); }
    function tick(now) {
      if (!button.isConnected || reduced.matches) { stop(); return; }
      let dt = Math.min((now - motion.last) / 1000, .04); motion.last = now;
      while (dt > 0) {
        const step = Math.min(dt, 1 / 240); dt -= step;
        motion.velocity += ((motion.target - motion.position) * spring.settle.stiffness - motion.velocity * spring.settle.damping) * step;
        motion.position += motion.velocity * step;
      }
      if (Math.abs(motion.target - motion.position) < .001 && Math.abs(motion.velocity) < .01) { stop(); return; }
      paint(); motion.frame = requestAnimationFrame(tick);
    }
    motion.stop = stop; pinMotions.set(button, motion);
    reduced.addEventListener('change', preferenceChanged);
    paint(); motion.frame = requestAnimationFrame(tick);
  }
  const instances = new Map();
  // Near-critical damping avoids a rubbery overshoot on selection. Direct
  // manipulation stays firmer so the lens still follows the pointer promptly.
  const spring = { settle: { stiffness: 220, damping: 29 }, drag: { stiffness: 600, damping: 44 } };
  const clamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n));
  const rubber = (value, lo, hi) => value < lo ? lo - 14 * (1 - Math.exp((value - lo) / 45)) : value > hi ? hi + 14 * (1 - Math.exp((hi - value) / 45)) : value;

  function mountSegments(root) {
    if (!root || instances.has(root)) return;
    root.classList.add('glass-segments');
    const lens = document.createElement('span'); lens.className = 'glass-lens';
    lens.setAttribute('aria-hidden', 'true'); root.append(lens);
    const state = {x: 0, y: 0, w: 0, h: 0, lift: 0}, velocity = {...state}, target = {...state};
    let frame = 0, lastTime = 0, ready = false, drag = null, suppressUntil = 0;
    const buttons = () => [...root.querySelectorAll(':scope > button')].filter(b => !b.disabled && !b.hidden);
    const selected = () => buttons().find(b => b.matches('.nav-active,[aria-selected="true"],[aria-pressed="true"]'));
    const bounds = b => ({x: b.offsetLeft + b.offsetWidth / 2, y: b.offsetTop, w: b.offsetWidth, h: b.offsetHeight});
    function paint() {
      const lift = reduced.matches ? 0 : state.lift;
      const stretch = reduced.matches ? 0 : Math.min(.09, Math.abs(velocity.x) / 4800);
      lens.style.width = `${state.w}px`; lens.style.height = `${state.h}px`;
      lens.style.transform = `translate3d(${state.x - state.w / 2}px,${state.y}px,0) scale(${1 + stretch + lift * .025},${1 - stretch * .36 + lift * .05})`;
      lens.style.borderRadius = `${Math.max(14, state.h / 2 - stretch * 24)}px / ${Math.max(12, state.h / 2 + stretch * 18)}px`;
      lens.style.setProperty('--lens-light', `${50 - clamp(velocity.x / 18, -35, 35)}%`);
      lens.style.setProperty('--lens-lift', state.lift);
    }
    function tick(now) {
      frame = 0;
      if (!root.isConnected) { dispose(); return; }
      let dt = Math.min((now - (lastTime || now - 16)) / 1000, .04); lastTime = now;
      let moving = false;
      const { stiffness, damping } = drag ? spring.drag : spring.settle;
      // Small integration steps keep springs stable at 60/120 Hz and after stalls.
      while (dt > 0) {
        const step = Math.min(dt, 1 / 240); dt -= step;
        for (const key of Object.keys(state)) {
          velocity[key] += ((target[key] - state[key]) * stiffness - velocity[key] * damping) * step;
          state[key] += velocity[key] * step;
        }
      }
      for (const key of Object.keys(state)) if (Math.abs(target[key] - state[key]) > .015 || Math.abs(velocity[key]) > .08) moving = true;
      if (!moving) { Object.assign(state, target); for (const key in velocity) velocity[key] = 0; }
      paint();
      if (moving) frame = requestAnimationFrame(tick); else lastTime = 0;
    }
    function move(next, immediate = false) {
      Object.assign(target, next);
      if (immediate || reduced.matches) {
        cancelAnimationFrame(frame); frame = 0; lastTime = 0;
        Object.assign(state, target); for (const key in velocity) velocity[key] = 0; paint();
      } else if (!frame) frame = requestAnimationFrame(tick);
    }
    function sync() {
      const button = selected();
      if (!button || !root.offsetWidth) return;
      if (root.id === 'nav') for (const b of buttons()) {
        if (b === button) b.setAttribute('aria-current', 'page'); else b.removeAttribute('aria-current');
      }
      if (!drag) move({...bounds(button), lift: 0}, !ready);
      ready = true; root.classList.add('motion-ready');
    }
    function point(event) { return event.clientX - root.getBoundingClientRect().left - root.clientLeft + root.scrollLeft; }
    function nearest(x) { return buttons().reduce((best, b) => !best || Math.abs(bounds(b).x - x) < Math.abs(bounds(best).x - x) ? b : best, null); }
    function down(event) {
      const button = event.target.closest('button');
      if (event.button !== 0 || !event.isPrimary || drag || !buttons().includes(button)) return;
      suppressUntil = 0;
      drag = {id: event.pointerId, startX: event.clientX, startY: event.clientY, anchor: point(event) - bounds(button).x, moved: false};
      move({...bounds(button), lift: 1}); root.classList.add('is-held');
    }
    function motion(event) {
      if (!drag || event.pointerId !== drag.id) return;
      const dx = event.clientX - drag.startX, dy = event.clientY - drag.startY;
      if (!drag.moved && Math.abs(dy) > 8 && Math.abs(dy) > Math.abs(dx)) { finish(false); return; }
      if (!drag.moved && Math.abs(dx) < 5) return;
      if (!drag.moved) { drag.moved = true; root.setPointerCapture(drag.id); root.classList.add('is-dragging'); }
      event.preventDefault();
      const items = buttons(), x = point(event) - drag.anchor, closest = nearest(x);
      if (!closest) { finish(false); return; }
      const centers = items.map(b => bounds(b).x);
      move({...bounds(closest), x: rubber(x, Math.min(...centers), Math.max(...centers)), lift: 1});
    }
    function finish(commit) {
      if (!drag) return;
      const current = drag; drag = null;
      root.classList.remove('is-held', 'is-dragging');
      if (root.hasPointerCapture(current.id)) root.releasePointerCapture(current.id);
      if (current.moved) {
        suppressUntil = performance.now() + 500;
        if (commit) { const button = nearest(target.x); button?.focus({preventScroll: true}); button?.click(); }
      }
      sync();
    }
    function up(event) { if (event.pointerId === drag?.id) finish(true); }
    function cancel(event) { if (!event || event.type === 'blur' || event.pointerId === drag?.id) finish(false); }
    function lostCapture(event) {
      // Touch implicitly captures the pressed button. Its lost capture bubbles
      // when motion transfers capture to root; only losing root's capture ends
      // our drag, not that normal handoff from a descendant.
      if (event.target === root) cancel(event);
    }
    function click(event) {
      // The release click must not activate the original or captured element again.
      if (event.detail && performance.now() < suppressUntil) { event.preventDefault(); event.stopImmediatePropagation(); }
    }
    function keyboard(event) {
      if (event.key === 'Escape' && drag) { event.preventDefault(); finish(false); return; }
      if (root.getAttribute('role') === 'tablist') return;
      const items = buttons(), index = items.indexOf(event.target);
      if (index < 0) return;
      const next = event.key === 'ArrowRight' ? (index + 1) % items.length : event.key === 'ArrowLeft' ? (index + items.length - 1) % items.length : event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : -1;
      if (next < 0) return;
      event.preventDefault(); items[next].focus(); items[next].click();
    }
    const resize = new ResizeObserver(() => { finish(false); sync(); }); resize.observe(root); buttons().forEach(b => resize.observe(b));
    const selection = new MutationObserver(records => {
      if (records.some(record => record.target.tagName === 'BUTTON')) sync();
    });
    selection.observe(root, {subtree: true, attributes: true, attributeFilter: ['class', 'aria-selected', 'aria-pressed', 'disabled', 'hidden']});
    const preference = () => { finish(false); sync(); };
    const listeners = {pointerdown: down, pointermove: motion, pointerup: up, pointercancel: cancel, lostpointercapture: lostCapture, keydown: keyboard};
    for (const [type, listener] of Object.entries(listeners)) root.addEventListener(type, listener);
    root.addEventListener('click', click, true); window.addEventListener('blur', cancel); reduced.addEventListener('change', preference);
    // A tap can end outside without ever crossing the drag threshold.
    window.addEventListener('pointerup', up); window.addEventListener('pointercancel', cancel);
    function dispose() {
      cancelAnimationFrame(frame); resize.disconnect(); selection.disconnect();
      for (const [type, listener] of Object.entries(listeners)) root.removeEventListener(type, listener);
      root.removeEventListener('click', click, true); window.removeEventListener('blur', cancel);
      window.removeEventListener('pointerup', up); window.removeEventListener('pointercancel', cancel);
      reduced.removeEventListener('change', preference); instances.delete(root);
      lens.remove(); root.classList.remove('glass-segments', 'motion-ready', 'is-held', 'is-dragging');
    }
    instances.set(root, dispose); document.fonts?.ready.then(() => { if (instances.get(root) === dispose && root.isConnected) sync(); }); sync();
  }
  function prune() { for (const [root, dispose] of instances) if (!root.isConnected) dispose(); }
  // Draft values live in the dialog. Only the owner's submit handler commits them.
  function dateRangePicker(dialog) {
    const find = suffix => dialog.querySelector('#run-date-' + suffix);
    const start = find('start'), end = find('end'), grid = find('calendar');
    const value = date => `${String(date.getFullYear()).padStart(4,'0')}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`;
    const parse = text => {
      if (!/^\d{4}-\d{2}-\d{2}$/.test(text)) return null;
      const [y,m,d] = text.split('-').map(Number), date = new Date(y,m-1,d);
      return value(date) === text ? date : null;
    };
    let month = new Date(), editing = start, cursor = value(month);
    const monthStart = date => new Date(date.getFullYear(), date.getMonth(), 1);
    function render(animate = false) {
      find('month').textContent = month.toLocaleDateString('zh-CN', {year:'numeric',month:'long'});
      find('hint').textContent = editing === start ? '选择开始日期 · 或输入 YYYY-MM-DD' : '选择结束日期 · 或输入 YYYY-MM-DD';
      start.parentElement.classList.toggle('is-editing', editing === start);
      end.parentElement.classList.toggle('is-editing', editing === end);
      grid.replaceChildren();
      const header = document.createElement('div'); header.className = 'date-week'; header.setAttribute('role','row');
      for (const day of ['一','二','三','四','五','六','日']) {
        const cell = document.createElement('span'); cell.textContent = day; cell.setAttribute('role','columnheader'); header.append(cell);
      }
      grid.append(header);
      const first = new Date(month); first.setDate(1 - (first.getDay()+6)%7);
      const valid = Boolean(parse(start.value) && parse(end.value) && start.value <= end.value);
      for (let row = 0; row < 6; row++) {
        const week = document.createElement('div'); week.className = 'date-week'; week.setAttribute('role','row');
        for (let col = 0; col < 7; col++) {
          const date = new Date(first); date.setDate(first.getDate()+row*7+col);
          const key = value(date), selected = key === start.value || key === end.value;
          const cell = document.createElement('div'); cell.setAttribute('role','gridcell'); cell.setAttribute('aria-selected',String(selected || (valid && key > start.value && key < end.value)));
          cell.className = 'date-cell';
          cell.classList.toggle('in-range',!!valid && key >= start.value && key <= end.value);
          cell.classList.toggle('range-start',key === start.value); cell.classList.toggle('range-end',key === end.value);
          const button = document.createElement('button'); button.type = 'button'; button.textContent = date.getDate(); button.dataset.date = key;
          button.className = 'date-day'; button.tabIndex = key === cursor ? 0 : -1;
          button.classList.toggle('outside-month',date.getMonth() !== month.getMonth());
          button.setAttribute('aria-label',date.toLocaleDateString('zh-CN',{year:'numeric',month:'long',day:'numeric',weekday:'long'}) + (key === start.value ? '，开始' : '') + (key === end.value ? '，结束' : ''));
          if (key === value(new Date())) button.setAttribute('aria-current','date');
          button.onclick = () => {
            if (editing === start) { start.value = key; end.value = ''; editing = end; }
            else { end.value = key; if (start.value > key) [start.value,end.value] = [key,start.value]; editing = start; }
            find('error').textContent = ''; cursor = key; month = monthStart(date); render(); focusDay();
          };
          cell.append(button); week.append(cell);
        }
        grid.append(week);
      }
      if (animate) ConsoleMotion.reveal(grid,0,4);
    }
    function focusDay() { grid.querySelector(`[data-date="${cursor}"]`)?.focus({preventScroll:true}); }
    function shiftMonth(amount) {
      const next = new Date(month.getFullYear(),month.getMonth()+amount,1);
      if(next.getFullYear()<100 || next.getFullYear()>9999) return;
      month=next; cursor=value(month); render(true);
    }
    for (const [id,name,label] of [['prev','left','上个月'],['next','right','下个月'],['today','sun','回到本月'],['apply','check','应用日期范围']]) iconButton(find(id),name,label);
    find('prev').onclick = () => shiftMonth(-1); find('next').onclick = () => shiftMonth(1);
    find('today').onclick = () => { month=monthStart(new Date());cursor=value(new Date());render(true); };
    for(const input of [start,end]) {
      input.addEventListener('focus',()=>{editing=input;const date=parse(input.value);if(date){month=monthStart(date);cursor=value(date)}render()});
      input.addEventListener('input',()=>{find('error').textContent='';const date=parse(input.value);if(date){month=monthStart(date);cursor=value(date)}render()});
    }
    grid.addEventListener('keydown',event=>{
      const date=parse(event.target.dataset.date);if(!date)return;
      const offset={ArrowLeft:-1,ArrowRight:1,ArrowUp:-7,ArrowDown:7}[event.key];
      if(offset) date.setDate(date.getDate()+offset);
      else if(event.key==='Home')date.setDate(date.getDate()-(date.getDay()+6)%7);
      else if(event.key==='End')date.setDate(date.getDate()+6-(date.getDay()+6)%7);
      else if(event.key==='PageUp'||event.key==='PageDown') {
        const day=date.getDate();date.setDate(1);date.setMonth(date.getMonth()+(event.key==='PageUp'?-1:1)*(event.shiftKey?12:1));
        date.setDate(Math.min(day,new Date(date.getFullYear(),date.getMonth()+1,0).getDate()));
      } else return;
      event.preventDefault();if(date.getFullYear()<100||date.getFullYear()>9999)return;
      const changed=date.getMonth()!==month.getMonth()||date.getFullYear()!==month.getFullYear();
      month=monthStart(date);cursor=value(date);render(changed);focusDay();
    });
    let backdropDown=false;
    dialog.addEventListener('pointerdown',event=>{backdropDown=event.target===dialog});
    dialog.addEventListener('click',event=>{if(backdropDown&&event.target===dialog)ConsoleMotion.closeDialog(dialog)});
    return {open(from,to) {start.value=from;end.value=to;editing=start;month=monthStart(parse(from)||new Date());cursor=from;render();ConsoleMotion.openDialog(dialog);focusDay()}};
  }
  return {icon, iconButton, action, setResident, mountSegments, prune, dateRangePicker};
})();
