#!/usr/bin/env python3
"""Real-console Graph workspace acceptance under production CSP.

Drives the authenticated Aegis console (booted by the Go test harness) with
Chromium at desktop and narrow widths. Proves routing, refresh/back,
collection absence, connected geometry, selection without network mutations,
bounded viewport controls, responsive stacking and no-script textual access.

No authentication, admission or authority claims are made here: the password
comes from the test harness and selection must not issue any request.
"""
import os
import pathlib

from playwright.sync_api import sync_playwright

BASE = os.environ["AEGIS_CONSOLE_BASE"]
PASSWORD = os.environ["AEGIS_CONSOLE_PASSWORD"]
ARTIFACTS = pathlib.Path(os.environ.get("AEGIS_GRAPH_ARTIFACTS", ".aegis-graph-proof"))
ARTIFACTS.mkdir(parents=True, exist_ok=True)

DETAIL = BASE + "/console/graphs?record_key=graph-proof%3A1#/graphs/graph-proof:1"
violations = []


def login(page):
    page.goto(BASE + "/console/agents")
    page.fill("#password", PASSWORD)
    page.click("#session-form button[type=submit]")
    page.wait_for_url("**/console/agents**")


def connected_geometry(page):
    return page.evaluate(
        """() => {
      const nodes = [...document.querySelectorAll('[data-graph-node]')];
      const edges = [...document.querySelectorAll('.graph-edge-path')];
      if (edges.length === 0 || nodes.length < 2) return {ready: false};
      const matrix = edges[0].getScreenCTM();
      if (!matrix) return {ready: false};
      const start = edges[0].getPointAtLength(0).matrixTransform(matrix);
      const end = edges[0].getPointAtLength(edges[0].getTotalLength()).matrixTransform(matrix);
      const from = nodes.find(n => n.textContent.includes('intake'));
      const to = nodes.find(n => n.textContent.includes('review'));
      const fromRect = from.getBoundingClientRect();
      const toRect = to.getBoundingClientRect();
      return {
        ready: true,
        startAtNode: Math.abs(start.x - fromRect.right) < 4 && start.y > fromRect.top && start.y < fromRect.bottom,
        endAtNode: Math.abs(end.x - toRect.left) < 4 && end.y > toRect.top && end.y < toRect.bottom,
        start: [start.x, start.y], end: [end.x, end.y],
        from: [fromRect.left, fromRect.right, fromRect.top, fromRect.bottom],
        to: [toRect.left, toRect.right, toRect.top, toRect.bottom],
      };
    }"""
    )


def desktop(page):
    login(page)
    page.goto(BASE + "/console/graphs")
    page.wait_for_selector("a[data-detail-link]")
    cards = page.locator("a[data-detail-link]").count()
    assert cards >= 1, "graph collection has no record cards"
    historical = page.locator('a[data-record-key="graph-proof:1"]')
    if historical.count() == 1:
        historical.click()
    else:
        assert historical.count() == 0 and page.locator('a[data-record-key^="graph-proof:"]').count() >= 1, \
            "graph-proof record missing from collection"
        page.goto(DETAIL)
    page.wait_for_selector("[data-graph-workspace]")
    url = page.url
    assert url.endswith("#/graphs/graph-proof:1"), f"fragment does not identify exact record: {url}"
    assert "record_key=graph-proof%3A1" in url, f"record key not bound: {url}"
    assert page.locator("a[data-detail-link]").count() == 0, "collection cards leaked onto detail route"
    assert page.locator("#close-inspector").get_attribute("href"), "breadcrumb back link missing"
    assert not page.locator("#graph-context").is_visible(), "accepted canvas-first entry opened a generic inspector"
    body = page.locator("[data-graph-workspace]").inner_text()
    assert "viewing historical revision" in body, "historical revision state not shown"
    assert "Not submittable" in body or "Submittability" in body, "submittability explanation missing"
    assert page.locator("[data-reason-code]").count() >= 1, "machine-traceable reason codes missing"

    geometry = connected_geometry(page)
    assert geometry["ready"], "canvas missing nodes or edges"
    assert geometry["startAtNode"] and geometry["endAtNode"], f"edge endpoints disconnected: {geometry}"

    # Selection and viewport controls must not touch the network or authority state.
    seen = []
    page.on("request", lambda request: seen.append(request.url))
    node = page.locator('[data-graph-node="0"]')
    node.focus()
    page.keyboard.press("Enter")
    assert page.locator('[data-graph-node-panel="0"]').is_visible(), "keyboard selection failed"
    assert node.get_attribute("aria-pressed") == "true"
    assert page.locator("[data-graph-definition-panel]").is_hidden(), "node panel did not replace definition panel"
    page.keyboard.press("Escape")
    assert not page.locator("#graph-context").is_visible(), "escape did not close inspector"
    page.locator("[data-graph-definition]").first.click()
    assert page.locator("[data-graph-definition-panel]").is_visible(), "definition restore failed"
    for _ in range(30):
        page.locator('[data-graph-zoom="in"]').click()
    assert page.locator("[data-graph-scale]").inner_text() == "200%", "zoom upper bound failed"
    for _ in range(30):
        page.locator('[data-graph-zoom="out"]').click()
    assert page.locator("[data-graph-scale]").inner_text() == "30%", "zoom lower bound failed"
    page.locator("[data-graph-fit]").click()
    viewport = page.locator("[data-graph-viewport]")
    viewport.focus()
    page.keyboard.press("ArrowRight")
    page.wait_for_timeout(150)
    page.keyboard.press("ArrowLeft")
    page.wait_for_timeout(150)
    assert not seen, f"selection/viewport interaction issued requests: {seen}"

    page.screenshot(path=str(ARTIFACTS / "console-desktop-detail.png"), full_page=True)

    page.reload()
    page.wait_for_selector("[data-graph-workspace]")
    assert page.url.endswith("#/graphs/graph-proof:1"), f"refresh lost exact record fragment: {page.url}"
    assert "viewing historical revision" in page.locator("[data-graph-workspace]").inner_text(), "refresh lost record"
    assert page.locator("a[data-detail-link]").count() == 0, "collection leaked after refresh"

    page.go_back()
    page.wait_for_selector("a[data-detail-link]")
    assert "/console/graphs" in page.url and "record_key" not in page.url, f"back did not reach collection: {page.url}"
    print("PASS desktop: collection routing, exact historical fragment, refresh, back, "
          "connected geometry, keyboard selection, zero-mutation viewport controls")


