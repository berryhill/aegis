"use strict";

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
