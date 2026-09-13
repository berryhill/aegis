"use strict";
// Execute the actual embedded navigation source without network or DOM powers.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");
const source = fs.readFileSync(path.join(__dirname, "../web/console/navigation.js"), "utf8");
function run(route) {
  const redirects = [];
  const listeners = {};
  const location = new URL(route, "https://console.example");
  location.replace = target => redirects.push(new URL(target));
  vm.runInNewContext(source, {location, URL, document: {body: null},
    window: {addEventListener: (event, fn) => { listeners[event] = fn; }}});
  return {location, redirects, listeners};
}
for (const key of ["graph-proof:1", "graph/a:7", "https://other.example:2"]) {
  const result = run("/console/graphs?q=review#/graphs/" + encodeURIComponent(key));
  assert.equal(result.redirects.length, 1);
  const target = result.redirects[0];
  assert.equal(target.origin, "https://console.example");
  assert.equal(target.pathname, "/console/graphs");
  assert.equal(target.searchParams.get("q"), "review");
  assert.equal(target.searchParams.get("record_key"), key);
  assert.equal(run(target.href).redirects.length, 0, "redirect must converge");
}
for (const route of ["/console/agents#/graphs/a:1", "/console/graphs#/graphs/",
  "/console/graphs#/graphs/%ZZ", "/console/graphs#/graphs/%00a:1",
  "/console/graphs#/graphs/" + "a".repeat(1025), "/console/graphs#/loops/a:1"]) {
  assert.equal(run(route).redirects.length, 0, route);
}
const changed = run("/console/graphs?record_key=a%3A2#/graphs/a:2");
changed.location.hash = "#/graphs/a:1";
changed.listeners.hashchange();
assert.equal(changed.redirects[0].searchParams.get("record_key"), "a:1");
console.log("PASS bounded Graph bookmark resolution: same-origin, exact revision, malformed/oversized keys, convergence and hashchange");
