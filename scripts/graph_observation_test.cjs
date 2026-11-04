"use strict";
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/console/graph_observation.js','utf8');
async function fixture(mode) {
  const root = {hidden:false}, events = {}, timers = new Map(), requests=[];
  let children = ['protected-json-and-controls'], timerID=0, reloads=0, delayNext=false, resume;
  const document = {visibilityState:'visible',querySelector:s=>s==='[data-graph-workspace]'?root:null,
    addEventListener:(n,f)=>events[n]=f,createElement:()=>({dataset:{},setAttribute(){}}),
    body:{replaceChildren(...values){children=values;}}};
  const ctx={document,window:{addEventListener:(n,f)=>events[n]=f},
    location:{href:'http://localhost/console/graphs?record_key=g%3A1#/graphs/g:1',origin:'http://localhost',reload:()=>reloads++},URL,AbortController,
    setTimeout:(f,ms)=>{timers.set(++timerID,{f,ms});return timerID;},clearTimeout:id=>timers.delete(id),
    DOMParser:class {parseFromString(){return {querySelector:s=>s==='#session-form'?(mode==='lost'?{}:null):s==='#logout'?(mode==='valid'?{}:null):s==='[data-graph-workspace]'?(mode==='valid'?{}:null):null};}},
    fetch:async(url,options)=>{requests.push({url,options});if(delayNext){delayNext=false;await new Promise(r=>resume=r);}if(mode==='network')throw Error('offline');return {status:mode==='unauthorized'?401:200,ok:mode!=='unauthorized',headers:{get:()=> 'text/html; charset=utf-8'},text:async()=>'<inert>'};}};
  vm.runInNewContext(source,ctx);await new Promise(setImmediate);
  assert.equal(requests.length,1);assert.equal(requests[0].options.method,'GET');
  assert.equal(requests[0].options.credentials,'same-origin');assert.equal(requests[0].options.cache,'no-store');assert(!requests[0].url.includes('#'));
  return {root,events,timers,requests,children:()=>children,document,reloads:()=>reloads,deferNext:()=>{delayNext=true;},resume:()=>resume()};
}
(async()=>{
 for(const mode of ['lost','unauthorized','network','unknown']) {const f=await fixture(mode);assert.equal(f.children().length,2);assert.equal(f.children()[0].dataset.graphAuthCleared,['lost','unauthorized'].includes(mode)?'authentication-required':'unavailable');assert.equal(f.timers.size,0);f.events.focus();await new Promise(setImmediate);assert.equal(f.requests.length,1);}
 const f=await fixture('valid');assert.equal(f.root.hidden,false);assert([...f.timers.values()].some(t=>t.ms===15000));
 f.document.visibilityState='hidden';f.events.visibilitychange();assert.equal(f.root.hidden,true);
 f.document.visibilityState='visible';f.events.visibilitychange();await new Promise(setImmediate);assert.equal(f.root.hidden,false);assert.equal(f.requests.length,2);
 f.deferNext();const poll=[...f.timers.values()].find(t=>t.ms===15000);poll.f();assert.equal(f.root.hidden,false,'routine session observation must not hide a verified workspace');f.resume();await new Promise(setImmediate);
 f.events.pagehide();assert.equal(f.children()[0].dataset.graphAuthCleared,'unavailable');f.events.pageshow({persisted:true});assert.equal(f.reloads(),1);
 console.log('PASS Graph auth observation: bounded same-origin GET, confirmed/unknown fail-closed clearing, visibility and cached restoration');
})().catch(e=>{console.error(e);process.exitCode=1;});
