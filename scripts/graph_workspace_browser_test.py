#!/usr/bin/env python3
"""Opt-in component proof under production CSP; no authentication/authority claims."""
import sys
from pathlib import Path
from playwright.sync_api import sync_playwright

ARTIFACTS = Path(__file__).resolve().parents[1] / ".aegis-graph-proof"
ARTIFACTS.mkdir(parents=True, exist_ok=True)


def verify(page, width):
    page.wait_for_selector("[data-graph-node]")
    page.wait_for_function("document.querySelector('[data-graph-scale]')")
    assert not page.locator('#graph-context').is_visible(), 'entry must be canvas-first, not an open generic inspector'
    assert page.locator('.graph-workspace').evaluate("e => e.classList.contains('solo')")
    definition = page.locator('[data-graph-definition]').first
    assert definition.get_attribute('aria-expanded') == 'false'
    definition.click()
    assert page.locator('[data-graph-definition-panel]').is_visible()
    page.locator('[data-graph-close]').click()
    assert not page.locator('#graph-context').is_visible()
    page.evaluate("document.querySelector('[data-graph-viewport]').scrollLeft = 0; document.querySelector('[data-graph-viewport]').scrollTop = 0")
    connected = page.evaluate("""() => {
      const nodes = [...document.querySelectorAll('[data-graph-node]')];
      const edges = [...document.querySelectorAll('.graph-edge-path')];
      if (edges.length === 0 || nodes.length < 2) return {ready: false};
      const matrix = edges[0].getScreenCTM();
      if (!matrix) return {ready: false};
      const start = edges[0].getPointAtLength(0).matrixTransform(matrix);
      const end = edges[0].getPointAtLength(edges[0].getTotalLength()).matrixTransform(matrix);
      const startRect = nodes[0].getBoundingClientRect();
      const endRect = nodes[1].getBoundingClientRect();
      const stage = document.querySelector('[data-graph-stage]');
      const stageComputed = window.getComputedStyle(stage);
      const view = document.querySelector('[data-graph-viewport]');
      const stageRect = stage.getBoundingClientRect();
      const viewRect = view.getBoundingClientRect();
      const startAtRightOfA = Math.abs(start.x - (startRect.right)) < 3 && Math.abs(start.y - (startRect.top + 51)) < 3;
      const endAtLeftOfB = Math.abs(end.x - endRect.left) < 3 && Math.abs(end.y - (endRect.top + 51)) < 3;
      const withinViewport = start.x >= viewRect.left && end.x <= viewRect.right + viewRect.width;
      return {
        ready: true,
        startAtRightOfA, endAtLeftOfB, withinViewport,
        start: [start.x, start.y], end: [end.x, end.y],
        startRight: startRect.right, endLeft: endRect.left,
        startTop51: startRect.top + 51, endTop51: endRect.top + 51,
        viewLeft: viewRect.left, viewRight: viewRect.right,
        stageComputedWidth: stageComputed.width,
        stageComputedHeight: stageComputed.height,
        stageRect: [stageRect.left, stageRect.top, stageRect.width, stageRect.height],
        dataColumns: stage.dataset.columns,
        dataRows: stage.dataset.rows,
      };
    }""")
    page.screenshot(path=str(ARTIFACTS / f"graph-{width}.png"), full_page=True)
    assert connected["ready"], f"{width}: canvas missing nodes or edges"
    assert connected["startAtRightOfA"] and connected["endAtLeftOfB"] and connected["withinViewport"], f"{width}: edge endpoints do not connect to rendered nodes: {connected}"
    node = page.locator('[data-graph-node="0"]')
    node.focus()
    page.keyboard.press("Enter")
    assert page.locator('[data-graph-node-panel="0"]').is_visible()
    assert node.get_attribute("aria-pressed") == "true"
    page.keyboard.press("Escape")
    assert not page.locator("#graph-context").is_visible()
    node.tap()
    assert page.locator('[data-graph-node-panel="0"]').is_visible()
    canvas = page.locator(".gw-canvas").bounding_box() or {}
    inspector = page.locator("#graph-context").bounding_box() or {}
    assert canvas and inspector, f"missing geometry: {canvas} {inspector}"
    if width < 1080:
        assert inspector["y"] >= canvas["y"] + canvas["height"] - 1, f"inspector not stacked: {canvas} {inspector}"
    else:
        assert inspector["x"] >= canvas["x"] + canvas["width"] - 1, f"inspector not beside canvas: {canvas} {inspector}"
    for _ in range(25):
        page.locator('[data-graph-zoom="in"]').click()
    assert page.locator('[data-graph-scale]').inner_text() == "200%"
    for _ in range(25):
        page.locator('[data-graph-zoom="out"]').click()
    assert page.locator('[data-graph-scale]').inner_text() == "30%"
    page.locator('[data-graph-fit]').click()


with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    violations = []
    for width in (1440, 390):
        context = browser.new_context(viewport={"width": width, "height": 900}, has_touch=True)
        page = context.new_page()
        page.on("console", lambda msg: violations.append(msg.text) if "Content Security Policy" in msg.text else None)
        page.on("pageerror", lambda exc: violations.append(str(exc)))
        page.goto(sys.argv[1] + "/#/graphs/graph-proof:1")
        verify(page, width)
        context.close()
        print(f"PASS component {width}px: connected geometry, keyboard, touch selection, responsive inspector, bounded zoom, fit")
    context = browser.new_context(java_script_enabled=False)
    page = context.new_page()
    page.goto(sys.argv[1])
    page.locator(".graph-textual > summary").click()
    page.locator(".graph-textual details > summary").first.click()
    assert page.locator(".graph-textual").get_by_text("Exact Agent revision").first.is_visible()
    assert page.locator(".graph-textual .graph-edge-text").last.is_visible()
    context.close()
    print("PASS no-script textual node details and declared edges")
    assert not violations, f"runtime errors/CSP violations: {violations}"
    browser.close()
