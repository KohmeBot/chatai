// Scheduling shares the console's authenticated API, group picker and dialog styles.
const scheduleLabels={active:'等待执行',running:'执行中',completed:'已完成',failed:'失败',interrupted:'已中断',success:'成功'};
function scheduleBadge(status){return el('span','badge '+({failed:'error',interrupted:'error',completed:'success'}[status]||status),scheduleLabels[status]||status)}
function renderSchedules(root,data){
 $('catalog-status').textContent='任务与执行记录持久保存 · 每 5 秒更新 · 离线任务等待连接恢复';
 root.append(action('＋ 新增任务',()=>createSchedule(data.bots)));
 section(root,'定时任务',data.items,t=>{
  const c=card(t.name,t.instruction,t.cron?'周期任务':'一次性任务');c.classList.add('catalog-summary');
  c.append(scheduleBadge(t.status),el('p','meta',`#${t.id} · 用户 ${t.user_id} · 机器人 ${t.bot_id}`));
  if(t.cron)c.append(el('p','',`${t.cron} · ${t.timezone}`));
  c.append(el('p','',t.next_run_at?`下次执行：${time(t.next_run_at)}（浏览器本地时间）`:'无后续执行'));
  if(t.status==='active'&&t.next_run_at&&Date.parse(t.next_run_at)<Date.now())c.append(el('p','muted','已到期，等待机器人在线及执行槽位。'));
  const buttons=el('div','actions');buttons.append(action('详情',()=>showScheduleDetail(t)),action('删除任务',()=>deleteSchedule(t),true));c.append(buttons);return c;
 });
 $('pagination').hidden=false;$('previous').disabled=offset===0;$('next').disabled=!data.has_more;$('page-number').textContent=`第 ${offset/50+1} 页`;
}
function showScheduleDetail(task){
 const {body}=resourceDialog(task.name,'定时任务详情');
 body.append(scheduleBadge(task.status),el('h3','section-heading','任务说明'),el('p','resource-description',task.instruction),el('h3','section-heading','执行配置'),detailFields([
  ['任务 ID',task.id],['执行方式',task.cron?'周期任务':'一次性任务'],['所属群',task.group_id],['发起用户',task.user_id],['机器人',task.bot_id],['创建时间',time(task.created_at)],
  ...(task.cron?[['Cron 表达式',task.cron],['调度时区',task.timezone]]:[]),['下次执行',task.next_run_at?time(task.next_run_at)+'（浏览器本地时间）':'无后续执行']
 ]));
 if(task.cron){const parts=task.cron.trim().split(/\s+/);body.append(el('h4','','周期规则'),detailFields(['分钟','小时','日期','月份','星期'].map((label,i)=>[label,parts[i]==='*'?'每个'+label:parts[i]])),el('p','muted','* 表示任意值，逗号表示多个值，连字符表示范围，/ 表示间隔；星期 0 表示周日。'))}
 if(task.status==='active'&&task.next_run_at&&Date.parse(task.next_run_at)<Date.now())body.append(el('p','muted','已到期，等待机器人在线及执行槽位。'));
 body.append(el('h3','section-heading',`近期执行 · ${(task.runs||[]).length} 条`),el('p','muted','时间按浏览器本地时区展示；关闭详情后列表恢复自动更新。'));
 for(const run of task.runs||[]){const row=el('section','execution-record');row.append(scheduleBadge(run.status),detailFields([['计划时间',time(run.scheduled_at)],['开始时间',time(run.started_at)],['结束时间',run.finished_at?time(run.finished_at):'执行尚未结束'],['耗时',run.finished_at?duration(Date.parse(run.finished_at)-Date.parse(run.started_at)):'进行中']]));if(run.error)row.append(el('p','warning',run.error));body.append(row)}
 if(!task.runs?.length)body.append(el('p','muted','尚未执行'));
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
 const buttons=el('div','actions'),save=el('button','primary','创建任务');save.type='submit';buttons.append(action('取消',()=>ConsoleMotion.closeDialog(dialog)),save);form.append(error,buttons);
 form.onsubmit=async e=>{e.preventDefault();save.disabled=true;error.textContent='';try{
  const payload={name:name.value,instruction:instruction.value,user_id:user.value.trim(),bot_id:bot.value.trim()};
  if(mode.value==='cron'){payload.cron=cron.value.trim();payload.timezone=zone.value.trim()}else{const date=new Date(at.value);if(!Number.isFinite(date.getTime())||date<=new Date())throw Error('请选择未来的执行时间');payload.run_at=date.toISOString()}
  await api(`/api/inspect/schedules?group=${encodeURIComponent(group)}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});ConsoleMotion.closeDialog(dialog);if(currentView==='schedules'&&$('group').value===group){offset=0;await loadCatalog()}
 }catch(e){error.textContent=e.message}finally{save.disabled=false}};
 ConsoleMotion.openDialog(dialog);name.focus();
}
function deleteSchedule(task){
 const group=$('group').value,{dialog,form,error}=scheduleDialog('删除任务');form.append(el('p','',`确认删除「${task.name}」？后续将不再触发，已经开始的执行可能仍会完成。`));
 const buttons=el('div','actions'),remove=el('button','danger','确认删除');remove.type='submit';buttons.append(action('取消',()=>ConsoleMotion.closeDialog(dialog)),remove);form.append(error,buttons);
 form.onsubmit=async e=>{e.preventDefault();remove.disabled=true;try{await api(`/api/inspect/schedules?group=${encodeURIComponent(group)}`,{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({task_id:task.id})});ConsoleMotion.closeDialog(dialog);if(currentView==='schedules'&&$('group').value===group)await loadCatalog()}catch(e){error.textContent=e.message}finally{remove.disabled=false}};ConsoleMotion.openDialog(dialog);
}
// Refresh only the schedule view, without interrupting open forms.
setInterval(async()=>{
 if(!authenticated||currentView!=='schedules'||document.querySelector('dialog[open]')||!$('group').value||document.hidden)return;
 const serial=catalogSerial,group=$('group').value,page=offset;
 try{const data=await api(`/api/inspect/schedules?group=${encodeURIComponent(group)}&offset=${page}`);if(serial!==catalogSerial||!authenticated||currentView!=='schedules'||document.querySelector('dialog[open]'))return;if(JSON.stringify(catalogData)!==JSON.stringify(data)){catalogData=data;renderCatalog()}}catch(e){if(serial===catalogSerial)$('catalog-status').textContent=e.message}
},5000);
