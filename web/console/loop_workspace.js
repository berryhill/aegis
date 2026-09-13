"use strict";
// Presentation only: no network, storage or route mutations.
(() => {
  const root = document.querySelector("[data-loop-workspace]");
  if (!root) return;
  const viewport = root.querySelector("[data-loop-viewport]");
  const stage = root.querySelector("[data-loop-stage]");
  const inspector = root.querySelector("#loop-context");
  const title = root.querySelector("#loop-context-title");
  const split = root.querySelector(".loop-workspace");
  const definition = root.querySelector("[data-loop-definition-panel]");
  const panels = Array.from(root.querySelectorAll("[data-loop-node-panel]"));
  const nodes = Array.from(root.querySelectorAll("[data-loop-node]"));
  const definitionButtons = Array.from(root.querySelectorAll("[data-loop-definition]"));
  if (!stage) return;
  // Match the accepted workspace's canvas-first entry. Keep the server-rendered
  // definition visible without JavaScript; enhancement makes it contextual.
  if (inspector) inspector.hidden = true;
  if (split) split.classList.add("solo");
  for (const button of definitionButtons) button.setAttribute("aria-expanded", "false");
  const show = (index) => {
    if (index !== null && !panels.some(p => p.dataset.loopNodePanel === index)) return;
    if (inspector) inspector.hidden = false;
    if (split) split.classList.remove("solo");
    if (definition) definition.hidden = index !== null;
    for (const panel of panels) panel.hidden = panel.dataset.loopNodePanel !== index;
    for (const node of nodes) node.setAttribute("aria-pressed", String(node.dataset.loopNode === index));
    for (const button of definitionButtons) button.setAttribute("aria-expanded", String(index === null));
    title.textContent = index === null ? "Loop definition" : "Node inspector";
    title.focus({preventScroll: true});
    if (matchMedia("(max-width:1080px)").matches && inspector) inspector.scrollIntoView({block:"start"});
  };
  for (const button of definitionButtons) button.addEventListener("click", () => show(null));
  let lastNode = null;
  for (const node of nodes) node.addEventListener("click", () => { lastNode = node; show(node.dataset.loopNode); });
  const close = () => {
    if (inspector) inspector.hidden = true;
    if (split) split.classList.add("solo");
    for (const node of nodes) node.setAttribute("aria-pressed", "false");
    for (const button of definitionButtons) button.setAttribute("aria-expanded", "false");
    (lastNode || definitionButtons[0]).focus({preventScroll:true});
  };
  const closeBtn = root.querySelector("[data-loop-close]");
  if (closeBtn) closeBtn.addEventListener("click", close);
  if (inspector) inspector.addEventListener("keydown", e => { if (e.key === "Escape") { e.preventDefault(); close(); } });
  // Bounded zoom: scale the grid stage without changing the layout grid.
  // The grid keeps nodes positioned by track; transform only re-renders
  // their visual size, and SVG paths are expressed as percentages so the
  // arrows remain attached without inline-style layout.
  let zoom = 1;
  const setZoom = (value, fit = false) => {
    const old = zoom;
    const x = (viewport.scrollLeft + viewport.clientWidth/2)/old;
    const y = (viewport.scrollTop + viewport.clientHeight/2)/old;
    zoom = Math.max(.3, Math.min(2, value));
    stage.style.transform = `scale(${zoom})`;
    viewport.scrollLeft = fit ? 0 : x*zoom - viewport.clientWidth/2;
    viewport.scrollTop = fit ? 0 : y*zoom - viewport.clientHeight/2;
    root.querySelector("[data-loop-scale]").textContent = `${Math.round(zoom*100)}%`;
  };
  const fit = () => {
    const sw = stage.scrollWidth, sh = stage.scrollHeight;
    const fitZoom = Math.min(1,(viewport.clientWidth-8)/sw,(viewport.clientHeight-8)/sh);
    setZoom(Math.max(.3, Math.min(2, fitZoom)), true);
  };
  const fitBtn = root.querySelector("[data-loop-fit]");
  if (fitBtn) fitBtn.addEventListener("click", fit);
  for (const button of root.querySelectorAll("[data-loop-zoom]")) button.addEventListener("click", () => setZoom(zoom+(button.dataset.loopZoom === "in" ? .1 : -.1)));
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