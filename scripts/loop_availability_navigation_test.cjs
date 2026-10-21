"use strict";
// Execute the shipped asset: browser-restored values are not URL authority.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync("web/console/navigation.js", "utf8");
function fixture(query, initial) {
  const events = {}, changes = {};
  let submits = 0;
  const location = new URL("https://console.example/console/loops" + query);
  const select = {value: initial, options: ["published", "archived"].map(value =>
    ({value, defaultSelected: value === initial})),
    form: {requestSubmit() { submits++; }},
    addEventListener(name, fn) { changes[name] = fn; }};
  vm.runInNewContext(source, {URL, location,
    document: {body: null, querySelector() { return select; }},
    window: {addEventListener(name, fn) { (events[name] ||= []).push(fn); }}});
  return {select, location, changes, submits: () => submits,
    show(persisted) { for (const fn of events.pageshow) fn({persisted}); }};
}
for (const persisted of [false, true]) {
  for (const [query, expected] of [["", "published"], ["?availability=", "published"],
    ["?availability=published&q=review", "published"], ["?availability=archived", "archived"]]) {
    const f = fixture(query, expected);
    const href = f.location.href;
    f.select.value = expected === "published" ? "archived" : "published";
    f.show(persisted);
    assert.equal(f.select.value, expected, `pageshow persisted=${persisted} ${query}`);
    assert.equal(f.location.href, href);
    assert.equal(f.submits(), 0, "restoration must never navigate or mutate");
    f.select.value = "archived";
    f.changes.change();
    assert.equal(f.submits(), 1, "explicit change retains native GET submission");
  }
}
const f = fixture("?availability=published", "published");
f.select.value = "archived"; f.show(false); // Back after browser form restoration.
assert.equal(f.select.value, "published");
f.location.search = "?availability=archived"; f.select.value = "published"; f.show(true);
assert.equal(f.select.value, "archived", "Forward/BFCache uses current URL");
assert.equal(f.submits(), 0);
const invalid = fixture("?availability=unknown", "published");
invalid.select.value = "archived"; invalid.show(false);
assert.equal(invalid.select.value, "published", "unknown query never becomes a control value");
assert.equal(invalid.location.search, "?availability=unknown", "server still owns unknown-query denial");
console.log("PASS: actual navigation asset restores default/Published/Archived on native and BFCache pageshow, Back/Forward; no restoration submission; explicit GET change preserved; invalid query unchanged");
