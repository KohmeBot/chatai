// Catalog presentation. All user-controlled content is rendered as DOM text.
let resourceDialogSerial=0;
function resourceDialog(title,description){
 const dialog=el('dialog','memory-dialog resource-detail'),head=el('div','row resource-heading'),heading=el('h2','',title);
 heading.id=`resource-title-${++resourceDialogSerial}`;dialog.setAttribute('aria-labelledby',heading.id);
 const close=action('关闭',()=>ConsoleMotion.closeDialog(dialog));head.append(heading,close);
 const body=el('div','resource-body');dialog.append(head);if(description)body.append(el('p','resource-description',description));dialog.append(body);
 dialog.onclose=()=>dialog.remove();document.body.append(dialog);ConsoleMotion.openDialog(dialog);close.focus();return {dialog,body};
}
function detailFields(entries){
 const list=el('dl','detail-fields');for(const [label,value] of entries){list.append(el('dt','',label),el('dd','',value===undefined||value===null||value===''?'未提供':String(value)))}return list;
}
function metadataList(root,label,values){
 root.append(el('h4','',label));if(!values?.length){root.append(el('p','muted','未声明'));return}
 const list=el('ul');for(const value of values)list.append(el('li','',value));root.append(list);
}
function renderSkills(root,data){
 $('catalog-status').textContent=data.enabled?`${data.items.length} 项可用技能 · 未激活、已退役和已过期的经验技能不在此列`:'Skills 功能未启用';
 root.append(el('p','muted','常驻技能会在每次运行的首轮请求中带入完整正文并加载关联工具，减少读取技能的额外调用，但会增加输入 token。全局技能的设置对所有群生效，保存后下次运行生效。'));
 const group=$('group').value;
 section(root,'可用技能',data.items,s=>{const c=card(s.name,s.description,s.source);c.classList.add('catalog-summary');if(s.path)c.append(el('div','meta',s.path));const buttons=el('div','actions'),status=el('span','muted');status.setAttribute('role','status');const toggle=action('',async()=>{
  toggle.disabled=true;status.textContent='保存中…';
  try{const saved=await api(`/api/inspect/skills?group=${encodeURIComponent(group)}`,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({...s.id?{id:s.id}:{name:s.name},resident:!s.resident})});s.resident=saved.resident;update();status.textContent='已保存，下次运行生效'}catch(e){status.textContent=e.message}finally{toggle.disabled=false}
 });const update=()=>{toggle.textContent=s.resident?'常驻：已开启':'常驻：未开启';toggle.setAttribute('aria-pressed',String(!!s.resident));toggle.setAttribute('aria-label',`${s.name} 常驻`)};update();buttons.append(action('详情',()=>showSkillDetail(s)),toggle,status);c.append(buttons);return c});
}
async function showSkillDetail(skill){
 const group=$('group').value,{dialog,body}=resourceDialog(skill.name,skill.description);
 const endpoint=section=>`/api/inspect/${section}?group=${encodeURIComponent(group)}&name=${encodeURIComponent(skill.name)}`;
 const content=el('section'),metadata=el('section'),files=el('section');body.append(content,metadata,files);
 const render=s=>{
  content.replaceChildren(el('h3','section-heading','SKILL.md · 完整正文'),renderMarkdown(s.content||'暂无正文'));
  metadata.replaceChildren(el('h3','section-heading','技能元数据'),detailFields([['来源',skill.source],['路径',skill.path||'存储于数据库'],...(s.scope?[['作用范围',s.scope==='global'?'全局':'当前群']]:[]),...(s.version?[['版本',s.version],['更新时间',time(s.updated_at)]]:[])]));
  metadataList(metadata,'触发条件',s.triggers);metadataList(metadata,'不触发条件',s.non_triggers);metadataList(metadata,'依赖工具',s.required_tools);
 };
 if(!skill.path){render(skill);files.append(el('h3','section-heading','Skills 目录'),el('p','muted','此技能存储于数据库，没有关联的文件夹。'));return}
 const load=async()=>{content.replaceChildren(el('p','muted','正在读取 SKILL.md…'));try{const data=await api(endpoint('skill'));if(dialog.isConnected)render({...skill,...data})}catch(e){if(dialog.isConnected)content.replaceChildren(el('p','warning',e.message),action('重试',load))}};
 const loadFiles=async()=>{files.replaceChildren(el('h3','section-heading','Skills 目录'),el('p','muted','正在读取目录…'));try{const data=await api(endpoint('skill-files'));if(!dialog.isConnected)return;files.replaceChildren(el('h3','section-heading','Skills 目录'),el('p','muted','点击文件夹进入，点击文件预览。单文件最多 5 MiB。'),skillFileBrowser(data,endpoint))}catch(e){if(dialog.isConnected)files.replaceChildren(el('h3','section-heading','Skills 目录'),el('p','warning',e.message),action('重试目录',loadFiles))}};
 await Promise.allSettled([load(),loadFiles()]);
}
function skillFileBrowser(files,endpoint){
 const root=el('div','skill-browser'),nav=el('div','file-breadcrumb'),layout=el('div','file-layout'),list=el('div','file-list'),preview=el('div','file-preview');
 let folder='',serial=0;preview.append(el('p','muted','选择文件查看内容'));layout.append(list,preview);root.append(nav,layout);
 const openFile=async file=>{
  const request=++serial;for(const button of list.querySelectorAll('button'))button.setAttribute('aria-pressed',String(button.dataset.path===file.path));
  preview.replaceChildren(el('h4','',file.path),el('p','muted','加载中…'));
  try{const data=await api(endpoint('skill-file')+'&file='+encodeURIComponent(file.path));if(request!==serial||!root.isConnected)return;
   preview.replaceChildren(el('h4','',file.path),el('div','meta',fileSize(data.size)));
   if(data.kind==='image'){const img=el('img','skill-image');img.alt=file.path;img.src=`data:${data.mime};base64,${data.data}`;img.onerror=()=>img.replaceWith(el('p','warning','图片无法解码'));preview.append(img)}
   else if(data.kind==='text')preview.append(/\.md(?:own)?$/i.test(file.path)?renderMarkdown(data.content):el('pre','file-text',data.content));
   else preview.append(el('p','muted','该文件为二进制格式，暂不支持预览。'));
  }catch(e){if(request===serial&&root.isConnected)preview.replaceChildren(el('p','warning',e.message),action('重试',()=>openFile(file)))}
 };
 const navigate=path=>{
  folder=path;serial++;nav.replaceChildren(action('根目录',()=>navigate('')));let current='';for(const part of folder.split('/').filter(Boolean)){current+=part+'/';const target=current;nav.append(el('span','muted','/'),action(part,()=>navigate(target)))}
  list.replaceChildren();preview.replaceChildren(el('p','muted','选择文件查看内容'));const directories=new Set(),visible=[];
  for(const file of files){if(!file.path.startsWith(folder))continue;const relative=file.path.slice(folder.length);if(relative.includes('/'))directories.add(relative.split('/')[0]);else visible.push(file)}
  for(const name of [...directories].sort()){const b=action('▸ '+name+'/',()=>navigate(folder+name+'/'));b.classList.add('file-entry');list.append(b)}
  for(const file of visible.sort((a,b)=>a.path.localeCompare(b.path))){const b=action('▤ '+file.path.slice(folder.length),()=>openFile(file));b.dataset.path=file.path;b.setAttribute('aria-pressed','false');b.classList.add('file-entry');b.append(el('small','muted',fileSize(file.size)));list.append(b)}
  if(!list.children.length)list.append(el('p','muted','此目录为空'));
 };navigate('');return root;
}
function fileSize(size){return size<1024?`${size} B`:size<1048576?`${(size/1024).toFixed(1)} KiB`:`${(size/1048576).toFixed(1)} MiB`}

