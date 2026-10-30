'use strict';

// The graph describes observed decisions and their requested tools. Fan-out is
// a planning relationship, not a claim that the executor runs tools in parallel.
function buildRunDAG(run) {
  const nodes = [], edges = [], rounds = [];
  let round = null, frontier = [], trigger;
  const add = (kind, label, event, index, state = 'running') => {
    const node = {id: `n${nodes.length}`, kind, label, event, index, state};
    nodes.push(node); return node;
  };
  const link = (from, to) => { if (from && to) edges.push({from: from.id, to: to.id}); };
  const finishRound = () => {
    if (!round) return;
    frontier = round.tools.filter(n => n.started || n.finished);
    if (!frontier.length) frontier = [round.model];
  };
  for (const [index, e] of (run.events || []).entries()) {
    if (e.kind === 'run_started') {
      trigger = add('user', '用户触发', e, index, 'success'); frontier = [trigger];
    } else if (e.kind === 'model_started' || (e.kind === 'model_finished' && !round)) {
      finishRound();
      const model = add('model', `第 ${e.step || rounds.length + 1} 轮 · 决策`, e, index);
      frontier.forEach(n => link(n, model));
      round = {model, tools: []}; rounds.push(round);
      if (e.kind === 'model_finished') model.state = e.error ? 'error' : 'success';
    } else if (e.kind === 'model_finished' && round) {
      Object.assign(round.model, {event:e, index, state:e.error ? 'error' : 'success'});
    } else if (['tool_planned', 'tool_started', 'tool_finished'].includes(e.kind)) {
      if (!round) {
        const model = add('model', '决策记录缺失', {}, index, 'unknown');
        frontier.forEach(n => link(n, model)); round = {model, tools:[]}; rounds.push(round);
      }
      let node = e.kind === 'tool_planned' ? null : round.tools.find(n =>
        n.label === e.name && (e.call_id ? n.event.call_id === e.call_id : true) &&
        (e.kind === 'tool_started' ? !n.started && !n.finished : !n.finished));
      if (!node) {
        node = add('tool', e.name || '工具', e, index, 'planned');
        round.tools.push(node); link(round.model, node);
      }
      if (e.kind !== 'tool_planned') {
        if (e.kind === 'tool_started') { node.started = true; node.arguments = e.arguments; }
        if (e.kind === 'tool_finished') node.finished = true;
        Object.assign(node, {event:e, index, state:e.kind === 'tool_started' ? 'running' : e.error ? 'error' : 'success'});
      }
    } else if (e.kind === 'skill_loaded') {
      const tool = round?.tools.findLast(n => n.event.call_id === e.call_id && n.label === 'read_skill');
      if (tool) tool.skill = true;
    } else if (e.kind === 'run_finished') {
      finishRound();
      const end = add('end', e.error ? '运行失败' : '本轮完成', e, index, e.error ? 'error' : 'success');
      frontier.forEach(n => link(n,end)); frontier = [end];
    }
  }
  for (const node of nodes) {
    if (node.state === 'planned' && (run.status !== 'running' || !round?.tools.includes(node))) node.state = 'skipped';
    if (node.state === 'running' && run.status !== 'running') node.state = 'interrupted';
  }
  // Layers stay ordered even when retries reuse the same step or call ID.
  const width = Math.max(900, ...rounds.map(item => item.tools.length * 270 + 30));
  const center = (width - 220) / 2;
  let y = 22;
  const place = (node,x) => { node.x=x; node.y=y; };
  if (trigger) { place(trigger,center); y += 116; }
  for (const item of rounds) {
    place(item.model,center); y += 116;
    // Keep one rank per plan: wrapping branches makes edges cross tool cards.
    const left = (width-item.tools.length*220-(item.tools.length-1)*50)/2;
    item.tools.forEach((node,j)=>place(node,left+j*270));
    if(item.tools.length) y+=116;
  }
  for (const node of nodes.filter(n=>n.kind==='end')) { place(node,center); y+=116; }
  return {nodes, edges, width, height:Math.max(y,150)};
}

