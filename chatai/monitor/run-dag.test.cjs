const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),vm=require('node:vm');
const context=vm.createContext({});
vm.runInContext(fs.readFileSync(__dirname+'/run-dag.js','utf8'),context);
const graph=(events,status='success')=>JSON.parse(JSON.stringify(context.buildRunDAG({events,status})));
const e=(kind,extra={})=>({kind,...extra});

test('actual plans fan out and next decision depends on observed tools',()=>{
  const g=graph([e('run_started'),e('model_started',{step:1}),e('model_finished'),
    e('tool_planned',{name:'search',call_id:'a'}),e('tool_planned',{name:'read',call_id:'b'}),
    e('tool_started',{name:'search',call_id:'a'}),e('tool_finished',{name:'search',call_id:'a'}),
    e('tool_started',{name:'read',call_id:'b'}),e('tool_finished',{name:'read',call_id:'b',error:'failed'}),
    e('model_started',{step:2}),e('model_finished'),e('run_finished')]);
  assert.equal(g.nodes.length,6);
  assert.deepEqual(g.nodes.map(n=>n.state),['success','success','success','error','success','success']);
  assert.deepEqual(g.edges.filter(x=>x.from==='n1').map(x=>x.to),['n2','n3']);
  assert.deepEqual(g.edges.filter(x=>x.to==='n4').map(x=>x.from),['n2','n3']);
  for(const edge of g.edges)assert.ok(g.nodes.find(n=>n.id===edge.from).y<g.nodes.find(n=>n.id===edge.to).y);
});
test('running, unexecuted and interrupted calls remain distinct',()=>{
  const events=[e('model_started'),e('model_finished'),e('tool_planned',{name:'a'}),e('tool_planned',{name:'b'}),e('tool_started',{name:'a'})];
  assert.deepEqual(graph(events,'running').nodes.map(n=>n.state),['success','running','planned']);
  assert.deepEqual(graph(events,'error').nodes.map(n=>n.state),['success','interrupted','skipped']);
});
test('retry rounds with reused step and call IDs never merge or cycle',()=>{
  const events=[];
  for(let i=0;i<2;i++)events.push(e('model_started',{step:1}),e('model_finished'),e('tool_planned',{name:'search',call_id:'same'}),e('tool_started',{name:'search',call_id:'same'}),e('tool_finished',{name:'search',call_id:'same'}));
  const g=graph(events);
  assert.equal(g.nodes.length,4);assert.equal(new Set(g.nodes.map(n=>n.id)).size,4);
  assert.ok(g.edges.every(edge=>Number(edge.from.slice(1))<Number(edge.to.slice(1))));
});
test('older recordings without planning events and duplicate unnamed calls work',()=>{
  const g=graph([e('model_started'),e('model_finished'),e('tool_started',{name:'read_skill'}),e('tool_finished',{name:'read_skill'}),e('skill_loaded',{name:'read_skill'}),e('tool_started',{name:'read_skill'}),e('tool_finished',{name:'read_skill'}),e('run_finished')]);
  assert.equal(g.nodes.length,4);assert.equal(g.nodes[1].skill,true);
  assert.equal(g.nodes[2].state,'success');
  assert.deepEqual(graph([]).nodes,[]);
});
test('a superseded plan is not shown as still pending',()=>{
  const g=graph([e('model_started'),e('model_finished'),e('tool_planned',{name:'old'}),e('model_started')],'running');
  assert.equal(g.nodes[1].state,'skipped');assert.equal(g.nodes[2].state,'running');
});

test('inline console scripts remain syntactically valid',()=>{
  const html=fs.readFileSync(__dirname+'/index.html','utf8');
  for(const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g))new vm.Script(match[1]);
});

test('layout follows screen orientation only when the graph has enough width',()=>{
  for(const [width,screenWidth,screenHeight,expected] of [
    [1100,1440,900,'horizontal'],[700,844,390,'horizontal'],
    [600,1440,900,'vertical'],[360,390,844,'vertical'],[800,900,1200,'vertical'],
  ]) assert.equal(context.runDAGDirection(width,screenWidth,screenHeight),expected);
});

test('both layouts keep branches separate, edges forward and cards inside the canvas',()=>{
  const events=[e('run_started'),e('model_started'),e('model_finished')];
  for(let i=0;i<8;i++)events.push(e('tool_planned',{name:`tool${i}`}),e('tool_started',{name:`tool${i}`}),e('tool_finished',{name:`tool${i}`}));
  events.push(e('model_started'),e('model_finished'),e('run_finished'));
  const g=context.buildRunDAG({events,status:'success'});
  const edges=JSON.stringify(g.edges);
  for(const direction of ['horizontal','vertical','horizontal']){
    context.layoutRunDAG(g,direction);
    assert.equal(JSON.stringify(g.edges),edges);
    for(const node of g.nodes){
      assert.ok(node.x>=0 && node.y>=0 && node.x+220<=g.width && node.y+82<=g.height);
      for(const other of g.nodes.filter(n=>n!==node))assert.ok(
        node.x+220<=other.x || other.x+220<=node.x || node.y+82<=other.y || other.y+82<=node.y);
    }
    for(const edge of g.edges){
      const a=g.nodes.find(n=>n.id===edge.from),b=g.nodes.find(n=>n.id===edge.to);
      assert.ok(direction==='horizontal' ? a.x+220<b.x : a.y+82<b.y);
      assert.ok(context.runDAGEdgePath(a,b,direction).startsWith(direction==='horizontal' ? `M ${a.x+220} ${a.y+41}` : `M ${a.x+110} ${a.y+82}`));
    }
  }
});

test('a narrow single-branch graph fits a phone without shrinking its cards',()=>{
  const g=graph([e('run_started'),e('model_started'),e('model_finished'),e('run_finished')]);
  assert.equal(g.width,264);
});
