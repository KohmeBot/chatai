const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = vm.createContext({});
vm.runInContext(fs.readFileSync(__dirname + '/catalog-ui.js', 'utf8'), context);
const {filterCatalog, catalogCategory} = context;

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

test('disable control commits only saved state and can recover after failure', async () => {
 const make=()=>({attrs:{},children:[],classList:{add(){},toggle(){}},setAttribute(k,v){this.attrs[k]=v},removeAttribute(k){delete this.attrs[k]},append(...children){this.children.push(...children)}});
 let fail=false,request;
 const sandbox=vm.createContext({el:make,action:(_,handler)=>Object.assign(make(),{click:handler}),ConsoleControls:{iconButton(){}},api:async(url,opts)=>{request={url,...JSON.parse(opts.body)};if(fail)throw Error('保存失败');return {disabled:request.disabled}}});
 vm.runInContext(fs.readFileSync(__dirname+'/catalog-ui.js','utf8'),sandbox);
 const item={id:42,name:'demo',resident:true},card=make(),status=make(),button=sandbox.disableControl(item,'skills','10',card,status);
 assert.equal(button.attrs['aria-checked'],'false');
 await button.click();assert.equal(item.disabled,true);assert.equal(item.resident,true);assert.equal(request.id,42);assert.equal(button.attrs['aria-checked'],'true');assert.equal(card.children[0].hidden,false);
 fail=true;await button.click();assert.equal(item.disabled,true);assert.equal(status.textContent,'保存失败');assert.equal(button.disabled,false);assert.equal(button.attrs['aria-busy'],undefined);
 fail=false;await button.click();assert.equal(item.disabled,false);assert.equal(card.children[0].hidden,true);
});
