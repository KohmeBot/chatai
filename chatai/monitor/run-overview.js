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
  root.append(result);
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
