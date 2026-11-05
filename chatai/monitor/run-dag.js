'use strict';

// The graph describes observed decisions and their requested tools. Fan-out is
// a planning relationship, not a claim that the executor runs tools in parallel.
function buildRunDAG(run, direction = 'vertical') {
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
  const layers = [];
  if (trigger) layers.push([trigger]);
  for (const item of rounds) {
    layers.push([item.model]);
    // Keep one rank per plan: wrapping branches makes edges cross tool cards.
    if (item.tools.length) layers.push(item.tools);
  }
  for (const node of nodes.filter(n=>n.kind==='end')) layers.push([node]);
  const graph = {nodes, edges, layers};
  layoutRunDAG(graph, direction);
  return graph;
}

function runDAGDirection(availableWidth, screenWidth, screenHeight) {
  return availableWidth >= 640 && screenWidth >= screenHeight ? 'horizontal' : 'vertical';
}

function layoutRunDAG(graph, direction) {
  const horizontal = direction === 'horizontal';
  const mainSize = horizontal ? 220 : 82, crossSize = horizontal ? 82 : 220;
  const mainGap = horizontal ? 64 : 34, crossGap = horizontal ? 34 : 50;
  const branches = Math.max(1, ...graph.layers.map(layer => layer.length));
  const cross = branches * crossSize + (branches - 1) * crossGap + 44;
  const main = Math.max(1, graph.layers.length) * (mainSize + mainGap) - mainGap + 44;
  graph.layers.forEach((layer, rank) => {
    const start = (cross - layer.length * crossSize - (layer.length - 1) * crossGap) / 2;
    layer.forEach((node, branch) => {
      const along = 22 + rank * (mainSize + mainGap), across = start + branch * (crossSize + crossGap);
      node.x = horizontal ? along : across;
      node.y = horizontal ? across : along;
    });
  });
  Object.assign(graph, {direction, width: horizontal ? main : cross, height: horizontal ? cross : main});
}

function runDAGEdgePath(a, b, direction) {
  if (direction === 'horizontal') {
    const x1=a.x+220, x2=b.x, y1=a.y+41, y2=b.y+41;
    return `M ${x1} ${y1} C ${x1+28} ${y1}, ${x2-28} ${y2}, ${x2-7} ${y2} l -5 -4 m 5 4 l -5 4`;
  }
  const x1=a.x+110, x2=b.x+110, y1=a.y+82, y2=b.y;
  return `M ${x1} ${y1} C ${x1} ${y1+24}, ${x2} ${y2-28}, ${x2} ${y2-7} l -4 -5 m 4 5 l 4 -5`;
}

function renderRunDAG(run, previous = {}) {
  const graph = buildRunDAG(run), root=el('section','run-dag');
  const heading=el('div','row'), title=el('div');
  title.append(el('h3','','Agent 规划 · DAG'),el('p','muted','分支表示选定工具，工具按顺序执行。'));
  const controls=el('div','dag-controls');
  const minus=el('button','','−'),plus=el('button','','＋'),fit=el('button','','适应');
  ConsoleControls.iconButton(minus,'minus','缩小');ConsoleControls.iconButton(plus,'plus','放大');ConsoleControls.iconButton(fit,'fit','适应视图');
  const zoomReadout=el('output','dag-zoom','100%');
  zoomReadout.setAttribute('aria-label','当前缩放比例');
  minus.dataset.focusKey='dag-minus';plus.dataset.focusKey='dag-plus';fit.dataset.focusKey='dag-fit';
  minus.setAttribute('aria-label','缩小规划图');plus.setAttribute('aria-label','放大规划图');
  controls.append(minus,zoomReadout,plus,fit);heading.append(title,controls);root.append(heading);
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
    const a=nodeMap.get(edge.from),b=nodeMap.get(edge.to);
    const path=document.createElementNS(svg.namespaceURI,'path');
    path.setAttribute('d',runDAGEdgePath(a,b,graph.direction));
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
    minus.disabled=zoom<=.25;plus.disabled=zoom>=1.5;zoomReadout.textContent=`${Math.round(zoom*100)}%`;
  }
  let autoFit = previous.autoFit !== undefined ? previous.autoFit : !previous.zoom;
  root.dataset.autoFit=String(autoFit);
  const fitScale=()=>Math.min(1,(viewport.clientWidth-24)/graph.width);
  const manualScale=value=>{autoFit=false;root.dataset.autoFit='false';scale(value);};
  minus.onclick=()=>manualScale(zoom-.15);plus.onclick=()=>manualScale(zoom+.15);
  fit.onclick=()=>{autoFit=true;root.dataset.autoFit='true';scale(fitScale());};
  scale(zoom);inspect(nodeMap.get(chosen)||graph.nodes.findLast(n=>n.state==='running')||graph.nodes[graph.nodes.length-1]);
  requestAnimationFrame(()=>{
    if(!root.isConnected)return;
    let lastWidth=0, lastDirection='';
    function resize(){
      const width=viewport.clientWidth;
      if(!width)return; // Hidden tabs are laid out when shown again.
      const direction=runDAGDirection(width,window.innerWidth,window.innerHeight);
      if(width===lastWidth && direction===lastDirection)return;
      const changed=!!lastDirection && direction!==lastDirection;
      lastWidth=width;lastDirection=direction;
      layoutRunDAG(graph,direction);root.dataset.direction=direction;
      canvas.style.width=graph.width+'px';canvas.style.height=graph.height+'px';
      svg.setAttribute('width',graph.width);svg.setAttribute('height',graph.height);
      graph.edges.forEach((edge,i)=>svg.children[i].setAttribute('d',runDAGEdgePath(nodeMap.get(edge.from),nodeMap.get(edge.to),direction)));
      canvas.querySelectorAll('button').forEach(button=>{
        const node=nodeMap.get(button.dataset.node);
        button.style.left=node.x+'px';button.style.top=node.y+'px';
      });
      scale(autoFit ? fitScale() : zoom);
      if(changed){viewport.scrollLeft=0;viewport.scrollTop=0;}
    }
    const observer=new ResizeObserver(resize);observer.observe(viewport);
    window.addEventListener('resize',resize);
    const removal=new MutationObserver(()=>{
      if(root.isConnected)return;
      observer.disconnect();removal.disconnect();window.removeEventListener('resize',resize);
    });
    removal.observe(document.body,{childList:true,subtree:true});
    resize();
  });
  return root;
}
