// Dependency-free regression for the bounded record-only fragment resolver.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/console/navigation.js', 'utf8');
function resolve(href) {
  const replacements = [];
  const url = new URL(href);
  const location = {href, hash: url.hash, pathname: url.pathname, replace: value => replacements.push(value)};
  const context = {URL, location, document: {querySelector() { return null; }, body: {dataset: {}}}, addEventListener() {}};
  context.window = context;
  vm.runInNewContext(source, context);
  return replacements;
}
const base = 'https://aegis.invalid/console/agents';
for (const fragment of ['#/agents/%2Fetc', '#/agents/%ZZ', '#/agents/office?stanza=principal', '#/agents/office/extra', '#/agents/..', '#/agents/'+ 'a'.repeat(129), '#/queue/office']) {
  assert.deepEqual(resolve(base+fragment), [], fragment);
}
let target = new URL(resolve(base+'?q=office&revision=2&record_key=old#/agents/office')[0]);
assert.equal(target.origin, 'https://aegis.invalid');
assert.equal(target.pathname, '/console/agents');
assert.equal(target.searchParams.get('record_key'), 'office');
assert.equal(target.searchParams.has('revision'), false);
assert.equal(target.searchParams.get('q'), 'office');
assert.deepEqual(resolve(base+'?record_key=office&revision=2#/agents/office'), []);
console.log('PASS: bounded Agent fragment routing; invalid selectors denied; historical revision preserved only for the same Agent');

// Restoration is presentation-only and applies to both a new link-Back document
// (sessionStorage) and the original browser-Back entry (history.state/BFCache).
for (const entry of [false, true]) {
  const href = base + '?q=office&lifecycle=enabled&page=2&limit=20';
  const collectionURL = new URL(href).pathname + new URL(href).search;
  const saved = {collectionURL, recordKey:'office', x:0, y:417, inventoryY:0};
  const attributes = new Map(); const calls = []; const handlers = {};
  const selected = {dataset:{recordKey:'office'}, closest(){return null;}, addEventListener(){}, setAttribute(k,v){attributes.set(k,v);}, focus(options){calls.push(['focus',options.preventScroll]);}};
  const other = {dataset:{recordKey:'other'}, closest(){return null;}, addEventListener(){}, setAttribute(k,v){calls.push([k,v]);}};
  const u = new URL(href);
  const context = {URL, location:{href,hash:'',pathname:u.pathname,replace(){throw Error('unexpected navigation');}},
    history:{state:entry ? {aegisCollection:saved}:null,replaceState(){}},
    sessionStorage:{getItem(){return JSON.stringify(saved);}},
    document:{body:{dataset:{collectionUrl:collectionURL,detailOpen:'false'}},querySelector(){return null;},getElementById(){return null;},querySelectorAll(){return [selected,other];}},
    requestAnimationFrame(fn){fn();},scrollTo(x,y){calls.push(['scroll',x,y]);},addEventListener(name,fn){handlers[name]=fn;}};
  context.window=context;
  vm.runInNewContext(source,context);
  assert.equal(attributes.get('aria-current'),'true');
  assert(calls.some(v=>v[0]==='aria-current' && v[1]==='false'));
  assert(calls.some(v=>v[0]==='focus' && v[1]===true));
  assert(calls.some(v=>v[0]==='scroll' && v[2]===417));
  handlers.pageshow({persisted:true});
  assert.equal(attributes.get('aria-current'),'true');
}
console.log('PASS: Agent selection/focus/nonzero-scroll restoration with filters, native link Back and browser Back/BFCache entry');