function renderRunDAG(run, previous = {}) {
  const graph = buildRunDAG(run), root=el('section','run-dag');
  const heading=el('div','row'), title=el('div');
  title.append(el('h3','','Agent 规划 · DAG'),el('p','muted','真实模型决策 → 计划调用 → 后续决策。分支表示本轮选定的工具，工具仍按顺序执行。'));
  const controls=el('div','dag-controls');
  const minus=el('button','','−'),plus=el('button','','＋'),fit=el('button','','适应');
  ConsoleControls.iconButton(minus,'minus','缩小');ConsoleControls.iconButton(plus,'plus','放大');ConsoleControls.iconButton(fit,'fit','适应视图');
  minus.dataset.focusKey='dag-minus';plus.dataset.focusKey='dag-plus';fit.dataset.focusKey='dag-fit';
  minus.setAttribute('aria-label','缩小规划图');plus.setAttribute('aria-label','放大规划图');
  controls.append(minus,plus,fit);heading.append(title,controls);root.append(heading);
  if (!graph.nodes.length) { root.append(el('p','empty','暂无规划事件；运行开始后自动生成。')); return root; }
  const legend=el('div','dag-legend');
  const states={planned:'待执行',running:'运行中',success:'已完成',error:'失败',skipped:'未执行',interrupted:'已中断',unknown:'未知'};
  for(const state of ['planned','running','success','error']) legend.append(el('span',`dag-state ${state}`,states[state]));
  const viewport=el('div','dag-viewport');viewport.dataset.scrollKey='dag';viewport.tabIndex=0;viewport.setAttribute('aria-label','规划图，可滚动；使用缩放按钮调整大小');
  const spacer=el('div','dag-spacer'),canvas=el('div','dag-canvas');
  canvas.style.width=graph.width+'px';canvas.style.height=graph.height+'px';
  const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
  svg.setAttribute('width',graph.width);svg.setAttribute('height',graph.height);svg.setAttribute('aria-hidden','true');
  const nodeMap=new Map(graph.nodes.map(n=>[n.id,n]));
  for(const edge of graph.edges){
    const a=nodeMap.get(edge.from),b=nodeMap.get(edge.to),x1=a.x+110,x2=b.x+110,y1=a.y+82,y2=b.y;
    const path=document.createElementNS(svg.namespaceURI,'path');
    path.setAttribute('d',`M ${x1} ${y1} C ${x1} ${y1+24}, ${x2} ${y2-28}, ${x2} ${y2-7} l -4 -5 m 4 5 l 4 -5`);
    path.setAttribute('class','dag-edge'+(b.state==='running'?' is-active':''));svg.append(path);
  }
  canvas.append(svg);
  const inspector=el('div','dag-inspector');inspector.setAttribute('aria-live','polite');
  let chosen=previous.node || '',zoom=previous.zoom || 1;
  function inspect(node){
    chosen=node.id;root.dataset.node=chosen;
    canvas.querySelectorAll('button').forEach(b=>b.setAttribute('aria-pressed',b.dataset.node===chosen));
    inspector.replaceChildren(el('strong','',`${node.label} · ${states[node.state]}`));
    const e=node.event;
    if(e.summary)inspector.append(el('p','',e.summary));
    if(e.call_id)inspector.append(el('p','muted',`调用 ID · ${e.call_id}`));
    for(const [label,value]of [['内容',e.text],['工具参数',node.arguments || e.arguments],['工具结果',e.result],['错误',e.error]]){
      if(!value)continue;
      const d=el('details');d.dataset.key=`dag:${run.id}:${node.id}:${label}`;
      d.append(el('summary','',label),el('pre','',pretty(value)));inspector.append(d);
      d.firstChild.dataset.focusKey=d.dataset.key;
    }
  }
  for(const node of graph.nodes){
    const button=el('button',`dag-node dag-${node.kind} ${node.state}`);
    button.dataset.node=node.id;button.dataset.focusKey=`dag:${node.id}`;
    button.style.left=node.x+'px';button.style.top=node.y+'px';
    button.append(el('small','',node.kind==='model'?'◈ MODEL':node.kind==='tool'?(node.skill?'◇ SKILL':'⌘ TOOL'):node.kind==='user'?'◎ INPUT':'✓ RESULT'),el('strong','',node.label),el('span','dag-status',states[node.state]));
    button.title=`${node.label} · ${states[node.state]}`;button.onclick=()=>inspect(node);canvas.append(button);
  }
  spacer.append(canvas);viewport.append(spacer);root.append(legend,viewport,inspector);
  function scale(value){
    zoom=Math.max(.25,Math.min(1.5,value));root.dataset.zoom=zoom;
    canvas.style.transform=`scale(${zoom})`;spacer.style.width=graph.width*zoom+'px';spacer.style.height=graph.height*zoom+'px';
    minus.disabled=zoom<=.25;plus.disabled=zoom>=1.5;fit.textContent=`适应 · ${Math.round(zoom*100)}%`;
  }
  minus.onclick=()=>scale(zoom-.15);plus.onclick=()=>scale(zoom+.15);fit.onclick=()=>scale(Math.min(1,(viewport.clientWidth-24)/graph.width));
  scale(zoom);inspect(nodeMap.get(chosen)||graph.nodes.findLast(n=>n.state==='running')||graph.nodes[graph.nodes.length-1]);
  if(!previous.zoom) requestAnimationFrame(()=>{
    if(!root.isConnected)return;
    scale(Math.min(1,(viewport.clientWidth-24)/graph.width));
  });
  return root;
}
