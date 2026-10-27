'use strict';

function overviewJSON(text) {
  try { return JSON.parse(text); } catch { return null; }
}

// Pair starts and finishes so a call is counted once, including unfinished calls.
function summarizeRun(run) {
  const skills = new Map(), tools = new Map(), calls = [], outputs = [];
  let finalAnswer = '';
  for (const [index, event] of (run.events || []).entries()) {
    if (event.kind === 'skill_loaded') {
      const args = overviewJSON(event.arguments);
      const name = event.name === 'read_skill' ? args?.name || 'read_skill（名称未保留）' : event.name;
      skills.set(name, (skills.get(name) || 0) + 1);
    }
    if (event.kind === 'tool_started') calls.push({name: event.name, start: event, index});
    if (event.kind === 'tool_finished') {
      let call = calls.find(c => !c.end && c.name === event.name &&
        (event.call_id ? c.start?.call_id === event.call_id : c.start?.step === event.step));
      if (!call) { call = {name: event.name, index}; calls.push(call); }
      call.end = event;
    }
    if (event.kind === 'run_finished') finalAnswer = event.text || '';
  }
  for (const call of calls) {
    const stats = tools.get(call.name) || {name: call.name, total: 0, success: 0, error: 0, pending: 0};
    stats.total++;
    stats[!call.end ? 'pending' : call.end.error ? 'error' : 'success']++;
    tools.set(call.name, stats);
    if (!call.end) continue;
    const args = overviewJSON(call.start?.arguments), result = overviewJSON(call.end.result);
    let title = call.name, text = '';
    if (['send_message', 'send_raw_message', 'send_messages', 'at_user', 'send_image'].includes(call.name)) {
      title = call.name === 'send_image' ? '图片发送' : '消息发送';
      text = [args?.text, ...(Array.isArray(args?.messages) ? args.messages : []), args?.url].filter(v => typeof v === 'string').join('\n');
      if (call.name === 'send_raw_message' && Array.isArray(args?.message)) {
        text = args.message.map(segment => segment?.type === 'text' ? segment.data?.text || '' : JSON.stringify(segment, null, 2)).join('\n');
      }
    } else if (call.name === 'run_skill_script') {
      title = '脚本输出';
      text = [result?.stdout, result?.output, result?.stderr].filter(v => typeof v === 'string' && v).join('\n');
    }
    if (text || (call.end.result && call.end.result !== 'null') || call.end.error) {
      outputs.push({...call, title, text, result: call.end.result, error: call.end.error});
    }
  }
  return {skills: [...skills], tools: [...tools.values()], calls, outputs, finalAnswer};
}

