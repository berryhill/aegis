"use strict";
// Authenticated exact-run read reducer, separate from pure navigation.
(() => {
  const root = document.querySelector("[data-queue-workspace]");
  if (!root) return;
  const stage = root.querySelector("[data-queue-stage]");
  let nodes = [], panels = [];
  // The server projects only this exact run. Full snapshots are folded by
  // checkpoint/ordinal identity; a delayed response cannot roll back facts.
  if (!root.dataset.debuggerInitial) return;
  let latest = JSON.parse(root.dataset.debuggerInitial);
  let following = true, replay = Infinity, fresh = false, lastRead = 0, pausedFacts = [];
  let scope = latest.direct ? latest.nodes[0].node_id : "";
  // Presentation preferences only. Never persist facts, credentials or authority.
  const storageKey = "aegis-debugger/" + JSON.stringify([latest.queue_id, latest.graph_run_id, latest.snapshot_digest]);
  const factKey = f => `${f.node}/${f.attempt}/${f.sequence}/${f.ordinal}/${f.digest}`;
  let selection = {}, pausedKeys = null;
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey));
    if (saved && latest.nodes.some(n => n.node_id === saved.scope) || saved?.scope === "" && !latest.direct) {
      scope = saved.scope;
      if (saved.selection && typeof saved.selection === "object") selection = saved.selection;
      if (saved.following === false && Number.isInteger(saved.replay) && saved.replay >= 0 && Array.isArray(saved.pausedKeys)) {
        following = false; replay = saved.replay; pausedKeys = new Set(saved.pausedKeys.filter(k => typeof k === "string"));
      }
    }
  } catch (_) { /* storage unavailable or malformed: use exact server history */ }
  const persist = () => {
    try { sessionStorage.setItem(storageKey, JSON.stringify({scope, selection, following, replay: following ? 0 : replay, pausedKeys: following ? [] : pausedFacts.map(factKey)})); } catch (_) {}
  };
  root.addEventListener("queue-selection-changed", event => {
    const index = event.detail.index;
    selection[scope] = index === null ? null : nodes[Number(index)]?.querySelector("strong")?.textContent;
    persist();
  });
  const scopeInput = root.querySelector("[data-debugger-scope]");
  const historyInput = root.querySelector("[data-debugger-replay]");
  const followButton = root.querySelector("[data-debugger-follow]");
  const status = root.querySelector("[data-debugger-status]");
  const history = root.querySelector("[data-debugger-history]");
  const make = (tag, text, cls) => { const el = document.createElement(tag); if (text !== undefined) el.textContent = text; if (cls) el.className = cls; return el; };
  const allFacts = () => latest.nodes.flatMap(n => n.attempts.flatMap(a => a.facts.map(f => ({ ...f, node: n.node_id, attempt: a.id, number: a.number, execution: n.execution_id }))));
  if (pausedKeys) pausedFacts = allFacts().filter(f => pausedKeys.has(factKey(f)));
  const factText = f => `${f.at} · ${f.node}/${f.step_id} · ${f.kind}: ${f.outcome} · Queue attempt ${f.number} (${f.attempt}) · implementation pass ${f.pass || "not started"} · visit ${f.visit} / local retry ${f.retry} · checkpoint ${f.sequence}:${f.ordinal} @ ${f.digest}${f.transition_id ? ` · ${f.transition_id} → ${f.to_step_id}` : ""}${f.failure_code ? ` · reason ${f.failure_code}` : ""}`;
  const queueClosed = value => value.terminal || ["succeeded", "failed", "denied", "cancelled", "expired", "revoked"].includes(value.state);
  let layoutKey = "";
  const render = (liveUpdate = false) => {
    const facts = following ? allFacts() : pausedFacts, visible = following ? facts : facts.slice(0, replay);
    historyInput.max = String(facts.length); historyInput.value = String(following ? facts.length : Math.min(replay, facts.length));
    followButton.textContent = following ? "Pause following" : "Follow live";
    const closed = queueClosed(latest);
    root.dataset.debuggerFresh = String(fresh && following && !closed);
    status.textContent = `${following ? "Current" : "Historical replay — presentation paused; execution is not controlled"} · ${fresh ? "fresh observations" : "stale / disconnected — progress unconfirmed"} · Queue ${latest.state}${latest.terminal ? " · evidence-backed disposition " + latest.disposition_id : " · no evidence-backed terminal disposition"}`;
    root.querySelector("[data-debugger-notice]").textContent = latest.notice;
    root.querySelector("[data-debugger-identity]").textContent = `Graph run ${latest.graph_run_id} · snapshot ${latest.snapshot_digest} · ${scope ? "Loop execution " + (latest.nodes.find(n => n.node_id === scope)?.execution_id || "not observed") : "Graph definition context"}`;
    if (!scopeInput.options.length) {
      if (!latest.direct) { const option = make("option", "Graph run topology"); option.value = ""; scopeInput.append(option); }
      for (const n of latest.nodes) { const option = make("option", `${n.node_id} → ${n.loop.id} r${n.loop.revision} @ ${n.loop.digest}`); option.value = n.node_id; scopeInput.append(option); }
      scopeInput.value = scope;
    }
    const loop = latest.nodes.find(n => n.node_id === scope);
    const definitions = loop ? loop.steps : latest.nodes.map(n => ({ id: n.node_id, kind: "Graph node", terminal: "" }));
    const edges = loop ? loop.edges : (latest.dependencies || []);
    const key = latest.snapshot_digest + "/" + scope;
    if (key !== layoutKey) {
      layoutKey = key; stage.replaceChildren();
      const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg"); svg.classList.add("queue-edge-layer");
      svg.setAttribute("viewBox", `0 0 ${definitions.length * 244 + 32} 166`); svg.setAttribute("aria-hidden", "true");
      stage.append(svg); stage.style.gridTemplateColumns = `repeat(${definitions.length},212px)`;
      stage.dataset.columns = String(definitions.length); stage.dataset.rows = "1";
      const panelParent = root.querySelector("#queue-context .qw-body");
      for (const panel of root.querySelectorAll("[data-queue-node-panel]")) panel.remove();
      panels = []; nodes = [];
      for (const [index, def] of definitions.entries()) {
        const button = make("button", undefined, "queue-node"); button.type = "button";
        button.dataset.queueNode = String(index); button.dataset.gridColumn = String(index + 1); button.dataset.gridRow = "1";
        button.style.gridColumn = String(index + 1); button.setAttribute("aria-controls", "queue-context"); button.setAttribute("aria-pressed", String(selection[scope] === def.id));
        button.append(make("strong", def.id), make("span", "unknown", "queue-node-state"), make("span", def.kind, "queue-node-execution"));
        stage.append(button); nodes.push(button);
        const panel = make("section"); panel.dataset.queueNodePanel = String(index); panel.hidden = true;
        // Layout replacement must retain the definition/evidence navigation
        // supplied by the server-rendered node inspector.
        const back = make("button", "← Back to queue", "ghost"); back.type = "button";
        back.dataset.queueDefinition = ""; back.setAttribute("aria-expanded", "false");
        panel.append(back, make("h4", def.id));
        if (!loop) {
          const child = latest.nodes[index];
          panel.append(make("p", `${child.loop.id} r${child.loop.revision} @ ${child.loop.digest} · ${child.execution_id || "execution not observed"}`));
          const drill = make("button", "Open exact Loop execution"); drill.type = "button"; drill.addEventListener("click", () => { scope = child.node_id; selection[scope] ??= child.steps[0]?.id; scopeInput.value = scope; render(); persist(); }); panel.append(drill);
        } else panel.append(make("p", `Loop ${loop.loop.id} r${loop.loop.revision} @ ${loop.loop.digest} · ${loop.execution_id || "execution not observed"} · interpreter observations are not Queue completion`));
        const list = make("ol"); list.dataset.debuggerNodeHistory = ""; panel.append(list); panelParent.append(panel); panels.push(panel);
      }
      for (const [index, edge] of edges.entries()) {
        const a = definitions.findIndex(s => s.id === edge.from), b = definitions.findIndex(s => s.id === edge.to); if (a < 0 || b < 0) continue;
        const path = document.createElementNS(svg.namespaceURI, "path"), x1 = a * 244 + 244, x2 = b * 244 + 32;
        path.setAttribute("d", b === a + 1 ? `M ${x1} 83 C ${x1+16} 83 ${x2-16} 83 ${x2} 83` : `M ${x1} 83 L ${x1+12} 20 L ${x2-12} 20 L ${x2} 83`);
        path.dataset.edgeIndex = String(index); path.dataset.edgeId = edge.id; path.classList.add("queue-edge-path", "outcome-pending");
        const caption = document.createElementNS(svg.namespaceURI, "title"); caption.textContent = `${edge.id}: ${edge.from} → ${edge.to} · bound ${edge.limit || "definition"}`; path.append(caption); svg.append(path);
      }
      root.dispatchEvent(new Event("queue-layout-changed"));
    }
    for (const [index, def] of definitions.entries()) {
      const selectedFacts = visible.filter(f => loop ? f.node === scope && f.step_id === def.id : f.node === def.id);
      let state = "not observed", running = false;
      for (const f of selectedFacts) {
        if (f.kind === "step_start") { state = "running"; running = true; }
        else if (f.kind === "step_finish") { state = f.outcome; running = false; }
        else if (f.kind === "verification" && f.outcome === "failed") state = "failed";
      }
      if (!loop) {
        // A child boundary describes that child, not aggregate Graph success.
        const last = selectedFacts.at(-1);
        state = last ? (last.kind === "interpreter_terminal" ? "interpreter " + last.outcome : "child " + last.kind + ": " + last.outcome) : "not observed";
        running = last?.kind === "step_start";
      }
      const node = nodes[index]; node.className = `queue-node state-${state.replaceAll(" ", "-")}`;
      node.classList.toggle("current", running && !closed && following && fresh);
      node.querySelector(".queue-node-state").textContent = running && closed && following ? "finish unobserved (last observed running; Queue " + latest.state + ")" : state;
      const list = panels[index].querySelector("[data-debugger-node-history]"); list.replaceChildren(...selectedFacts.map(f => make("li", factText(f))));
      if (!selectedFacts.length) list.append(make("li", "No observed boundaries. Timing, pass and result unknown."));
    }
    for (const path of stage.querySelectorAll("[data-edge-id]")) {
      const occurrence = loop && visible.filter(f => f.node === scope && f.kind === "transition" && f.transition_id === path.dataset.edgeId).at(-1);
      const taken = !!occurrence, identity = occurrence ? `${occurrence.attempt}/${occurrence.sequence}/${occurrence.ordinal}` : "";
      const old = path.dataset.edgeOccurrence; path.dataset.edgeOccurrence = identity; path.dataset.edgeOutcome = taken ? "taken" : "pending";
      path.classList.toggle("outcome-taken", taken); path.classList.toggle("outcome-pending", !taken);
      path.classList.remove("observed-now");
      if (liveUpdate && fresh && following && !closed && taken && old !== undefined && old !== identity) { path.classList.add("observed-now"); path.addEventListener("animationend", () => path.classList.remove("observed-now"), {once:true}); }
    }
    history.replaceChildren(...visible.map(f => make("li", factText(f))));
    if (!visible.length) history.append(make("li", "No observed boundaries. Legacy cursors are not inferred step events."));
    const evidence = [latest.disposition_id, ...(latest.artifact_ids || []), ...(latest.receipt_ids || [])].filter(Boolean);
    if (following && evidence.length) history.append(make("li", "Authoritative evidence references: " + evidence.join(" · ")));
    if (closed) for (const button of root.querySelectorAll("[data-contextual-action] button")) button.disabled = true;
  };
  followButton.addEventListener("click", () => { if (following) pausedFacts = allFacts(); following = !following; replay = pausedFacts.length; render(); persist(); });
  root.querySelector("[data-debugger-current]").addEventListener("click", () => { following = true; replay = Infinity; render(); persist(); });
  historyInput.addEventListener("input", () => { if (following) pausedFacts = allFacts(); following = false; replay = Number(historyInput.value); render(); persist(); });
  scopeInput.addEventListener("change", () => { scope = scopeInput.value; render(); persist(); });
  const poll = async () => {
    if (!root.isConnected) return;
    const controller = new AbortController(), deadline = setTimeout(() => controller.abort(), 4000);
    try {
      const url = new URL(root.dataset.debuggerUrl, location.origin); url.searchParams.set("cursor", latest.watermark);
      const response = await fetch(url, { credentials:"same-origin", cache:"no-store", signal:controller.signal, headers:{Accept:"application/json"} });
      if (response.status === 401 || response.status === 403) { root.replaceChildren(make("p", "Session expired. Authenticate again to inspect execution.")); return; }
      if (!response.ok) throw Error("observations unavailable");
      const next = await response.json();
      if (next.queue_id !== latest.queue_id || next.snapshot_digest !== latest.snapshot_digest || next.version < latest.version || (queueClosed(latest) && (!queueClosed(next) || latest.state !== next.state))) throw Error("stale snapshot");
      const oldFacts = allFacts();
      const nextFacts = new Map(next.nodes.flatMap(n => n.attempts.flatMap(a => a.facts.map(f => [`${a.id}/${f.sequence}/${f.ordinal}`, f.digest]))));
      if (oldFacts.some(f => nextFacts.get(`${f.attempt}/${f.sequence}/${f.ordinal}`) !== f.digest)) throw Error("history gap or substitution");
      latest = next; fresh = true; lastRead = Date.now(); render(true);
    } catch (_) { fresh = false; render(); }
    finally { clearTimeout(deadline); }
    if (root.isConnected) setTimeout(poll, 1500);
  };
  render(); poll();
  const staleCheck = setInterval(() => { if (!root.isConnected) { clearInterval(staleCheck); return; } if (fresh && Date.now() - lastRead > 6000) { fresh = false; render(); } }, 2000);
})()