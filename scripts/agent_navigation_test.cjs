// Dependency-free regression for the bounded record-only fragment resolver.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/console/navigation.js', 'utf8');
function resolve(href) {
  const replacements = [];
  const location = {href, hash: new URL(href).hash, replace: value => replacements.push(value)};
  vm.runInNewContext(source, {URL, location, document: {body: {dataset: {}}}, addEventListener() {}});
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