function renderRunOverview(run, opened) {
  const data = summarizeRun(run), root = el('section', 'run-overview');
  root.setAttribute('aria-label', '对话总览');
  const title = el('div', 'row');
  title.append(el('h3', '', '对话总览'), badge(run.status));
  root.append(title, el('p', 'muted', `${data.skills.length} 个 Skill · ${data.tools.length} 种 Tool · ${data.calls.length} 次调用`));
  const result = el('div', 'overview-result');
  result.append(el('h4', '', '执行结果'));
  if (run.error) result.append(el('p', 'warning', run.error));
  result.append(el('p', 'overview-text', data.finalAnswer || (run.status === 'running' ? '正在执行，尚无最终回复。' : '本轮未记录最终文本回复，可查看下方消息和工具产出。')));
  if (run.status === 'running' && run.summary) result.append(el('p', 'stage-summary', run.summary));
  root.append(result, renderRunTiming(run));
  root.append(el('h4', '', 'Skills · 已加载'));
  const skills = el('div', 'overview-tags');
  for (const [name, count] of data.skills) skills.append(el('span', 'badge', `${name} × ${count}`));
  if (!data.skills.length) skills.append(el('span', 'muted', '尚无技能加载记录'));
  root.append(skills, el('h4', '', 'Tools · 调用情况'));
  const toolList = el('div', 'overview-tools');
  for (const tool of data.tools) {
    const row = el('div', 'overview-tool');
    row.append(el('strong', '', `${tool.name} × ${tool.total}`));
    const counts = el('div', 'overview-tags');
    if (tool.success) counts.append(el('span', 'badge success', `成功 ${tool.success}`));
    if (tool.error) counts.append(el('span', 'badge error', `失败 ${tool.error}`));
    if (tool.pending) counts.append(el('span', 'badge running', `${run.status === 'running' ? '执行中' : '未记录完成'} ${tool.pending}`));
    row.append(counts); toolList.append(row);
  }
  if (!data.tools.length) toolList.append(el('span', 'muted', '尚无工具调用记录'));
  root.append(toolList, el('h4', '', '产物与工具结果'));
  root.append(el('p', 'muted', '展示已记录的消息、图片来源、脚本输出及其他工具返回；工具返回不代表生成了持久文件。'));
  for (const output of data.outputs) {
    const card = el('article', 'overview-output'), heading = el('div', 'row');
    heading.append(el('strong', '', output.title), badge(output.error ? 'error' : 'success'));
    card.append(heading, el('small', 'muted', output.name));
    if (output.text) card.append(el('p', 'overview-text', output.text));
    if (output.error) card.append(el('p', 'warning', output.error));
    if (output.result) card.append(disclosure('查看工具返回', pretty(output.result), `${run.id}:overview:${output.index}`, opened));
    root.append(card);
  }
  if (!data.outputs.length) root.append(el('p', 'muted', '尚无工具产出记录'));
  return root;
}

