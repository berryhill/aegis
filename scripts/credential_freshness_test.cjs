const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/console/navigation.js', 'utf8');
function fixture() {
 const events = {}; let reloads = 0, dialog = false, editing = false;
 const href = 'https://aegis.invalid/console/credentials?q=test&status=active&record_key=secret-1#/credentials/secret-1';
 const location = {href, pathname: '/console/credentials', hash: '#/credentials/secret-1', reload(){ reloads++; }, replace(){throw Error('unexpected navigation');}};
 const document = {body:{dataset:{collectionUrl:'/console/credentials?q=test&status=active',detailOpen:'true'}}, visibilityState:'visible', activeElement:null,
 getElementById(){return null;}, querySelectorAll(){return [];}, querySelector(){return dialog ? {} : null;},
 addEventListener(n,f){events['document:'+n]=f;}};
 const context = {scrollX:0, scrollY:0, URL, location, document, history:{state:null,replaceState(){}}, sessionStorage:{getItem(){return null;}}, requestAnimationFrame(f){f();}, addEventListener(n,f){events[n]=f;}};
 context.window=context; vm.runInNewContext(source,context);
 return {events,location,document,reloads:()=>reloads,dialog(v){dialog=v;}};
}
let f=fixture(); f.events.focus?.(); assert.equal(f.reloads(),0,'initial focus must not loop');
f.events.blur?.(); f.events.focus?.(); assert.equal(f.reloads(),1,'returning focus must refresh');
f.events.focus?.(); assert.equal(f.reloads(),1,'duplicate focus must not loop');
assert.match(f.location.href,/q=test&status=active&record_key=secret-1/);
f=fixture(); f.dialog(true); f.events.blur?.(); f.events.focus?.(); assert.equal(f.reloads(),0,'open mutation dialog must survive');
f=fixture(); f.events['document:input']?.(); f.events.blur?.(); f.events.focus?.(); assert.equal(f.reloads(),0,'unsaved filters or mutation values must survive');
f=fixture(); f.events.pageshow?.({persisted:true}); assert.equal(f.reloads(),1,'BFCache must refresh metadata');
console.log('PASS credential focus/BFCache refresh, loop prevention, URL and edit preservation');
