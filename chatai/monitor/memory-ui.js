// Shared identity presentation. URLs are assembled locally, never by the server.
function identity(kind,id,name=''){
 const box=el('span','identity'),avatar=el('span','avatar',kind==='group'?'群':'人');avatar.setAttribute('aria-hidden','true');
 if(/^\d+$/.test(String(id))&&String(id)!=='0'){
  const img=el('img');img.alt='';img.loading='lazy';img.referrerPolicy='no-referrer';
  img.src=kind==='group'?`https://p.qlogo.cn/gh/${id}/${id}/640/`:`https://q1.qlogo.cn/g?b=qq&nk=${id}&s=640`;
  img.onerror=()=>img.remove();avatar.append(img);
 }
 box.append(avatar,el('span','',`${kind==='group'?'群':'用户'} ${name?name+' · ':''}${id}`));return box;
}
function identifyCard(c,kind,id){c.querySelector('h3').replaceChildren(identity(kind,id));return c}
function action(text,fn,danger=false){const b=el('button',danger?'danger':'',text);b.type='button';b.onclick=fn;return b}
function memoryActions(c,u){
 const row=el('div','actions');row.append(action('编辑简介与关系',()=>editProfile(u)),action('清空长期记忆',()=>confirmMemory('清空该用户全部长期记忆？','此操作会清空简介、关系和所有群中的结构化条目，影响所有群聊。之后仍可从新消息形成记忆。',{target:'profile',user_id:u.user_id,version:u.version}),true));c.append(row);
}
function groupActions(c,content){if(!content)return;const row=el('div','actions');row.append(action('修改群印象',()=>openMemoryDialog('修改群印象','只修改已有群印象。',form=>{const text=field(form,'群印象',content,'textarea',4000);return()=>({target:'group',previous:content,text:text.value})})),action('删除',()=>confirmMemory('删除该群印象？','删除后将不再向 Agent 提供这段群印象。',{target:'group',previous:content}),true));c.append(row)}
function itemActions(node,u,item){const row=el('div','actions');row.append(action('修改',()=>openMemoryDialog('修改结构化记忆','键、类型、来源和所属群保持不变。',form=>{
 form.append(el('p','muted',`键：${item.key} · 类型：${item.kind} · 来源 #${item.source_id}`),identity('group',String(item.group_id)));
 const text=field(form,'记忆内容',item.text,'textarea',200),confidence=field(form,'可信度',item.confidence,'select');
 const expiry=field(form,'到期时间（可选，含时区）',item.expires_at||'','text');expiry.placeholder='2026-12-31T23:59:59+08:00';
 return()=>{if(expiry.value&&!Number.isFinite(Date.parse(expiry.value)))throw Error('请输入有效的到期时间，例如 2026-12-31T23:59:59+08:00');return{target:'item',user_id:u.user_id,version:u.version,key:item.key,kind:item.kind,text:text.value,confidence:confidence.value,expires_at:expiry.value?new Date(expiry.value).toISOString():null}};
 })),action('删除',()=>confirmMemory('删除这条记忆？',`“${item.text}”将被删除。简介独立保存，如有重复内容，请同时修改简介。`,{target:'item',user_id:u.user_id,version:u.version,key:item.key}),true));node.append(row)}
function editProfile(u){openMemoryDialog('编辑用户简介与关系','用户简介与关系跨群共享。结构化条目请在卡片中逐条修改。',form=>{
 form.append(identity('user',u.user_id));const summary=field(form,'核心简介',u.summary||'','textarea',600),score=field(form,'好感度（-100 至 1000）',u.relationship.score,'number');score.min=-100;score.max=1000;score.step=1;score.required=true;
 const description=field(form,'关系描述',u.relationship.description||'','textarea',200),reason=field(form,'关系原因',u.relationship.reason||'','textarea',200);
 return()=>({target:'profile',user_id:u.user_id,version:u.version,summary:summary.value,score:Number(score.value),description:description.value,reason:reason.value});
 })}
let fieldCounter=0;
function field(form,label,value,type,max){const wrap=el('div','field'),id='memory-field-'+(++fieldCounter),lab=el('label','',label);lab.htmlFor=id;let input;
 if(type==='textarea')input=el('textarea');else if(type==='select'){input=el('select');for(const [v,t]of [['explicit','明确表达'],['inferred','推断']]){const o=el('option','',t);o.value=v;input.append(o)}}else{input=el('input');input.type=type}
 input.id=id;input.value=value;if(max)input.maxLength=max;wrap.append(lab,input);form.append(wrap);return input;
}
function confirmMemory(title,description,payload){openMemoryDialog(title,description,()=>()=>payload,true)}
function openMemoryDialog(title,description,build,remove=false){
 const group=$('group').value,dialog=el('dialog','memory-dialog'),form=el('form'),heading=el('h2','',title),error=el('p','form-error'),buttons=el('div','actions');
 heading.id='memory-dialog-title';dialog.setAttribute('aria-labelledby',heading.id);error.setAttribute('role','alert');form.append(heading,el('p','muted',description));const payload=build(form);
 const cancel=action('取消',()=>dialog.close()),save=el('button',remove?'danger':'primary',remove?'确认删除':'保存修改');save.type='submit';buttons.append(cancel,save);form.append(error,buttons);dialog.append(form);document.body.append(dialog);
 let busy=false;dialog.oncancel=e=>{if(busy)e.preventDefault()};dialog.onclose=()=>dialog.remove();
 form.onsubmit=async e=>{e.preventDefault();if(busy)return;try{const data=payload();busy=true;save.disabled=cancel.disabled=true;save.textContent='正在保存…';error.textContent='';await api(`/api/inspect/memory?group=${encodeURIComponent(group)}`,{method:remove?'DELETE':'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(data),signal:AbortSignal.timeout(15000)});dialog.close();if(authenticated&&currentView==='memory'&&$('group').value===group)await loadCatalog();notifyMemory(remove?'记忆已删除':'修改已保存')}catch(err){error.textContent=err.message+'；如数据已变化，请取消并刷新后重试。'}finally{busy=false;save.disabled=cancel.disabled=false;save.textContent=remove?'确认删除':'保存修改'}};
 dialog.showModal();(remove?cancel:form.querySelector('textarea,input,select')||cancel).focus();
}
function notifyMemory(message){const toast=el('div','toast',message);toast.setAttribute('role','status');document.body.append(toast);setTimeout(()=>toast.remove(),3500)}
function updateGroupIdentity(){const target=$('group-identity');target.replaceChildren();if($('group').value)target.append(identity('group',$('group').value))}
