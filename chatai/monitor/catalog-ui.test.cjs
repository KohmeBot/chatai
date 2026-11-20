const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = vm.createContext({});
vm.runInContext(fs.readFileSync(__dirname + '/catalog-ui.js', 'utf8'), context);
const {filterCatalog, catalogCategory, toolScopeLabel} = context;

test('tool filters default to all and combine risk, namespace and exact MCP server', () => {
  const items = [{risk:'low',namespace:'web'}, {risk:'high',namespace:'mcp_docs'}, {risk:'low',namespace:'mcp_docs'}, {risk:'low',namespace:'mcp_docs_extra'}];
  assert.equal(filterCatalog(items, {}).length, 4);
  assert.equal(filterCatalog(items, {risk:'',server:''}).length, 4);
  assert.equal(filterCatalog(items, {risk:'low',server:'server:docs'})[0], items[2]);
  assert.equal(filterCatalog(items, {risk:'low',server:'server:docs'}).length, 1);
  assert.equal(filterCatalog(items, {namespace:'web',server:'builtin'})[0], items[0]);
  assert.equal(filterCatalog(items, {namespace:'web',risk:'high'}).length, 0);
});

test('skill filters distinguish directory, generated, config and unknown files', () => {
  const items = [{path:'demo/SKILL.md',has_scripts:true,has_resources:false}, {source:'generated',has_scripts:false,has_resources:false}, {source:'config',has_scripts:false,has_resources:false}, {path:'broken/SKILL.md'}];
  assert.equal(filterCatalog(items, {source:'directory'}).length, 2);
  assert.equal(filterCatalog(items, {source:'directory',has_scripts:'true',has_resources:'false'})[0], items[0]);
  assert.equal(filterCatalog(items, {source:'generated'})[0], items[1]);
  assert.equal(filterCatalog(items, {has_scripts:'false'}).length, 2);
  assert.equal(filterCatalog(items, {has_resources:'unknown'})[0], items[3]);
  assert.equal(catalogCategory({}, 'risk'), 'unknown');
});

test('tool cards and details show their conversation scope', () => {
  const node=(tag, className, textContent)=>({tag,className,textContent,children:[],classList:{add(){},toggle(){}},setAttribute(){},append(...children){this.children.push(...children);}});
  const cards=node('div'), body=node('div'), status=node('span');
  const sandbox=vm.createContext({
    el:node, $:()=>status, matches:()=>true,
    section:(root,_title,items,render)=>root.append(...items.map(render)),
    card:(_name,_description,tag)=>node('article','',tag),
    action:(_label,click)=>({...node('button'),click}), ConsoleControls:{iconButton(){}},
  });
  vm.runInContext(fs.readFileSync(__dirname+'/catalog-ui.js','utf8'),sandbox);
  sandbox.resourceDialog=()=>({body});
  const tools=['group','private','both'].map(scope=>({scope,namespace:'context',definition:{function:{name:scope}}}));
  sandbox.renderTools(cards,tools);
  assert.deepEqual(cards.children.slice(1).map(card=>card.textContent),['context · 群聊','context · 私聊','context · 群聊和私聊']);
  cards.children[2].children.at(-1).children[0].click();
  const detail=body.children[0].children.map(node=>node.textContent);
  assert.equal(detail[detail.indexOf('适用会话')+1],'私聊');
});

test('disable control commits only saved state and can recover after failure', async () => {
 const make=()=>({attrs:{},children:[],classList:{add(){},toggle(){}},setAttribute(k,v){this.attrs[k]=v},removeAttribute(k){delete this.attrs[k]},append(...children){this.children.push(...children)}});
 let fail=false,request;
 const sandbox=vm.createContext({el:make,action:(_,handler)=>Object.assign(make(),{click:handler}),ConsoleControls:{iconButton(){}},api:async(url,opts)=>{request={url,...JSON.parse(opts.body)};if(fail)throw Error('保存失败');return {disabled:request.disabled}}});
 vm.runInContext(fs.readFileSync(__dirname+'/catalog-ui.js','utf8'),sandbox);
 const item={id:42,name:'demo',resident:true},card=make(),status=make(),button=sandbox.disableControl(item,'skills',card,status);
 assert.equal(button.attrs['aria-checked'],'false');
 await button.click();assert.equal(item.disabled,true);assert.equal(item.resident,true);assert.deepEqual(request,{url:'/api/inspect/skills',id:42,disabled:true});assert.equal(button.attrs['aria-checked'],'true');assert.equal(card.children[0].hidden,false);
 fail=true;await button.click();assert.equal(item.disabled,true);assert.equal(status.textContent,'保存失败');assert.equal(button.disabled,false);assert.equal(button.attrs['aria-busy'],undefined);
 fail=false;await button.click();assert.equal(item.disabled,false);assert.equal(card.children[0].hidden,true);
});

test('catalog tabs load and refresh without groups while scoped tabs keep the group picker', async () => {
  const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
  const script = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)]
    .map(match => match[1]).find(source => source.includes('function catalogEndpoint('));
  const buttons = ['runs', 'skills', 'tools', 'schedules'].map(view => ({dataset:{view}, classList:{toggle(){}}}));
  const elements = new Map([['nav', {querySelectorAll:() => buttons}]]);
  const getElement = id => {
    if (!elements.has(id)) elements.set(id, {value:'', setAttribute(){}, replaceChildren(){}});
    return elements.get(id);
  };
  const requests = [];
  let changeScope;
  const sandbox = vm.createContext({
    $:getElement, authenticated:true, fetch:async() => ({ok:false}),
    api:async url => {requests.push(url); return {};},
    closeGroupPicker(){}, syncScopeControl(){}, initScopeControl(_id, callback){changeScope = callback;},
    mountCatalogFilters(){}, renderSkills(){}, renderTools(){}, renderSchedules(){}, toolScopeLabel,
    ConsoleMotion:{view(){}, catalog(){}, busy(_button, load){return load();}},
  });
  vm.runInContext(script, sandbox);

  for (const [index, view] of [[1, 'skills'], [2, 'tools']]) {
    buttons[index].onclick();
    assert.equal(getElement('group-picker').hidden, true);
    assert.equal(getElement('catalog-scope').hidden, true);
    assert.equal(requests.at(-1), '/api/inspect/' + view);
    await getElement('catalog-refresh').onclick();
    assert.deepEqual(requests.splice(0), ['/api/inspect/' + view, '/api/inspect/' + view]);
  }

  getElement('catalog-query').value='私聊';
  assert.equal(sandbox.matches({scope:'private'}),true);
  assert.equal(sandbox.matches({scope:'both'}),true);
  assert.equal(sandbox.matches({scope:'group'}),false);

  buttons[3].onclick();
  assert.equal(getElement('group-picker').hidden, false);
  assert.equal(getElement('catalog-scope').hidden, false);
  assert.equal(requests.length, 0);
  changeScope('private');
  assert.equal(getElement('group-picker').hidden, true);
  assert.equal(requests.at(-1), '/api/inspect/schedules?scope=private&offset=0');
});