def narrow(page):
    login(page)
    page.goto(BASE + "/console/graphs#/graphs/graph-proof:1")
    page.wait_for_selector("[data-graph-workspace]")
    assert page.url.endswith("#/graphs/graph-proof:1"), "fragment-only entry lost exact revision"
    assert page.locator("a[data-detail-link]").count() == 0, "collection leaked on fragment-only entry"
    assert "viewing historical revision" in page.locator("[data-graph-workspace]").inner_text()
    node = page.locator('[data-graph-node="0"]')
    node.tap()
    assert page.locator('[data-graph-node-panel="0"]').is_visible(), "touch selection failed"
    canvas = page.locator(".gw-canvas").bounding_box()
    inspector = page.locator("#graph-context").bounding_box()
    assert canvas and inspector, "missing geometry"
    assert inspector["y"] >= canvas["y"] + canvas["height"] - 1, \
        f"inspector not stacked below canvas: canvas={canvas} inspector={inspector}"
    page.screenshot(path=str(ARTIFACTS / "console-narrow-detail.png"), full_page=True)
    print("PASS narrow: fragment-only entry, touch selection, stacked inspector below breakpoint")


def no_script(page):
    page.goto(BASE + "/console/agents")
    page.fill("#password", PASSWORD)
    page.click("#session-form button[type=submit]")
    page.wait_for_url("**/console/agents**")
    page.goto(DETAIL)
    textual = page.locator(".graph-textual")
    assert textual.is_visible(), "textual equivalent missing without scripts"
    textual.locator("> summary").click()
    textual.locator("details > summary").first.click()
    assert textual.get_by_text("Exact Agent revision").first.is_visible(), "node details hidden without scripts"
    assert textual.locator(".graph-edge-text").count() >= 1, "declared edges hidden without scripts"
    assert page.locator("[data-graph-scale]").inner_text() == "100%", "script state changed without scripts"
    page.screenshot(path=str(ARTIFACTS / "console-noscript-detail.png"), full_page=True)
    print("PASS no-script: native login, textual nodes and declared edges reachable")


with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    for name in ("desktop", "narrow"):
        context = browser.new_context(
            viewport={"width": 1440 if name == "desktop" else 390, "height": 900 if name == "desktop" else 844},
            has_touch=name == "narrow",
        )
        page = context.new_page()
        page.on("console", lambda msg: violations.append(msg.text) if "Content Security Policy" in msg.text else None)
        page.on("pageerror", lambda exc: violations.append(str(exc)))
        (desktop if name == "desktop" else narrow)(page)
        context.close()
    context = browser.new_context(java_script_enabled=False)
    page = context.new_page()
    page.on("console", lambda msg: violations.append(msg.text) if "Content Security Policy" in msg.text else None)
    no_script(page)
    context.close()
    browser.close()

assert not violations, f"runtime errors/CSP violations: {violations}"
