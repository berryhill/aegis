"use strict";
// Lightweight DOM-event regression of the shipped script, not browser acceptance.
const {test} = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync(__dirname + "/loop_workspace.js", "utf8");
function fixture() {
  let focused;
  class Element {
    constructor(dataset = {}) { this.dataset = dataset; this.attributes = {}; this.handlers = {}; this.hidden = false; }
    setAttribute(key, value) { this.attributes[key] = value; }
    addEventListener(type, fn) { (this.handlers[type] ||= []).push(fn); }
    focus() { focused = this; }
    dispatch(type, key) {
      const event = {key, prevented:false, preventDefault() {this.prevented=true;}};
      for (const fn of this.handlers[type] || []) fn(event);
      return event;
    }
  }
  const tabs = [new Element({loopTab:"inputs"}), new Element({loopTab:"evidence"})];
  const panels = [new Element({loopTabPanel:"inputs"}), new Element({loopTabPanel:"evidence"})];
  const nav = new Element();
  const root = {
    querySelector(selector) {return selector === ".loop-tab-nav" ? nav : null;},
    querySelectorAll(selector) {return selector === "[data-loop-tab]" ? tabs : selector === "[data-loop-tab-panel]" ? panels : [];}
  };
  const context = {document:{querySelector:()=>root}, fetch:()=>{throw Error("unexpected network");}};
  const enhance = () => vm.runInNewContext(source, context);
  return {tabs, panels, nav, enhance, focused:()=>focused};
}
function selected(f, index) {
  f.tabs.forEach((tab,i)=>{
    assert.equal(tab.attributes.role,"tab");
    assert.equal(tab.attributes["aria-selected"],String(i===index));
    assert.equal(tab.tabIndex,i===index?0:-1);
    assert.equal(f.panels[i].hidden,i!==index);
    assert.equal(f.panels[i].attributes.role,"tabpanel");
  });
}
test("click, arrows, Home/End and explicit activation switch local panels",()=>{
  const f=fixture();
  assert.ok(f.panels.every(p=>!p.hidden),"no-JS sections readable");
  f.enhance();
  assert.equal(f.nav.attributes.role,"tablist");
  selected(f,0);
  f.tabs[1].dispatch("click"); selected(f,1);
  for (const [from,key,to] of [[1,"ArrowRight",0],[0,"ArrowLeft",1],[1,"Home",0],[0,"End",1],[1,"Enter",1],[1," ",1]]) {
    assert.equal(f.tabs[from].dispatch("keydown",key).prevented,true);
    selected(f,to); assert.equal(f.focused(),f.tabs[to]);
  }
  assert.equal(f.tabs[1].dispatch("keydown","Tab").prevented,false);
});
test("fresh revision enhancement resets to coherent first tab",()=>{
  const previous=fixture(); previous.enhance(); previous.tabs[1].dispatch("click"); selected(previous,1);
  const next=fixture(); next.enhance(); selected(next,0);
});
