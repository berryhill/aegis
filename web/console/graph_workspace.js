"use strict";
// Presentation only: no network, storage or route mutations.
(() => {
  const root = document.querySelector("[data-graph-workspace]");
  if (!root) return;
  const viewport = root.querySelector("[data-graph-viewport]");
  const stage = root.querySelector("[data-graph-stage]");
  const inspector = root.querySelector("#graph-context");
  const title = root.querySelector("#graph-context-title");
  const split = root.querySelector(".graph-workspace");
  const definition = root.querySelector("[data-graph-definition-panel]");
  const panels = Array.from(root.querySelectorAll("[data-graph-node-panel]"));
  const nodes = Array.from(root.querySelectorAll("[data-graph-node]"));
  const definitionButtons = Array.from(root.querySelectorAll("[data-graph-definition]"));
  if (!stage) return;
  // Match the accepted workspace's canvas-first entry. Keep the server-rendered
  // definition visible without JavaScript; enhancement makes it contextual.
  inspector.hidden = true;
  split.classList.add("solo");
  for (const button of definitionButtons) button.setAttribute("aria-expanded", "false");
  let sizeCanvasPane = () => {};
  const show = (index) => {
    if (index !== null && !panels.some(p => p.dataset.graphNodePanel === index)) return;
    inspector.hidden = false;
    split.classList.remove("solo");
    definition.hidden = index !== null;
    for (const panel of panels) panel.hidden = panel.dataset.graphNodePanel !== index;
    for (const node of nodes) node.setAttribute("aria-pressed", String(node.dataset.graphNode === index));
    for (const button of definitionButtons) button.setAttribute("aria-expanded", String(index === null));
    title.textContent = index === null ? "Graph definition" : "Node inspector";
    sizeCanvasPane();
    title.focus({preventScroll: true});
    if (matchMedia("(max-width:1080px)").matches) inspector.scrollIntoView({block:"start"});
  };
  for (const button of definitionButtons) button.addEventListener("click", () => show(null));
  let lastNode = null;
  for (const node of nodes) node.addEventListener("click", () => { lastNode = node; show(node.dataset.graphNode); });
  const close = () => {
    inspector.hidden = true;
    split.classList.add("solo");
    for (const node of nodes) node.setAttribute("aria-pressed", "false");
    for (const button of definitionButtons) button.setAttribute("aria-expanded", "false");
    sizeCanvasPane();
    (lastNode || definitionButtons[0]).focus({preventScroll:true});
  };
  root.querySelector("[data-graph-close]").addEventListener("click", close);
  inspector.addEventListener("keydown", e => { if (e.key === "Escape") { e.preventDefault(); close(); } });
  // Only numerical, measured geometry enters the same-origin external CSSOM.
  // The clipping pan-space removes the unscaled layout footprint: native scroll
  // bounds must match the transformed cards AND SVG, including below 30% Fit.
  const space = root.querySelector("[data-graph-pan-space]");
  let stageRule, spaceRule, paneRule;
  for (const sheet of document.styleSheets) {
    try {
      if (!sheet.href || new URL(sheet.href, location.href).origin !== location.origin) continue;
      const i = sheet.insertRule(".graph-workspace .graph-pan-space {}", sheet.cssRules.length);
      spaceRule = sheet.cssRules[i].style;
      const j = sheet.insertRule(".graph-workspace .graph-stage {}", sheet.cssRules.length);
      stageRule = sheet.cssRules[j].style;
      const k = sheet.insertRule(".graph-workspace .gw-canvas {}", sheet.cssRules.length);
      paneRule = sheet.cssRules[k].style;
      break;
    } catch (_) { /* Native unscaled canvas/text remains usable without CSSOM. */ }
  }
  if (!stageRule || !spaceRule || !space) return;
  stageRule.position = "absolute";
  let zoom = 1, fitted = false;
  const fitScale = () => Math.max(Number.EPSILON, Math.min(1,
    Math.max(1, viewport.clientWidth-8)/Math.max(1, stage.scrollWidth),
    Math.max(1, viewport.clientHeight-8)/Math.max(1, stage.scrollHeight)));
  const setZoom = (value, fit = false) => {
    if (!Number.isFinite(value)) return;
    const old = zoom;
    const x = (viewport.scrollLeft + viewport.clientWidth/2)/old;
    const y = (viewport.scrollTop + viewport.clientHeight/2)/old;
    zoom = Math.max(Math.min(.3, fitScale()), Math.min(2, value));
    fitted = fit;
    stageRule.transform = `scale(${zoom})`;
    spaceRule.width = `${stage.scrollWidth*zoom}px`;
    spaceRule.height = `${stage.scrollHeight*zoom}px`;
    viewport.scrollLeft = fit ? 0 : x*zoom - viewport.clientWidth/2;
    viewport.scrollTop = fit ? 0 : y*zoom - viewport.clientHeight/2;
    const percent = zoom*100;
    root.querySelector("[data-graph-scale]").textContent = `${percent < 10 ? percent.toFixed(1) : Math.round(percent)}%`;
  };
  const fit = () => setZoom(fitScale(), true);
  const pane = root.querySelector(".gw-canvas");
  sizeCanvasPane = () => {
    // Natural definition height, not zoomed footprint, drives the adaptive pane.
    // Reset first so inspector/viewport wrapping can be measured after resizing.
    paneRule.height = "auto";
    const chrome = Math.max(0, pane.offsetHeight-viewport.clientHeight);
    const cap = Math.min(innerWidth <= 760 ? 360 : 620,
      Math.max(240, Math.round(innerHeight*.68)));
    let height = Math.max(240, Math.min(cap, stage.offsetHeight+chrome+10));
    if (!inspector.hidden && Math.abs(pane.getBoundingClientRect().top-inspector.getBoundingClientRect().top)<2)
      height = Math.max(height, Math.min(cap, inspector.offsetHeight));
    paneRule.height = `${Math.round(height)}px`;
  };
  sizeCanvasPane();
  window.addEventListener("resize", () => { sizeCanvasPane(); if (fitted) fit(); });
  if (typeof ResizeObserver !== "undefined") new ResizeObserver(sizeCanvasPane).observe(inspector);
  setZoom(1);
  if (typeof ResizeObserver !== "undefined") new ResizeObserver(() => {
    if (fitted) fit();
  }).observe(viewport);
  root.querySelector("[data-graph-fit]").addEventListener("click", fit);
  for (const button of root.querySelectorAll("[data-graph-zoom]")) button.addEventListener("click", () => setZoom(zoom+(button.dataset.graphZoom === "in" ? .1 : -.1)));
  viewport.addEventListener("keydown", e => {
    if (e.target !== viewport) return;
    const moves = {ArrowLeft:[-80,0],ArrowRight:[80,0],ArrowUp:[0,-80],ArrowDown:[0,80]};
    if (moves[e.key]) { e.preventDefault(); viewport.scrollBy(...moves[e.key]); }
    else if (e.key === "+" || e.key === "=") { e.preventDefault(); setZoom(zoom+.1); }
    else if (e.key === "-") { e.preventDefault(); setZoom(zoom-.1); }
    else if (e.key === "0") { e.preventDefault(); fit(); }
  });
  // Native scrolling handles touch with browser zoom preserved. Mouse drag
  // captures only the canvas background, never a node or inspector control.
  let drag = null;
  viewport.addEventListener("pointerdown", e => {
    if (e.pointerType !== "mouse" || e.button !== 0 || e.target.closest("button")) return;
    drag = {id:e.pointerId,x:e.clientX,y:e.clientY,left:viewport.scrollLeft,top:viewport.scrollTop};
    viewport.setPointerCapture(e.pointerId);
    e.preventDefault();
  });
  viewport.addEventListener("pointermove", e => {
    if (!drag || drag.id !== e.pointerId) return;
    viewport.scrollLeft = drag.left + drag.x - e.clientX;
    viewport.scrollTop = drag.top + drag.y - e.clientY;
  });
  const stop = () => { drag = null; };
  viewport.addEventListener("pointerup", stop);
  viewport.addEventListener("pointercancel", stop);
  viewport.addEventListener("lostpointercapture", stop);
})();
