"use strict";

// Fragments are not sent to the server. Resolve Graph bookmarks through the
// existing authenticated read route; a record key selects only a rendered view.
(() => {
  const resolveGraph = () => {
    if (location.pathname !== "/console/graphs") return;
    const prefix = "#/graphs/";
    if (!location.hash.startsWith(prefix)) return;
    let key;
    try { key = decodeURIComponent(location.hash.slice(prefix.length)); }
    catch (_) { return; }
    if (!key || key.length > 1024 || /[\u0000-\u001f\u007f]/.test(key)) return;
    const target = new URL(location.href);
    if (target.searchParams.get("record_key") === key) return;
    target.searchParams.set("record_key", key);
    location.replace(target.href);
  };
  resolveGraph();
  window.addEventListener("hashchange", resolveGraph);
})();

// Collection context is presentation-only. It can restore viewport and focus to
// a rendered record, but it never participates in server decisions.
(() => {
  const body = document.body;
  if (!body) return;

  // A fragment identifies a record only. Exact lookup stays on the
  // authenticated server path; ignore all other fragment input.
  const resolveAgentFragment = () => {
    const route = location.hash.match(/^#\/agents\/([^/?#]+)$/);
    if (!route) return;
    let id;
    try { id = decodeURIComponent(route[1]); } catch (_) { return; }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/.test(id)) return;
    const target = new URL(location.href);
    if (target.searchParams.get("record_key") === id && target.pathname === "/console/agents") return;
    const current = target.searchParams.get("record_key");
    target.pathname = "/console/agents";
    target.searchParams.set("record_key", id);
    if (current !== id) target.searchParams.delete("revision");
    location.replace(target.href);
  };
  resolveAgentFragment();
  addEventListener("hashchange", resolveAgentFragment);

  const collectionURL = body.dataset.collectionUrl || "";
  if (!collectionURL) return;
  const storageKey = "aegis.console.collection.v1:" + collectionURL;
  const inventory = document.getElementById("credential-inventory");
  const credentials = location.pathname === "/console/credentials";
  const detailOpen = body.dataset.detailOpen === "true";
  const boundedCoordinate = (value) =>
    Number.isFinite(value) && value >= 0 && value <= 10000000 ? Math.round(value) : 0;
  const readContext = () => {
    try {
      const entry = history.state?.aegisCollection;
      if (entry?.collectionURL === collectionURL) return entry;
      return JSON.parse(sessionStorage.getItem(storageKey) || "null");
    } catch (_) { return null; }
  };
  const snapshot = (recordKey) => ({
    collectionURL, recordKey,
    x: boundedCoordinate(scrollX), y: boundedCoordinate(scrollY),
    inventoryY: inventory ? boundedCoordinate(inventory.scrollTop) : 0,
  });
  const saveEntry = (context) => {
    try { history.replaceState({...history.state, aegisCollection: context}, ""); }
    catch (_) { /* Native navigation still works if history state is unavailable. */ }
  };
  // Native links carry record_key for no-script access. A pasted/edited hash is
  // translated to the same server route, never used to manufacture an inspector.
  const reconcileFragment = () => {
    if (!credentials || location.hash.length > 12302 || !location.hash.startsWith("#/credentials")) return false;
    const match = /^#\/credentials(?:\/([^/]+))?$/.exec(location.hash);
    if (!match) return false;
    let key;
    try { key = match[1] ? decodeURIComponent(match[1]) : ""; }
    catch (_) { return false; }
    if (key.length > 1024) return false;
    const target = new URL(location.href);
    if ((target.searchParams.get("record_key") || "") === key) return false;
    if (key) target.searchParams.set("record_key", key);
    else target.searchParams.delete("record_key");
    location.replace(target.href);
    return true;
  };
  if (reconcileFragment()) return;
  window.addEventListener("hashchange", () => {
    // An explicit removal closes the inspector; initial fragmentless native
    // record_key links remain valid for progressive enhancement.
    if (credentials && !location.hash) {
      const target = new URL(location.href);
      target.searchParams.delete("record_key");
      location.replace(target.href);
    } else reconcileFragment();
  });

  for (const link of document.querySelectorAll("a[data-detail-link][data-record-key]")) {
    link.addEventListener("click", () => {
      const recordKey = link.dataset.recordKey || "";
      if (!recordKey || recordKey.length > 1024) return;
      const context = snapshot(recordKey);
      saveEntry(context);
      try {
        if (!detailOpen) sessionStorage.setItem(storageKey, JSON.stringify(context));
      } catch (_) { /* Native links remain the storage-denied fallback. */ }
    });
  }
  window.addEventListener("pagehide", () => {
    const context = readContext();
    if (context && typeof context.recordKey === "string") saveEntry(snapshot(context.recordKey));
  });

  const restore = () => {
    const context = readContext();
    if (!context || typeof context.recordKey !== "string" || context.recordKey.length > 1024) return;
    const selected = Array.from(document.querySelectorAll("a[data-record-key]"))
      .find((link) => link.dataset.recordKey === context.recordKey);
    requestAnimationFrame(() => {
      if (inventory) inventory.scrollTop = boundedCoordinate(Number(context.inventoryY));
      if (detailOpen) return;
      const x = boundedCoordinate(Number(context.x));
      const y = boundedCoordinate(Number(context.y));
      if (credentials && selected) {
        for (const link of document.querySelectorAll("a[data-record-key]")) {
          link.closest("tr").dataset.selected = String(link === selected);
          link.setAttribute("aria-current", String(link === selected));
        }
      }
      scrollTo(x, y);
      if (selected) selected.focus({preventScroll: true});
      else inventory?.focus({preventScroll: true});
      scrollTo(x, y);
    });
  };
  restore();
  // BFCache restores the original DOM without rerunning this script. The
  // entry snapshot captured on pagehide, not another entry's state, wins.
  // Refresh only the read-only collection route on an actual return. Native
  // reload repeats authentication and retains the full filter/selection URL.
  // Never replace in-progress edits or a mutation dialog; there is no timer.
  let away = false, reloading = false, dirty = false;
  const refreshCredentials = () => {
    if (!credentials || reloading || dirty || document.visibilityState === "hidden" ||
        document.querySelector("dialog[open], [role='dialog'][aria-modal='true']")) return;
    reloading = true;
    const context = readContext();
    saveEntry(snapshot(context?.recordKey || new URL(location.href).searchParams.get("record_key") || ""));
    location.reload();
  };
  if (credentials) {
    document.addEventListener("input", () => { dirty = true; });
    document.addEventListener("change", () => { dirty = true; });
    window.addEventListener("blur", () => { away = true; });
    window.addEventListener("focus", () => {
      if (!away) return;
      away = false;
      refreshCredentials();
    });
  }
  window.addEventListener("pageshow", (event) => {
    if (event.persisted) { restore(); refreshCredentials(); }
  });
})();
