"use strict";
// Lightweight actual-script regression. The DOM model checks numerical CSSOM
// and control behavior, not browser layout/CSP or authenticated acceptance.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync(process.argv[2] || "web/console/graph_workspace.js", "utf8");
function element(dataset = {}) {
  return {dataset, hidden:false, textContent:"", listeners:{}, classList:{add(){},remove(){}},
    setAttribute(){}, focus(){}, scrollIntoView(){}, getBoundingClientRect(){return {top:0};},
    addEventListener(name, fn){this.listeners[name]=fn;},
    fire(name, event={}){this.listeners[name]?.(event);}};
}
function fixture(sw, sh, vw, vh) {
  const stage=element(), space=element(), viewport=element();
  stage.style={}; stage.scrollWidth=sw; stage.scrollHeight=sh; stage.offsetHeight=sh;
  viewport.clientWidth=vw; viewport.clientHeight=vh;
  const sheet={href:"http://fixture.test/console/assets/app.css",cssRules:[],
    insertRule(text,i){this.cssRules.splice(i,0,{style:{}});return i;}};
  let left=0,top=0;
  const size=(axis)=>parseFloat(sheet.cssRules[0]?.style[axis])||0;
  Object.defineProperties(viewport,{
    scrollLeft:{get:()=>left,set:v=>{left=Math.max(0,Math.min(v,Math.max(0,size("width")-viewport.clientWidth)));}},
    scrollTop:{get:()=>top,set:v=>{top=Math.max(0,Math.min(v,Math.max(0,size("height")-viewport.clientHeight)));}}
  });
  viewport.scrollBy=(x,y)=>{viewport.scrollLeft+=x;viewport.scrollTop+=y;};
  const scale=element(), fit=element(), plus=element({graphZoom:"in"}), minus=element({graphZoom:"out"});
  const inspector=element(); inspector.offsetHeight=680;
  const pane=element(); pane.offsetHeight=400;
  const node=element({graphNode:"0"}), panel=element({graphNodePanel:"0"}), close=element();
  const definition=element(), button=element();
  const lookup={".gw-canvas":pane,"[data-graph-viewport]":viewport,"[data-graph-stage]":stage,"[data-graph-pan-space]":space,
    "#graph-context":inspector,"#graph-context-title":element(),".graph-workspace":element(),
    "[data-graph-definition-panel]":definition,"[data-graph-close]":close,"[data-graph-scale]":scale,"[data-graph-fit]":fit};
  const root={querySelector:s=>lookup[s],querySelectorAll:s=>({"[data-graph-node-panel]":[panel],"[data-graph-node]":[node],"[data-graph-definition]":[button],"[data-graph-zoom]":[plus,minus]}[s]||[])};
  let resize;
  vm.runInNewContext(source,{document:{querySelector:()=>root,styleSheets:[sheet]},
    URL,location:{href:"http://fixture.test/console",origin:"http://fixture.test"},
    window:{addEventListener(){}},innerWidth:vw,innerHeight:1000,
    matchMedia:()=>({matches:false}),ResizeObserver:class{constructor(fn){resize=fn;}observe(){}}});
  const zoom=()=>Number((sheet.cssRules[1]?.style.transform || stage.style.transform).match(/scale\((.+)\)/)[1]);
  const expected=()=>Math.min(1,(viewport.clientWidth-8)/sw,(viewport.clientHeight-8)/sh);
  const fullFit=()=>{
    const z=zoom(); assert.ok(Math.abs(z-expected())<1e-12);
    assert.ok(sw*z<=viewport.clientWidth && sh*z<=viewport.clientHeight,"complete stage must fit on both axes");
    assert.equal(size("width"),sw*z);assert.equal(size("height"),sh*z);
    assert.equal(viewport.scrollLeft,0);assert.equal(viewport.scrollTop,0);
  };
  assert.equal(zoom(),1);
  for(let i=0;i<40;i++)plus.fire("click");assert.equal(zoom(),2);
  for(let i=0;i<40;i++)minus.fire("click");assert.equal(zoom(),Math.min(.3,expected()));
  fit.fire("click");fullFit();
  viewport.clientWidth=300;viewport.clientHeight=240;resize();fullFit();
  node.fire("click");assert.equal(inspector.hidden,false);assert.equal(panel.hidden,false);
  assert.equal(sheet.cssRules[2].style.height,`${vw<=760 ? 360 : 620}px`);
  inspector.fire("keydown",{key:"Escape",preventDefault(){}});assert.equal(inspector.hidden,true);
  plus.fire("click");
  viewport.scrollLeft=1e9;viewport.scrollTop=1e9;
  assert.equal(viewport.scrollLeft,Math.max(0,sw*zoom()-viewport.clientWidth));
  assert.equal(viewport.scrollTop,Math.max(0,sh*zoom()-viewport.clientHeight));
  viewport.fire("keydown",{target:viewport,key:"0",preventDefault(){}});fullFit();
  assert.equal(sheet.cssRules[1].style.position,"absolute");
}
for(const width of [1280,390]) {
  fixture(128*290-30,150,width,280); // full supported chain, 128 ranks
  fixture(260,128*132+18,width,280); // full disconnected rank, 128 rows
  fixture(5*290-30,2*132+18,width,280); // accepted branch/merge dimensions
}
console.log("PASS Graph actual-script numerical Fit (128 ranks/rows), CSSOM pan extent, bounded zoom, resize, keyboard and inspector controls; browser layout not exercised");