// Use the recorded intervals, not the position of unrelated events. A skill
// read is one tool call; skill_loaded notifications must not count it twice.
function runTiming(run) {
  const events = run.events || [], calls = [], markers = [];
  const timestamp = value => {
    const n = Date.parse(value);
    return Number.isFinite(n) ? n : null;
  };
  const eventTimes = events.map(e => timestamp(e.time)).filter(t => t !== null);
  const origin = timestamp(run.started_at) ?? (eventTimes.length ? Math.min(...eventTimes) : 0);
  const elapsed = Math.max(0, Number(run.duration_ms) || 0);
  const finish = [...events].reverse().find(e => e.kind === 'run_finished');
  const boundary = timestamp(finish?.time) ?? Math.max(origin + elapsed, ...eventTimes);
  for (const [index, event] of events.entries()) {
    const type = event.kind.startsWith('model_') ? 'model' : 'tool';
    if (event.kind === 'model_started' || event.kind === 'tool_started') {
      calls.push({type, start: event, index});
    } else if (event.kind === 'model_finished' || event.kind === 'tool_finished') {
      let call = calls.find(c => c.type === type && !c.end &&
        (type === 'model' ? c.start.step === event.step : c.start.name === event.name &&
          (event.call_id ? c.start.call_id === event.call_id : c.start.step === event.step)));
      if (!call) { call = {type, index}; calls.push(call); }
      call.end = event;
    } else if (event.kind === 'skill_loaded' && event.name !== 'read_skill') {
      // Skills returned inside search_tools have no independent duration.
      markers.push({event, index});
    }
  }
  const rows = [];
  let untimed = 0;
  for (const call of calls) {
    const event = call.start || call.end, args = overviewJSON(call.start?.arguments);
    const skill = call.type === 'tool' && event.name === 'read_skill';
    const kind = call.type === 'model' ? 'model' : skill ? 'skill' : 'tool';
    const label = kind === 'model' ? '思考 / 模型响应' : skill
      ? `Skill · ${args?.name || '名称未保留'} / ${args ? args.file || 'SKILL.md' : '文件未保留'}`
      : `Tool · ${event.name || '名称未保留'}`;
    let start = timestamp(call.start?.time), end = timestamp(call.end?.time);
    const recordedDuration = Number(call.end?.duration_ms);
    if (start === null && end !== null && Number.isFinite(recordedDuration) && recordedDuration >= 0) start = end - recordedDuration;
    if (end === null && call.end && start !== null && Number.isFinite(recordedDuration) && recordedDuration >= 0) end = start + recordedDuration;
    if (!call.end) end = boundary;
    if (start === null || end === null || end < start) { untimed++; continue; }
    rows.push({key: `${kind}:${label}`, kind, label, index: call.index,
      start: Math.max(0, start - origin), end: Math.max(0, end - origin),
      duration: end - start, step: event.step, callID: event.call_id,
      state: !call.end ? run.status === 'running' ? '执行中 · 暂计' : '未记录完成 · 估算' : call.end.error ? '失败' : '完成',
      estimated: !call.end});
  }
  for (const {event, index} of markers) {
    const t = timestamp(event.time);
    if (t === null) { untimed++; continue; }
    const label = `Skill · ${event.name || '名称未保留'}`;
    rows.push({key: `skill:${label}`, kind: 'skill', label, index,
      start: Math.max(0, t - origin), end: Math.max(0, t - origin), duration: 0,
      step: event.step, callID: event.call_id, state: '随工具加载 · 无独立耗时', marker: true});
  }
  rows.sort((a, b) => a.start - b.start || a.index - b.index);
  const groups = new Map();
  for (const row of rows) {
    if (!groups.has(row.key)) groups.set(row.key, {key: row.key, kind: row.kind, label: row.label, duration: 0, count: 0, estimated: false});
    const group = groups.get(row.key);
    group.duration += row.duration;
    group.count++;
    group.estimated ||= !!row.estimated;
  }
  // Allocate separate hues within each family, consistently between both charts.
  const palette = {model: 215, skill: 275, tool: 15};
  for (const kind of ['model', 'skill', 'tool']) {
    const keys = [...groups.keys()].filter(key => groups.get(key).kind === kind);
    keys.forEach((key, i) => {
      groups.get(key).color = `hsl(${palette[kind] + (kind === 'tool' ? i * 61 % 160 : i * 29 % 65)} 68% 55%)`;
    });
  }
  for (const row of rows) row.color = groups.get(row.key).color;
  const categories = [...groups.values()].sort((a, b) => b.duration - a.duration);
  return {origin, rows, categories, untimed, total: categories.reduce((n, c) => n + c.duration, 0),
    span: Math.max(elapsed, boundary - origin, ...rows.map(r => r.end), 0)};
}

function timingDuration(ms) {
  return ms === 0 ? '0 ms' : ms < 1000 ? `${Math.round(ms)} ms` : ms < 60000 ? `${(ms / 1000).toFixed(2)} s` : `${(ms / 60000).toFixed(2)} min`;
}

function timingSwatch(color) {
  const swatch = el('span', 'timing-swatch');
  swatch.style.backgroundColor = color;
  swatch.setAttribute('aria-hidden', 'true');
  return swatch;
}

