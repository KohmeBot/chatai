const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// A small event/layout harness keeps pointer regressions runnable with Node alone.
// Actual browser layout, native drag and keyboard behavior are checked separately.
function fixture(reducedMotion = false) {
  let now = 0, nextFrame = 0;
  const frames = new Map(), observers = [];
  class Target {
    listeners = new Map();
    addEventListener(type, fn) { if (!this.listeners.has(type)) this.listeners.set(type, new Set()); this.listeners.get(type).add(fn); }
    removeEventListener(type, fn) { this.listeners.get(type)?.delete(fn); }
    emit(type, values = {}) {
      const event = {type, target: this, pointerId: 1, button: 0, isPrimary: true, clientX: 0, clientY: 0,
        preventDefault() { this.prevented = true; }, stopImmediatePropagation() { this.stopped = true; }, ...values};
      for (const fn of this.listeners.get(type) || []) { fn(event); if (event.stopped) break; }
      return event;
    }
  }
  class Node extends Target {
    constructor(tag) {
      super(); this.tagName = tag.toUpperCase(); this.children = []; this.attributes = new Map(); this.isConnected = true;
      this.style = {setProperty(name, value) { this[name] = value; }};
      const classes = new Set(); this.classList = {add: (...v) => v.forEach(c => classes.add(c)), remove: (...v) => v.forEach(c => classes.delete(c)), contains: c => classes.has(c)};
      this.offsetWidth = 60; this.offsetHeight = 44; this.offsetTop = 5; this.offsetLeft = 5; this.clientLeft = 1; this.scrollLeft = 0;
    }
    setAttribute(key, value) { this.attributes.set(key, String(value)); }
    getAttribute(key) { return this.attributes.get(key) ?? null; }
    removeAttribute(key) { this.attributes.delete(key); }
    append(...nodes) { for (const node of nodes) { node.parentElement = this; this.children.push(node); } }
    remove() { if (this.parentElement) this.parentElement.children = this.parentElement.children.filter(node => node !== this); }
    replaceChildren(...nodes) { this.children = []; this.append(...nodes); }
    closest() { return this.tagName === 'BUTTON' ? this : this.parentElement?.closest(); }
    matches() { return this.classList.contains('nav-active') || this.getAttribute('aria-selected') === 'true' || this.getAttribute('aria-pressed') === 'true'; }
    querySelectorAll() { return this.children.filter(c => c.tagName === 'BUTTON'); }
    getBoundingClientRect() { return {left: 100}; }
    setPointerCapture(id) { this.capture = id; }
    hasPointerCapture(id) { return this.capture === id; }
    releasePointerCapture() { this.capture = null; }
    focus() { this.focused = true; }
    click() { const e = this.parentElement.emit('click', {target: this, detail: 0}); if (!e.prevented) this.onclick?.(); }
  }
  const window = new Target(), preference = new Target(); preference.matches = reducedMotion;
  class Observer {
    constructor(callback) { this.callback = callback; observers.push(this); }
    observe() {}
    disconnect() { this.disconnected = true; }
  }
  const context = vm.createContext({window, document: {createElement: tag => new Node(tag), createElementNS: (_, tag) => new Node(tag)},
    matchMedia: () => preference, ResizeObserver: Observer, MutationObserver: Observer, performance: {now: () => now},
    requestAnimationFrame: callback => { frames.set(++nextFrame, callback); return nextFrame; }, cancelAnimationFrame: id => frames.delete(id)});
  vm.runInContext(fs.readFileSync(__dirname + '/glass-controls.js', 'utf8'), context);
  const controls = window.ConsoleControls, root = new Node('div'); root.offsetWidth = 280;
  const buttons = [60, 92, 70].map((width, index) => {
    const b = new Node('button'); b.offsetWidth = width; b.offsetLeft = [5, 71, 169][index]; b.setAttribute('aria-pressed', index === 0); root.append(b); return b;
  });
  let calls = [];
  const sync = () => observers.filter(o => !o.disconnected).forEach(o => {
    // Resize callbacks cancel gestures; only notify the selection observer here.
    if (o === observers[1]) o.callback([{target: buttons[0]}]);
  });
  buttons.forEach((button, index) => button.onclick = () => { calls.push(index); buttons.forEach(b => b.setAttribute('aria-pressed', b === button)); sync(); });
  controls.mountSegments(root);
  const lens = root.children.at(-1);
  const frame = (step = 1000 / 60) => { now += step; const pending = [...frames.values()]; frames.clear(); pending.forEach(fn => fn(now)); };
  const settle = (step) => { for (let i = 0; frames.size && i < 800; i++) frame(step); assert.equal(frames.size, 0, 'spring must settle and release the animation loop'); };
  const pointer = (type, x, extras = {}) => root.emit(type, {target: buttons[0], clientX: x + 101, ...extras});
  return {root, buttons, controls, lens, pointer, frame, settle, frames, calls, preference, window, observers, sync};
}

