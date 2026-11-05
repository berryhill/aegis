"use strict";
const assert=require("node:assert/strict"),fs=require("node:fs"),vm=require("node:vm");
const source=fs.readFileSync("web/console/navigation.js","utf8");
function resolve(href) {
 const url=new URL(href),changes=[];
 const location={pathname:url.pathname,hash:url.hash,href:url.href,replace(value){changes.push(new URL(value));}};
 vm.runInNewContext(source,{URL,location,document:{body:null,querySelector(){return null;}},window:{addEventListener(){}}});
 return changes;
}
let changes=resolve("https://console.example/console/loops?q=review&availability=archived&revision=8#/loops/stable:2");
assert.equal(changes.length,1);assert.equal(changes[0].origin,"https://console.example");
assert.equal(changes[0].searchParams.get("record_key"),"stable:2");assert.equal(changes[0].searchParams.get("revision"),null);
assert.equal(changes[0].searchParams.get("q"),"review");assert.equal(changes[0].searchParams.get("availability"),"archived");
for(const hash of ["#/loops/%2f%2fevil","#/loops/a?authority=x","#/loops/a%00","#/loops/a:0","#/loops/a:01","#/loops/a:"+"9".repeat(20),"#/loops/"+"a".repeat(277)]) {
 assert.equal(resolve("https://console.example/console/loops"+hash).length,0,hash);
}
assert.equal(resolve("https://console.example/console/loops?record_key=a%3A2#/loops/a:2").length,0);
for(const key of ["logical-loop","team/loop:stage","team/loop:stage:3","a".repeat(255)+":18446744073709551615"]) {
 const result=resolve("https://console.example/console/loops#/loops/"+encodeURIComponent(key));
 assert.equal(result.length,1,key);assert.equal(result[0].pathname,"/console/loops");
 assert.equal(result[0].origin,"https://console.example");assert.equal(result[0].searchParams.get("record_key"),key);
}
console.log("PASS Loop-only exact read reconciliation, filter retention, stale revision removal and malformed denial");
