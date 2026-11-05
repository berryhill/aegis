"use strict";
// Loop-only authenticated read observation, separate from presentation-only
// NavigationJS. No authority fields, action requests or session refresh.
(() => {
  const root = document.querySelector("[data-loop-workspace]");
  if (!root) return;
  let stopped = false, pending = false, timer, epoch = 0;
  const clear = (confirmedLoss) => {
    if (stopped) return;
    stopped = true;
    clearTimeout(timer);
    const message = document.createElement("p");
    message.setAttribute("role", "alert");
    message.dataset.loopAuthCleared = confirmedLoss ? "authentication-required" : "unavailable";
    message.textContent = confirmedLoss ? "Authentication required. Protected Loop details cleared." :
      "Authentication could not be verified. Protected Loop details cleared.";
    const link = document.createElement("a");
    link.href = "/console/loops";
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
      if (url.origin !== location.origin || url.pathname !== "/console/loops") throw new Error("unexpected Loop route");
      const response = await fetch(url.href, {method:"GET", credentials:"same-origin",
        cache:"no-store", redirect:"error", headers:{Accept:"text/html"}, signal:controller.signal});
      if (response.status === 401) { clear(true); return; }
      if (!response.ok || !(response.headers.get("content-type") || "").includes("text/html")) throw new Error("unavailable read");
      // Inert parsing only: never insert fetched scripts, CSRF or protected data.
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      if (page.querySelector("#session-form") && !page.querySelector("#logout")) { clear(true); return; }
      if (!page.querySelector("#logout") || !page.querySelector("[data-loop-workspace]")) throw new Error("unknown read");
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
  // Do not resurrect a cached protected body; repeat native authentication.
  window.addEventListener("pagehide", () => clear(false));
  window.addEventListener("pageshow", e => { if (e.persisted) location.reload(); else immediate(); });
  immediate();
})();