// Small, safe Markdown renderer: HTML remains text; URLs allow only HTTP(S).
function markdownInline(parent,text){
 const pattern=/(`[^`]+`|\*\*[^*]+\*\*|\*[^*]+\*|\[[^\]]+\]\([^\s)]+\))/g;let cursor=0;
 for(const match of text.matchAll(pattern)){parent.append(document.createTextNode(text.slice(cursor,match.index)));const token=match[0];let node;
  if(token.startsWith('`'))node=el('code','',token.slice(1,-1));else if(token.startsWith('**'))node=el('strong','',token.slice(2,-2));else if(token.startsWith('*'))node=el('em','',token.slice(1,-1));else{const [,label,url]=token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);node=el('span','',label);if(/^https?:\/\//i.test(url)){node=el('a','',label);node.href=url;node.target='_blank';node.rel='noopener noreferrer'}else node.append(document.createTextNode(` (${url})`))}parent.append(node);cursor=match.index+token.length;
 }parent.append(document.createTextNode(text.slice(cursor)));
}
function renderMarkdown(source){
 const root=el('div','markdown-body'),lines=String(source).replace(/\r\n/g,'\n').split('\n');let i=0;
 if(lines[0]==='---'){const end=lines.indexOf('---',1);if(end>0){root.append(el('pre','markdown-frontmatter',lines.slice(0,end+1).join('\n')));i=end+1}}
 while(i<lines.length){const line=lines[i];if(!line.trim()){i++;continue}
  const fence=line.match(/^\s*(`{3,}|~{3,})(.*)$/);if(fence){const text=[];i++;while(i<lines.length&&!new RegExp('^\\s*'+fence[1][0]+'{'+fence[1].length+',}\\s*$').test(lines[i]))text.push(lines[i++]);i++;const pre=el('pre'),code=el('code','',text.join('\n'));pre.append(code);root.append(pre);continue}
  const heading=line.match(/^(#{1,6})\s+(.+)$/);if(heading){const h=el('h'+heading[1].length);markdownInline(h,heading[2]);root.append(h);i++;continue}
  if(/^\s*([-*_])(?:\s*\1){2,}\s*$/.test(line)){root.append(el('hr'));i++;continue}
  if(i+1<lines.length&&line.includes('|')&&/^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[i+1])){
   const table=el('table'),cells=s=>s.trim().replace(/^\||\|$/g,'').split('|');const header=el('tr');for(const cell of cells(line)){const th=el('th');markdownInline(th,cell.trim());header.append(th)}table.append(header);i+=2;
   while(i<lines.length&&lines[i].includes('|')&&lines[i].trim()){const row=el('tr');for(const cell of cells(lines[i++])){const td=el('td');markdownInline(td,cell.trim());row.append(td)}table.append(row)}const wrap=el('div','markdown-table');wrap.append(table);root.append(wrap);continue;
  }
  const item=line.match(/^\s*(?:[-+*]|\d+\.)\s+(.+)$/);if(item){const ordered=/^\s*\d+\./.test(line),list=el(ordered?'ol':'ul');while(i<lines.length){const match=lines[i].match(ordered?/^\s*\d+\.\s+(.+)$/:/^\s*[-+*]\s+(.+)$/);if(!match)break;const li=el('li');markdownInline(li,match[1]);list.append(li);i++}root.append(list);continue}
  const p=el(line.startsWith('>')?'blockquote':'p');markdownInline(p,line.replace(/^>\s?/,''));root.append(p);i++;
 }return root;
}
function renderTools(root,data){
 $('catalog-status').textContent=`${data.length} 个已注册工具 · 包含 search_tools 发现入口`;
 section(root,'工具能力',data,t=>{const f=t.definition.function,c=card(f.name,f.description,t.namespace);c.classList.add('catalog-summary');c.append(el('div','meta',`${t.read_only?'只读':'可执行写操作'} · 风险 ${t.risk}`));const buttons=el('div','actions');buttons.append(action('详情',()=>showToolDetail(t)));c.append(buttons);return c});
}
function showToolDetail(tool){
 const f=tool.definition.function,{body}=resourceDialog(f.name,f.description);
 body.append(detailFields([['能力分类',tool.namespace],['访问方式',tool.read_only?'只读':'可执行写操作'],['风险等级',tool.risk],['幂等',tool.idempotent?'是':'否'],['群聊动作',tool.group_action?'是':'否']]),el('h3','section-heading','参数定义'));
 let schema=f.parameters;try{if(typeof schema==='string')schema=JSON.parse(schema)}catch{body.append(el('p','warning','参数定义无法解析'));return}body.append(renderSchema(schema??{type:'object',properties:{}},schema));
}
function readableValue(value){if(value===null)return'空值';if(typeof value==='boolean')return value?'是':'否';if(Array.isArray(value))return value.map(readableValue).join('、')||'空列表';if(typeof value==='object')return Object.entries(value).map(([k,v])=>`${k}：${readableValue(v)}`).join('；')||'空对象';return String(value)}
function renderSchema(schema,root=schema,depth=0,refs=[]){
 const node=el('div','schema-node');if(depth>16){node.append(el('p','muted','更深层的嵌套定义已省略'));return node}
 if(typeof schema==='boolean'){node.append(el('p','muted',schema?'允许任意值':'不允许传入值'));return node}schema=schema||{};
 if(schema.$ref){const ref=schema.$ref;if(refs.includes(ref)){node.append(el('p','muted','递归引用：'+ref));return node}let target=root;if(ref.startsWith('#/'))for(const key of ref.slice(2).split('/'))target=target?.[key.replace(/~1/g,'/').replace(/~0/g,'~')];else target=null;if(target)node.append(renderSchema(target,root,depth+1,[...refs,ref]));else node.append(el('p','muted','引用定义：'+ref));}
 const types={string:'文本',integer:'整数',number:'数字',boolean:'布尔值',object:'对象',array:'列表',null:'空值'};
 const type=schema.type||(schema.properties?'object':schema.items?'array':'');if(type)node.append(el('span','badge',Array.isArray(type)?type.map(t=>types[t]||t).join(' / '):types[type]||type));
 if(schema.title)node.append(el('strong','schema-title',schema.title));if(schema.description)node.append(el('p','',schema.description));
 const constraints=[];for(const [key,label] of [['enum','可选值'],['const','固定值'],['default','默认值'],['examples','示例'],['format','格式'],['pattern','匹配规则'],['minimum','最小值'],['maximum','最大值'],['exclusiveMinimum','必须大于'],['exclusiveMaximum','必须小于'],['multipleOf','倍数'],['minLength','最少字符'],['maxLength','最多字符'],['minItems','最少元素'],['maxItems','最多元素'],['uniqueItems','元素唯一'],['minProperties','最少字段'],['maxProperties','最多字段']])if(Object.hasOwn(schema,key))constraints.push([label,readableValue(schema[key])]);if(constraints.length)node.append(detailFields(constraints));
 const properties=Object.entries(schema.properties||{}),required=new Set(schema.required||[]);
 for(const [name,definition] of properties){const row=el('section','schema-field'),head=el('div','row');head.append(el('code','',name),el('span','badge '+(required.has(name)?'running':''),required.has(name)?'必填':'可选'));row.append(head,renderSchema(definition,root,depth+1,refs));node.append(row)}
 if(type==='object'&&!properties.length&&!schema.$ref)node.append(el('p','muted','未声明固定参数'));
 if(schema.items!==undefined){node.append(el('h4','','列表元素'));if(Array.isArray(schema.items))schema.items.forEach((item,i)=>{node.append(el('h5','',`第 ${i+1} 项`),renderSchema(item,root,depth+1,refs))});else node.append(renderSchema(schema.items,root,depth+1,refs))}
 for(const [key,label] of [['anyOf','满足任一条件'],['oneOf','仅满足其中一个条件'],['allOf','同时满足所有条件']])if(schema[key]){node.append(el('h4','',label));for(const [i,variant] of schema[key].entries()){const block=el('section','schema-field');block.append(el('strong','',`方案 ${i+1}`),renderSchema(variant,root,depth+1,refs));node.append(block)}}
 if(schema.additionalProperties===false)node.append(el('p','muted','不允许额外字段'));else if(schema.additionalProperties===true)node.append(el('p','muted','允许额外字段'));else if(schema.additionalProperties){node.append(el('h4','','额外字段的值'),renderSchema(schema.additionalProperties,root,depth+1,refs))}
 return node;
}
