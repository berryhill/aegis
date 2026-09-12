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

  const collectionURL = body.dataset.collectionUrl || "";
  if (!collectionURL) return;

  const storageKey = "aegis.console.collection.v1:" + collectionURL;
  const boundedCoordinate = (value) =>
    Number.isFinite(value) && value >= 0 && value <= 10000000 ? Math.round(value) : 0;

  for (const link of document.querySelectorAll("a[data-detail-link][data-record-key]")) {
    link.addEventListener("click", () => {
      const recordKey = link.dataset.recordKey || "";
      if (!recordKey || recordKey.length > 1024) return;
      try {
        sessionStorage.setItem(storageKey, JSON.stringify({
          recordKey,
          x: boundedCoordinate(scrollX),
          y: boundedCoordinate(scrollY),
        }));
      } catch (_) {
        // Native links remain the complete no-script/storage-denied fallback.
      }
    });
  }

  if (body.dataset.detailOpen === "true") return;

  let context;
  try {
    context = JSON.parse(sessionStorage.getItem(storageKey) || "null");
  } catch (_) {
    return;
  }
  if (!context || typeof context.recordKey !== "string" || context.recordKey.length > 1024) return;

  const selected = Array.from(document.querySelectorAll("a[data-record-key]"))
    .find((link) => link.dataset.recordKey === context.recordKey);
  if (!selected) return;

  const x = boundedCoordinate(Number(context.x));
  const y = boundedCoordinate(Number(context.y));
  requestAnimationFrame(() => {
    scrollTo(x, y);
    selected.focus({preventScroll: true});
    // Focus must not replace the collection viewport selected by the operator.
    scrollTo(x, y);
  });
})();