function renderRunTiming(run) {
  const data = runTiming(run), root = el('section', 'run-timing');
  root.setAttribute('aria-label', 'Agent 耗时分析');
  root.append(el('h4', '', '耗时分析'), el('p', 'muted', `本轮经过 ${timingDuration(data.span)} · 已记录阶段累计 ${timingDuration(data.total)}`));
  root.append(el('p', 'timing-note', '思考 / 模型响应包含接口等待、推理和生成，无法单独拆出纯思考时间。Skill 按名称和文件区分，Tool 按工具名称区分；每次调用单独展示。'));
  if (!data.rows.length) {
    root.append(el('p', 'muted', '暂无可用的阶段时间记录。'));
    return root;
  }
  const chart = el('div', 'timing-distribution'), pie = el('div', 'timing-pie');
  pie.setAttribute('role', 'img');
  pie.setAttribute('aria-label', data.total ? `耗时占比饼图，${data.categories.filter(c => c.duration > 0).map(c => `${c.label} ${(c.duration / data.total * 100).toFixed(1)}%`).join('；')}` : '暂无可统计的耗时');
  let angle = 0;
  const slices = data.categories.filter(c => c.duration > 0).map(c => {
    const start = angle; angle += c.duration / data.total * 360;
    return `${c.color} ${start}deg ${angle}deg`;
  });
  if (slices.length) pie.style.background = `conic-gradient(${slices.join(',')})`;
  else pie.append(el('span', 'muted', '暂无耗时'));
  const ranking = el('div', 'timing-ranking');
  ranking.append(el('strong', '', '耗时占比 · 从高到低'));
  for (const [index, category] of data.categories.entries()) {
    const row = el('div', 'timing-category');
    row.append(timingSwatch(category.color), el('span', 'timing-label', category.label));
    const value = category.duration > 0 ? `${timingDuration(category.duration)} · ${(category.duration / data.total * 100).toFixed(1)}%` : '0 ms / 仅事件';
    row.append(el('small', '', `${value} · ${category.count} 次${category.estimated ? ' · 暂计' : ''}${index === 0 && category.duration > 0 ? ' · 最耗时' : ''}`));
    ranking.append(row);
  }
  chart.append(pie, ranking); root.append(chart);
  root.append(el('p', 'timing-note', '饼图按已记录阶段累计耗时计算；并行阶段会分别累计，阶段间空档不计入。虚线表示尚无完成事件，耗时暂计至本轮结束或本次刷新；随搜索加载的 Skill 仅标记时间点，不重复计时。'));
  if (data.untimed) root.append(el('p', 'warning', `${data.untimed} 个阶段缺少有效时间，未计入图表。`));
  root.append(el('h4', '', '执行甘特图'));
  const scroll = el('div', 'timing-scroll');
  scroll.tabIndex = 0; scroll.setAttribute('role', 'region'); scroll.setAttribute('aria-label', '甘特图，可横向滚动查看');
  const gantt = el('div', 'timing-gantt'), axis = el('div', 'timing-axis');
  const selection = el('p', 'timing-selection', '悬停或聚焦色条，查看该阶段的起止时间和调用 ID。');
  selection.setAttribute('aria-live', 'polite');
  const scale = Math.max(1, data.span);
  for (let i = 0; i <= 4; i++) axis.append(el('span', '', `+${timingDuration(data.span * i / 4)}`));
  gantt.append(axis);
  for (const row of data.rows) {
    const item = el('div', 'timing-row'), label = el('div', 'timing-row-label');
    const caption = `${row.step ? `第 ${row.step} 轮 · ` : ''}${timingDuration(row.duration)} · ${row.state}`;
    label.append(timingSwatch(row.color), el('span', '', row.label), el('small', 'muted', caption));
    const track = el('div', 'timing-track'), bar = el('div', 'timing-bar' + (row.estimated ? ' timing-pending' : '') + (row.duration === 0 ? ' timing-point' : ''));
    bar.style.left = `${Math.min(100, row.start / scale * 100)}%`;
    bar.style.width = `${Math.min(100, row.duration / scale * 100)}%`;
    bar.style.backgroundColor = row.color;
    bar.tabIndex = 0;
    const clock = offset => new Date(data.origin + offset).toLocaleTimeString('zh-CN', {hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3});
    bar.title = `${row.label}\n${clock(row.start)} → ${clock(row.end)}\n+${timingDuration(row.start)} → +${timingDuration(row.end)}\n${caption}${row.callID ? '\n调用 ID：' + row.callID : ''}`;
    bar.setAttribute('role', 'img'); bar.setAttribute('aria-label', bar.title);
    bar.onmouseenter = bar.onfocus = () => { selection.textContent = bar.title; };
    track.append(bar); item.append(label, track); gantt.append(item);
  }
  scroll.append(gantt); root.append(scroll, selection);
  root.append(el('p', 'timing-note', '横轴为相对开始时间；悬停或聚焦色条可查看起止时间与调用 ID。短于刻度的调用以最小宽度显示。'));
  return root;
}