test('drag previews without committing, then activates the nearest segment exactly once', () => {
  const f = fixture(); f.pointer('pointerdown', 35); f.pointer('pointermove', 117); f.frame();
  assert.deepEqual(f.calls, []); assert.equal(f.root.capture, 1);
  const stretch = Number(f.lens.style.transform.match(/scale\(([^,]+)/)[1]);
  assert.ok(stretch > 1 && stretch < 1.12, 'the moving lens stretches subtly');
  f.pointer('pointerup', 117); assert.deepEqual(f.calls, [1]);
  assert.equal(f.root.emit('click', {detail: 1}).prevented, true, 'suppress native release click');
  f.settle(); assert.equal(f.lens.style.transform, 'translate3d(71px,5px,0) scale(1,1)');
  assert.equal(f.lens.style.width, '92px');
  // A subsequent real click is not swallowed by the previous gesture.
  f.pointer('pointerdown', 35); f.pointer('pointerup', 35);
  assert.notEqual(f.root.emit('click', {detail: 1}).prevented, true);
});

test('cancel, lost capture, Escape and window blur restore selection without activating', () => {
  for (const reason of ['pointercancel', 'lostpointercapture', 'Escape', 'blur']) {
    const f = fixture(); f.pointer('pointerdown', 35); f.pointer('pointermove', 204); f.frame();
    if (reason === 'Escape') f.root.emit('keydown', {key: reason});
    else if (reason === 'blur') f.window.emit(reason);
    else f.pointer(reason, 204, {target: f.root});
    f.settle(); assert.deepEqual(f.calls, [], reason);
    assert.equal(f.root.classList.contains('is-held'), false, reason);
    assert.equal(f.lens.style.transform, 'translate3d(5px,5px,0) scale(1,1)');
  }
});

test('touch capture transfers from the button to the root without cancelling the drag', () => {
  const f = fixture();
  const touch = (type, x, extras = {}) => f.pointer(type, x, {pointerType: 'touch', ...extras});
  touch('pointerdown', 35);
  touch('pointermove', 65);
  // Touch implicitly captures the button. On the next event, transferring
  // capture to the root emits a bubbling lostpointercapture from that button.
  touch('lostpointercapture', 65);
  assert.equal(f.root.classList.contains('is-dragging'), true);
  assert.equal(f.root.hasPointerCapture(1), true);
  touch('pointermove', 204, {target: f.root});
  assert.deepEqual(f.calls, [], 'only preview while the finger is moving');
  touch('pointerup', 204, {target: f.root});
  touch('lostpointercapture', 204, {target: f.root});
  assert.equal(f.root.emit('click', {detail: 1}).prevented, true);
  f.settle();
  assert.deepEqual(f.calls, [2], 'activate the release destination exactly once');
  assert.equal(f.lens.style.transform, 'translate3d(169px,5px,0) scale(1,1)');
});

test('vertical touch scrolling and secondary pointers do not activate a segment', () => {
  const f = fixture(); f.pointer('pointerdown', 35); f.pointer('pointermove', 204, {pointerId: 2});
  assert.equal(f.root.hasPointerCapture(2), false);
  const event = f.pointer('pointermove', 37, {clientY: 25});
  assert.notEqual(event.prevented, true); f.pointer('pointerup', 37); f.settle(); assert.deepEqual(f.calls, []);
});

test('overscroll stays bounded and disabled destinations are skipped', () => {
  const f = fixture(); f.buttons[2].disabled = true;
  f.pointer('pointerdown', 35); f.pointer('pointermove', 10000); f.settle();
  const x = Number(f.lens.style.transform.match(/translate3d\(([^p]+)/)[1]);
  assert.ok(x < 100, 'rubber band is bounded near the last enabled button');
  f.pointer('pointerup', 10000); f.settle(); assert.deepEqual(f.calls, [1]);
});

test('rapid reversal is continuous and settles at the latest selection at 120 Hz', () => {
  const f = fixture(); f.buttons[2].click(); f.frame(1000 / 120); const before = f.lens.style.transform;
  f.buttons[0].click(); assert.equal(f.lens.style.transform, before, 'retarget without jumping');
  f.settle(1000 / 120); assert.equal(f.lens.style.transform, 'translate3d(5px,5px,0) scale(1,1)');
});

test('selection responds immediately and settles gently at 60 and 120 Hz', () => {
  for (const hz of [60, 120]) {
    const f = fixture(); f.buttons[2].click();
    assert.deepEqual(f.calls, [2], 'activation never waits for the spring');
    const position = () => Number(f.lens.style.transform.match(/translate3d\(([^p]+)/)[1]);
    let previous = position();
    for (let i = 0; i < hz; i++) {
      f.frame(1000 / hz);
      const x = position();
      assert.ok(x >= previous - .02 && x <= 169.2, 'no visible recoil or overshoot');
      if (i === Math.round(hz / 10) - 1) assert.ok(x > 30 && x < 100, 'visible response without rushing to the destination');
      if (i === Math.round(hz / 2) - 1) assert.ok(Math.abs(x - 169) < 2, 'effectively at rest within half a second');
      previous = x;
    }
    f.settle(1000 / hz);
    assert.equal(position(), 169);
  }
});

test('enabling reduced motion during travel immediately releases the animation loop', () => {
  const f = fixture(); f.buttons[2].click(); f.frame();
  f.preference.matches = true; f.preference.emit('change');
  assert.equal(f.frames.size, 0);
  assert.equal(f.lens.style.transform, 'translate3d(169px,5px,0) scale(1,1)');
});

test('reduced motion keeps dragging functional without stretching or animation frames', () => {
  const f = fixture(true); f.pointer('pointerdown', 35); f.pointer('pointermove', 204);
  assert.match(f.lens.style.transform, /scale\(1,1\)$/); assert.equal(f.frames.size, 0);
  f.pointer('pointerup', 204); assert.deepEqual(f.calls, [2]); assert.equal(f.frames.size, 0);
});

test('keyboard navigation skips disabled options and removal disposes all listeners', () => {
  const f = fixture(); f.buttons[1].disabled = true;
  f.root.emit('keydown', {target: f.buttons[0], key: 'ArrowRight'}); assert.deepEqual(f.calls, [2]);
  f.root.isConnected = false; f.controls.prune(); assert.equal(f.frames.size, 0);
  assert.ok(f.observers.every(o => o.disconnected));
  assert.ok([...f.window.listeners.values()].every(list => list.size === 0));
  assert.ok([...f.preference.listeners.values()].every(list => list.size === 0));
  f.root.isConnected = true; f.controls.mountSegments(f.root);
  assert.equal(f.root.children.length, 4, 'reattachment must not duplicate the lens');
});

test('shared actions show SVG only while keeping their accessible names', () => {
  const f = fixture(); const button = f.controls.action('修改', () => {});
  assert.equal(button.getAttribute('aria-label'), '修改'); assert.equal(button.title, '修改');
  assert.equal(button.children[0].tagName, 'SVG'); assert.equal(button.children[0].getAttribute('aria-hidden'), 'true');
});
