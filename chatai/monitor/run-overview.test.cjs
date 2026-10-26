const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = vm.createContext({});
vm.runInContext(fs.readFileSync(__dirname + '/run-overview.js', 'utf8'), context);
const summarize = run => JSON.parse(JSON.stringify(context.summarizeRun(run)));

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
