const http=require('node:http'),fs=require('node:fs'),path=require('node:path');
const start=Date.now()-20000,events=[];
function event(kind,ms,extra={}){events.push({kind,time:new Date(start+ms).toISOString(),...extra})}
event('run_started',0,{text:'整理今天的群聊重点，生成一份周报',summary:'已收到任务，准备开始处理。'});
event('model_started',10,{step:1});event('model_finished',1800,{step:1,input_tokens:900,output_tokens:84,summary:'读取群聊上下文与周报技能。'});
for(const [name,id]of [['read_group_context','a'],['read_skill','b'],['search_web','c']])event('tool_planned',1810,{step:1,name,call_id:id});
event('tool_started',1900,{step:1,name:'read_group_context',call_id:'a',arguments:'{"limit":30}'});event('tool_finished',2200,{step:1,name:'read_group_context',call_id:'a',result:'{"messages":30}',duration_ms:300});
event('tool_started',2210,{step:1,name:'read_skill',call_id:'b',arguments:'{"name":"weekly-report"}'});event('tool_finished',2500,{step:1,name:'read_skill',call_id:'b',result:'{"loaded":true}',duration_ms:290});event('skill_loaded',2501,{name:'read_skill',call_id:'b',arguments:'{"name":"weekly-report"}'});
event('tool_started',2510,{step:1,name:'search_web',call_id:'c'});event('tool_finished',4500,{step:1,name:'search_web',call_id:'c',duration_ms:1990});
event('model_started',4600,{step:2});event('model_finished',7000,{step:2,input_tokens:1200,output_tokens:220,summary:'重点已整理，准备发送报告。'});event('tool_planned',7010,{step:2,name:'send_message',call_id:'d'});event('tool_started',7020,{step:2,name:'send_message',call_id:'d',arguments:'{"text":"周报已整理完成。"}'});event('tool_finished',7200,{step:2,name:'send_message',call_id:'d',result:'{"message_id":123}',duration_ms:180});event('run_finished',7250,{text:'周报已完成',summary:'已完成群聊总结并发送周报。'});
const run={id:'demo-42',group_id:'123456',user_id:'987654',user_name:'小陈',prompt:'整理今天的群聊重点，生成一份周报',model:'deepseek:deepseek-chat',status:'success',started_at:new Date(start).toISOString(),duration_ms:7250,input_tokens:2100,output_tokens:304,summary:'已完成群聊总结并发送周报。',events};
http.createServer((req,res)=>{
 const url=new URL(req.url,'http://127.0.0.1');
 if(url.pathname==='/api/session'){res.writeHead(204);res.end();return}
 if(url.pathname==='/api/events'){res.writeHead(200,{'Content-Type':'text/event-stream'});res.write('data: refresh\n\n');return}
 let data;if(url.pathname==='/api/runs')data=[{...run,events:undefined}];else if(url.pathname==='/api/runs/demo-42')data=run;else if(url.pathname==='/api/inspect/groups')data=[];else if(url.pathname==='/api/retention')data={persistent:true,days:7};
 if(data){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(data));return}
 const name=url.pathname==='/'?'index.html':url.pathname.slice(1);if(!/^[\w.-]+$/.test(name)){res.writeHead(404);res.end();return}
 try{const file=fs.readFileSync(path.join(__dirname,'chatai/monitor',name));res.writeHead(200,{'Content-Type':name.endsWith('.js')?'text/javascript':name.endsWith('.css')?'text/css':'text/html'});res.end(file)}catch{res.writeHead(404);res.end()}
}).listen(9187,'127.0.0.1',()=>console.log('Monitor fixture http://127.0.0.1:9187'));
