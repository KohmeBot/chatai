const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = vm.createContext({});
vm.runInContext(fs.readFileSync(__dirname + '/run-overview.js', 'utf8'), context);
const summarize = run => JSON.parse(JSON.stringify(context.summarizeRun(run)));
const timing = run => JSON.parse(JSON.stringify(context.runTiming(run)));
const at = ms => new Date(Date.UTC(2026, 9, 27) + ms).toISOString();
test('counts model requests once, including failures, repeated steps and pending calls', () => {
  assert.equal(summarize({events: [
    {kind: 'model_started', step: 1}, {kind: 'model_finished', step: 1},
    {kind: 'model_started', step: 1}, {kind: 'model_finished', step: 1, error: 'failed'},
    {kind: 'model_finished', step: 2}, {kind: 'model_started', step: 3},
  ]}).modelCallCount, 4);
  assert.equal(summarize({}).modelCallCount, 0);
});
const event = (kind, ms, extra = {}) => ({kind, time: at(ms), ...extra});

test('timing separates each skill file, pairs duplicate tools by ID and avoids double-counting skill notifications', () => {
  const result = timing({started_at: at(0), duration_ms: 1000, status: 'success', events: [
    event('model_started', 0, {step: 1}), event('model_finished', 500, {step: 1}),
    event('tool_started', 500, {name: 'read_skill', call_id: 's1', arguments: '{"name":"report"}'}),
    event('tool_finished', 520, {name: 'read_skill', call_id: 's1'}),
    event('skill_loaded', 520, {name: 'read_skill', call_id: 's1', arguments: '{"name":"report"}'}),
    event('tool_started', 520, {name: 'read_skill', call_id: 's2', arguments: '{"name":"report","file":"references/example.md"}'}),
    event('tool_finished', 550, {name: 'read_skill', call_id: 's2'}),
    event('tool_started', 550, {name: 'search', call_id: 'a'}),
    event('tool_started', 600, {name: 'search', call_id: 'b'}),
    event('tool_finished', 700, {name: 'search', call_id: 'b', error: 'failed'}),
    event('tool_finished', 900, {name: 'search', call_id: 'a'}),
    event('run_finished', 1000),
  ]});
  assert.deepEqual(result.rows.map(r => r.duration), [500, 20, 30, 350, 100]);
  assert.equal(result.rows[4].state, '失败');
  assert.equal(result.rows[3].color, result.rows[4].color);
  assert.notEqual(result.rows[1].color, result.rows[2].color);
  assert.equal(new Set(result.categories.map(c => c.color)).size, 4);
  assert.equal(result.total, 1000);
  assert.equal(result.span, 1000);
  assert.equal(result.categories[0].kind, 'model');
  assert.equal(result.categories[1].count, 2);
});

test('active and interrupted calls are bounded by the run snapshot, including zero-duration calls', () => {
  const run = {started_at: at(0), duration_ms: 1000, status: 'running', events: [
    event('tool_started', 0, {name: 'fast', step: 1}),
    event('tool_finished', 0, {name: 'fast', step: 1}),
    event('model_started', 100, {step: 2}),
  ]};
  let result = timing(run);
  assert.equal(result.rows[0].duration, 0);
  assert.equal(result.rows[1].duration, 900);
  assert.equal(result.rows[1].state, '执行中 · 暂计');
  run.status = 'error';
  run.events.push(event('run_finished', 800));
  result = timing(run);
  assert.equal(result.rows[1].duration, 700);
  assert.equal(result.rows[1].state, '未记录完成 · 估算');
});

test('search-loaded skill markers do not inflate duration and new categories preserve earlier colors', () => {
  const run = {started_at: at(0), status: 'running', duration_ms: 100, events: [
    event('tool_started', 0, {name: 'search_tools', step: 1}),
    event('skill_loaded', 30, {name: 'report', step: 1}),
    event('tool_finished', 100, {name: 'search_tools', step: 1}),
  ]};
  const first = timing(run);
  assert.equal(first.total, 100);
  assert.equal(first.rows[1].marker, true);
  run.events.push(event('tool_started', 100, {name: 'another_tool', step: 2}));
  const next = timing(run);
  assert.equal(first.rows[0].color, next.rows[0].color);
  assert.notEqual(next.rows[0].color, next.rows[2].color);
});

test('missing starts use recorded duration, malformed timing and truncated arguments degrade safely', () => {
  const result = timing({started_at: at(0), events: [
    event('model_finished', 1000, {step: 1, duration_ms: 500}),
    event('tool_started', 1000, {name: 'read_skill', arguments: '{[truncated]'}),
    event('tool_finished', 1100, {name: 'read_skill'}),
    {kind: 'tool_finished', name: 'unknown', time: 'invalid'},
  ]});
  assert.equal(result.rows[0].start, 500);
  assert.equal(result.rows[0].duration, 500);
  assert.match(result.rows[1].label, /名称未保留/);
  assert.equal(result.untimed, 1);
  assert.equal(result.total, 600);
  assert.equal(timing({}).rows.length, 0);
  assert.equal(timing({}).span, 0);
});

test('pairs repeated calls by ID and preserves failed and pending calls', () => {
  const result = summarize({events: [
    {kind: 'tool_started', name: 'search', call_id: 'a'},
    {kind: 'tool_started', name: 'search', call_id: 'b'},
    {kind: 'tool_finished', name: 'search', call_id: 'b', error: 'failed'},
    {kind: 'tool_finished', name: 'search', call_id: 'a', result: '[]'},
    {kind: 'tool_started', name: 'search', call_id: 'c'},
  ]});
  assert.deepEqual(result.tools, [{name: 'search', total: 3, success: 1, error: 1, pending: 1}]);
});

test('collects loaded skills, sent content and final answer without using intermediate model text', () => {
  const result = summarize({events: [
    {kind: 'skill_loaded', name: 'read_skill', arguments: '{"name":"report","file":"SKILL.md"}'},
    {kind: 'skill_loaded', name: 'report'},
    {kind: 'tool_started', name: 'send_messages', call_id: 'a', arguments: '{"messages":["第一条","第二条"]}'},
    {kind: 'tool_finished', name: 'send_messages', call_id: 'a', result: '{"message_ids":[1,2]}'},
    {kind: 'model_finished', text: 'intermediate'},
    {kind: 'run_finished', text: '已完成'},
  ]});
  assert.deepEqual(result.skills, [['report', 2]]);
  assert.equal(result.outputs[0].text, '第一条\n第二条');
  assert.equal(result.finalAnswer, '已完成');
});

test('keeps script output on failure and handles missing/truncated payloads', () => {
  const result = summarize({events: [
    {kind: 'skill_loaded', name: 'read_skill', arguments: '{[内容已截断]'},
    {kind: 'tool_finished', name: 'run_skill_script', result: '{"stdout":"partial","stderr":"error"}', error: 'exit 1'},
    {kind: 'tool_started', name: 'send_image', call_id: 'a', arguments: '{[内容已截断]'},
    {kind: 'tool_finished', name: 'send_image', call_id: 'a', result: '{[内容已截断]'},
  ]});
  assert.equal(result.outputs[0].text, 'partial\nerror');
  assert.equal(result.outputs[0].error, 'exit 1');
  assert.equal(result.outputs[1].result, '{[内容已截断]');
  assert.equal(result.tools[0].error, 1);
  assert.deepEqual(summarize({}).outputs, []);
});
