"use strict";
// Graph-only read observation. The server authenticates every GET; this script
// neither supplies authority nor changes session lifetime. Keep out of NavigationJS.
(() => {
  const root = document.querySelector("[data-graph-workspace]");
  if (!root) return;
  let stopped = false, pending = false, timer, epoch = 0;
  const clear = (confirmedLoss) => {
    if (stopped) return;
    stopped = true;
    clearTimeout(timer);
    const message = document.createElement("p");
    message.setAttribute("role", "alert");
    message.dataset.graphAuthCleared = confirmedLoss ? "authentication-required" : "unavailable";
    message.textContent = confirmedLoss ? "Authentication required. Protected Graph details cleared." :
      "Authentication could not be verified. Protected Graph details cleared.";
    const link = document.createElement("a");
    link.href = "/console/graphs";
    link.textContent = "Reload authenticated Console";
    document.body.replaceChildren(message, link);
  };
  const observe = async () => {
    if (stopped || pending || document.visibilityState === "hidden") return;
    pending = true;
    const observedEpoch = epoch;
    const controller = new AbortController();
    const deadline = setTimeout(() => controller.abort(), 5000);
    try {
      const url = new URL(location.href);
      url.hash = "";
      if (url.origin !== location.origin || url.pathname !== "/console/graphs") throw new Error("unexpected Graph route");
      const response = await fetch(url.href, {method:"GET", credentials:"same-origin",
        cache:"no-store", redirect:"error", headers:{Accept:"text/html"}, signal:controller.signal});
      if (response.status === 401) { clear(true); return; }
      if (!response.ok || !(response.headers.get("content-type") || "").includes("text/html")) throw new Error("unavailable read");
      // Inert parser: never insert fetched scripts, CSRF fields or protected data.
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      if (page.querySelector("#session-form") && !page.querySelector("#logout")) { clear(true); return; }
      if (!page.querySelector("#logout") || !page.querySelector("[data-graph-workspace]")) throw new Error("unknown read");
      if (!stopped && observedEpoch === epoch && document.visibilityState !== "hidden") root.hidden = false;
    } catch (_) { clear(false); }
    finally {
      clearTimeout(deadline);
      pending = false;
      if (!stopped) timer = setTimeout(observe, observedEpoch === epoch ? 15000 : 0);
    }
  };
  const immediate = () => { epoch++; root.hidden = true; clearTimeout(timer); void observe(); };
  window.addEventListener("focus", immediate);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") { epoch++; root.hidden = true; clearTimeout(timer); }
    else immediate();
  });
  // A cached document contains no protected body to resurrect. Restoration must
  // repeat a native authenticated server render, never reinsert old JSON/cards.
  window.addEventListener("pagehide", () => clear(false));
  window.addEventListener("pageshow", e => { if (e.persisted) location.reload(); else immediate(); });
  immediate();
})();
