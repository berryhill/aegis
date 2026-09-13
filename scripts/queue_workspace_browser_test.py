#!/usr/bin/env python3
"""Opt-in component proof under production CSP; no authentication/authority claims.

The Queue execution detail workspace reconstructs the exact pinned Graph/Loop
revision from the queue snapshot binding, projects authoritative runtime state
onto pinned definition nodes, and renders six supporting tabs (Inputs and
outputs, Timeline, Authority and runtime, Evidence, Admission, Submission
snapshot). Browser state cannot forge lifecycle, authority, evidence, or
outcome.
"""

import sys
from pathlib import Path
from playwright.sync_api import sync_playwright

ARTIFACTS = Path(__file__).resolve().parents[1] / ".aegis-queue-proof"
ARTIFACTS.mkdir(parents=True, exist_ok=True)


def verify(page, width):
    page.wait_for_selector("[data-queue-node]")
    page.wait_for_function("document.querySelector('[data-queue-scale]')")
    assert not page.locator('#queue-context').is_visible(), 'entry must be canvas-first, not an open generic inspector'
    assert page.locator('.queue-workspace').evaluate("e => e.classList.contains('solo')")
    definition = page.locator('[data-queue-definition]').first
    assert definition.get_attribute('aria-expanded') == 'false'
    definition.click()
    assert page.locator('[data-queue-definition-panel]').is_visible()
    page.locator('[data-queue-close]').click()
    assert not page.locator('#queue-context').is_visible()
    page.evaluate("document.querySelector('[data-queue-viewport]').scrollLeft = 0; document.querySelector('[data-queue-viewport]').scrollTop = 0")
    connected = page.evaluate("""() => {
      const nodes = [...document.querySelectorAll('[data-queue-node]')];
      const edges = [...document.querySelectorAll('.queue-edge-path')];
      if (edges.length === 0 || nodes.length < 2) return {ready: false};
      const matrix = edges[0].getScreenCTM();
      if (!matrix) return {ready: false};
      const start = edges[0].getPointAtLength(0).matrixTransform(matrix);
      const end = edges[0].getPointAtLength(edges[0].getTotalLength()).matrixTransform(matrix);
      const startRect = nodes[0].getBoundingClientRect();
      const endRect = nodes[1].getBoundingClientRect();
      const stage = document.querySelector('[data-queue-stage]');
      const stageComputed = window.getComputedStyle(stage);
      const view = document.querySelector('[data-queue-viewport]');
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
    page.screenshot(path=str(ARTIFACTS / f"queue-{width}.png"), full_page=True)
    assert connected["ready"], f"{width}: canvas missing nodes or edges"
    assert connected["startAtRightOfA"] and connected["endAtLeftOfB"] and connected["withinViewport"], f"{width}: edge endpoints do not connect to rendered nodes: {connected}"
    node = page.locator('[data-queue-node="0"]')
    node.focus()
    page.keyboard.press("Enter")
    assert page.locator('[data-queue-node-panel="0"]').is_visible()
    assert node.get_attribute("aria-pressed") == "true"
    page.keyboard.press("Escape")
    assert not page.locator("#queue-context").is_visible()
    node.tap()
    assert page.locator('[data-queue-node-panel="0"]').is_visible()
    canvas = page.locator(".qw-canvas").bounding_box() or {}
    inspector = page.locator("#queue-context").bounding_box() or {}
    assert canvas and inspector, f"missing geometry: {canvas} {inspector}"
    if width < 1080:
        assert inspector["y"] >= canvas["y"] + canvas["height"] - 1, f"inspector not stacked: {canvas} {inspector}"
    else:
        assert inspector["x"] >= canvas["x"] + canvas["width"] - 1, f"inspector not beside canvas: {canvas} {inspector}"
    for _ in range(25):
        page.locator('[data-queue-zoom="in"]').click()
    assert page.locator('[data-queue-scale]').inner_text() == "200%"
    for _ in range(25):
        page.locator('[data-queue-zoom="out"]').click()
    assert page.locator('[data-queue-scale]').inner_text() == "30%"
    page.locator('[data-queue-fit]').click()
    for label in ("inputs", "timeline", "authority", "evidence", "admission", "snapshot"):
        tab = page.locator(f'[data-queue-tab="{label}"]')
        tab.click()
        assert tab.get_attribute('aria-pressed') == 'true', f"{label} tab did not activate"
        panel = page.locator(f'[data-queue-tab-panel="{label}"]')
        assert not panel.is_hidden(), f"{label} panel did not show"
    # Security-negative: a passing receipt must not promote the failed
    # execution state to succeeded. We assert the lifecycle badge remains
    # failed even after a verifier receipt passes.
    lifecycle = page.locator('.lifecycle.state-large').first
    assert 'failed' in (lifecycle.inner_text() or '').lower(), f"lifecycle badge was promoted: {lifecycle.inner_text()}"


with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    violations = []
    for width in (1440, 390):
        context = browser.new_context(viewport={"width": width, "height": 900}, has_touch=True)
        page = context.new_page()
        page.on("console", lambda msg: violations.append(msg.text) if "Content Security Policy" in msg.text else None)
        page.on("pageerror", lambda exc: violations.append(str(exc)))
        page.goto(sys.argv[1] + "/#/queue/queue-proof:1")
        verify(page, width)
        context.close()
        print(f"PASS component {width}px: connected geometry, keyboard, touch selection, responsive inspector, bounded zoom, fit, six tabs, security-negative")
    context = browser.new_context(java_script_enabled=False)
    page = context.new_page()
    page.goto(sys.argv[1])
    page.locator(".queue-textual > summary").click()
    page.locator(".queue-textual details > summary").first.click()
    assert page.locator(".queue-textual").get_by_text("Authoritative failure").first.is_visible()
    assert page.locator(".queue-textual .queue-edges").last.is_visible()
    context.close()
    print("PASS no-script textual node details and declared transitions")
    assert not violations, f"runtime errors/CSP violations: {violations}"
    browser.close()