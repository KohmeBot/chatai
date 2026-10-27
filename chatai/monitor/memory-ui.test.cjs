const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = vm.createContext({});
vm.runInContext(fs.readFileSync(__dirname + '/memory-ui.js', 'utf8'), context);

test('manual memory source displays 自行添加 and never creates a source group avatar', () => {
  context.identity = () => { throw Error('Manual entry must not render a source group'); };
  const node = {append() { throw Error('Unexpected source group'); }};
  for (const source_id of [0, '0', undefined, null, '']) {
    const item = {source_id, group_id: '123456'};
    assert.equal(context.memorySource(item), '自行添加');
    context.appendMemorySourceGroup(node, item);
  }
});

test('chat-derived memory keeps its original message and group attribution', () => {
  const children = [];
  context.identity = (kind, id) => `${kind}:${id}`;
  const node = {append(child) { children.push(child); }};
  const item = {source_id: 42, group_id: '123456'};
  assert.equal(context.memorySource(item), '#42');
  context.appendMemorySourceGroup(node, item);
  assert.deepEqual(children, ['group:123456']);
  context.appendMemorySourceGroup(node, {source_id: 42});
  assert.equal(children.length, 1);
});
