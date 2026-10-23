// Scheduling shares the console's authenticated API, group picker and dialog styles.
const scheduleLabels={active:'等待执行',running:'执行中',completed:'已完成',failed:'失败',interrupted:'已中断',success:'成功'};
function scheduleBadge(status){return el('span','badge '+({failed:'error',interrupted:'error',completed:'success'}[status]||status),scheduleLabels[status]||status)}
function renderSchedules(root,data){
 $('catalog-status').textContent='任务与执行记录持久保存 · 每 5 秒更新 · 离线任务等待连接恢复';
 root.append(action('＋ 新增任务',()=>createSchedule(data.bots)));
 section(root,'定时任务',data.items,t=>{
  const c=card(t.name,t.instruction,t.cron?'周期任务':'一次性任务');
  c.append(scheduleBadge(t.status),el('p','meta',`#${t.id} · 用户 ${t.user_id} · 机器人 ${t.bot_id}`));
  if(t.cron)c.append(el('p','',`${t.cron} · ${t.timezone}`));
  c.append(el('p','',t.next_run_at?`下次执行：${time(t.next_run_at)}（浏览器本地时间）`:'无后续执行'));
  if(t.status==='active'&&t.next_run_at&&Date.parse(t.next_run_at)<Date.now())c.append(el('p','muted','已到期，等待机器人在线及执行槽位。'));
  const recent=el('details');recent.open=true;recent.append(el('summary','',`近期执行 · ${t.runs.length} 条`));
  for(const r of t.runs){const row=el('div','memory-item');row.append(scheduleBadge(r.status),el('div','meta',`计划 ${time(r.scheduled_at)} · 开始 ${time(r.started_at)}`),el('div','meta',r.finished_at?`结束 ${time(r.finished_at)} · ${duration(Date.parse(r.finished_at)-Date.parse(r.started_at))}`:'执行尚未结束'));if(r.error)row.append(el('p','warning',r.error));recent.append(row)}
  if(!t.runs.length)recent.append(el('p','muted','尚未执行'));
  c.append(recent,action('删除任务',()=>deleteSchedule(t),true));return c;
 });
 $('pagination').hidden=false;$('previous').disabled=offset===0;$('next').disabled=!data.has_more;$('page-number').textContent=`第 ${offset/50+1} 页`;
}
function scheduleDialog(title){
 const dialog=el('dialog','memory-dialog'),heading=el('h2','',title);heading.id='schedule-dialog-title';dialog.setAttribute('aria-labelledby',heading.id);
 const form=el('form'),error=el('p','warning');error.setAttribute('role','alert');dialog.append(heading,form);dialog.onclose=()=>dialog.remove();document.body.append(dialog);
 return {dialog,form,error};
}
function createSchedule(bots){
 const group=$('group').value,{dialog,form,error}=scheduleDialog('新增定时任务');
 form.append(el('p','muted',`群 ${group} · 到期后由 Agent 按任务说明执行。`));
 const name=field(form,'任务名称','','text',60);name.required=true;
 const instruction=field(form,'任务说明','','textarea',4000);instruction.required=true;
 const user=field(form,'发起用户 QQ','','text',20);user.required=true;user.pattern='[1-9][0-9]*';
 const bot=field(form,'机器人 QQ',bots.length===1?bots[0]:'','text',20);bot.required=true;bot.pattern='[1-9][0-9]*';
 if(bots.length)form.append(el('p','muted','当前在线机器人：'+bots.join('、')));
 const mode=field(form,'执行方式','','select');mode.replaceChildren();for(const [v,label]of [['once','一次性'],['cron','周期 cron']]){const o=el('option','',label);o.value=v;mode.append(o)}
 const at=field(form,'执行时间（浏览器本地时间）','','datetime-local');at.required=true;
 const cron=field(form,'Cron（分 时 日 月 星期）','0 9 * * *','text',100);
 const zone=field(form,'Cron 时区','Asia/Shanghai','text',100);
 const hint=el('p','muted','示例：0 9 * * 1-5 = 工作日 9 点；*/30 * * * * = 每半小时。');form.append(hint);
 const toggle=()=>{const periodic=mode.value==='cron';at.parentElement.hidden=periodic;at.required=!periodic;cron.parentElement.hidden=!periodic;zone.parentElement.hidden=!periodic;hint.hidden=!periodic;cron.required=periodic;zone.required=periodic};mode.onchange=toggle;toggle();
 const buttons=el('div','actions'),save=el('button','primary','创建任务');save.type='submit';buttons.append(action('取消',()=>dialog.close()),save);form.append(error,buttons);
 form.onsubmit=async e=>{e.preventDefault();save.disabled=true;error.textContent='';try{
  const payload={name:name.value,instruction:instruction.value,user_id:user.value.trim(),bot_id:bot.value.trim()};
  if(mode.value==='cron'){payload.cron=cron.value.trim();payload.timezone=zone.value.trim()}else{const date=new Date(at.value);if(!Number.isFinite(date.getTime())||date<=new Date())throw Error('请选择未来的执行时间');payload.run_at=date.toISOString()}
  await api(`/api/inspect/schedules?group=${encodeURIComponent(group)}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});dialog.close();if(currentView==='schedules'&&$('group').value===group){offset=0;await loadCatalog()}
 }catch(e){error.textContent=e.message}finally{save.disabled=false}};
 dialog.showModal();name.focus();
}
function deleteSchedule(task){
 const group=$('group').value,{dialog,form,error}=scheduleDialog('删除任务');form.append(el('p','',`确认删除「${task.name}」？后续将不再触发，已经开始的执行可能仍会完成。`));
 const buttons=el('div','actions'),remove=el('button','danger','确认删除');remove.type='submit';buttons.append(action('取消',()=>dialog.close()),remove);form.append(error,buttons);
 form.onsubmit=async e=>{e.preventDefault();remove.disabled=true;try{await api(`/api/inspect/schedules?group=${encodeURIComponent(group)}`,{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({task_id:task.id})});dialog.close();if(currentView==='schedules'&&$('group').value===group)await loadCatalog()}catch(e){error.textContent=e.message}finally{remove.disabled=false}};dialog.showModal();
}
// Refresh only the schedule view, without interrupting open forms.
setInterval(async()=>{
 if(!authenticated||currentView!=='schedules'||document.querySelector('dialog[open]')||!$('group').value||document.hidden)return;
 const serial=catalogSerial,group=$('group').value,page=offset;
 try{const data=await api(`/api/inspect/schedules?group=${encodeURIComponent(group)}&offset=${page}`);if(serial!==catalogSerial||!authenticated||currentView!=='schedules'||document.querySelector('dialog[open]'))return;if(JSON.stringify(catalogData)!==JSON.stringify(data)){catalogData=data;renderCatalog()}}catch(e){if(serial===catalogSerial)$('catalog-status').textContent=e.message}
},5000);
