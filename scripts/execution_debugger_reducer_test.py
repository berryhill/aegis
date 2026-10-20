#!/usr/bin/env python3
"""Synthetic transport regression of production scripts; not live Graph execution."""
import copy
import json
from pathlib import Path
from playwright.sync_api import sync_playwright

root = Path(__file__).resolve().parents[1]
fact = dict(sequence=1, ordinal=0, digest='checkpoint1', kind='step_start', step_id='implement', visit=1, retry=1, **{'pass': 1}, at='2026-10-20T12:00:00Z', outcome='running')
state = dict(queue_id='q', graph_run_id='parent', snapshot_digest='snap', direct=False, state='claimed', terminal=False, version=1, watermark='w', notice='Synthetic fixture', dependencies=[], nodes=[dict(node_id='child', loop=dict(id='loop', revision=1, digest='pinned'), execution_id='exec', steps=[dict(id='implement', kind='executable')], edges=[], attempts=[dict(id='attempt', number=1, facts=[fact])])])
# Explicit single-cell server projection for this synthetic transport fixture;
# canonical multi-node geometry is verified through the real Go/Serve fixture.
state['graph_layout'] = dict(Columns=1, Rows=1, Nodes=[dict(GridColumn=1, GridRow=1)], EdgeGeometry=[])
state['layouts'] = {'child': copy.deepcopy(state['graph_layout'])}
html = '''<main data-queue-workspace data-debugger-url="/poll"><div class="qw-body" id="unrelated"></div><div class="queue-workspace"><section><div data-queue-viewport tabindex="0"><div data-queue-stage></div></div></section><aside id="queue-context"><h3 id="queue-context-title" tabindex="-1"></h3><div class="qw-body"><div data-queue-definition-panel>Authoritative Queue evidence</div><section data-queue-node-panel="0" hidden><ol data-debugger-node-history></ol></section></div><button data-queue-close>Close</button></aside></div><select data-debugger-scope></select><input data-debugger-replay type="range" min="0"><button data-debugger-follow></button><button data-debugger-current>Current</button><p data-debugger-status></p><p data-debugger-notice></p><p data-debugger-identity></p><ol data-debugger-history></ol><span data-queue-scale></span></main>'''
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()
    errors = []
    page.on('pageerror', lambda e: errors.append(str(e)))
    def route(r):
        path = r.request.url.split('http://fixture')[1]
        if path.split('?')[0] == '/poll':
            r.fulfill(content_type='application/json', body=json.dumps(state))
        elif path.endswith('.js'):
            r.fulfill(content_type='application/javascript', body=(root / 'web/console' / path[1:]).read_text())
        else:
            # Real navigation/reload reconstructs from current server snapshot.
            initial = json.dumps(state).replace('<', '\\u003c')
            r.fulfill(content_type='text/html', body=html + '<script>document.querySelector("main").dataset.debuggerInitial=JSON.stringify(' + initial + ')</script><script src="/queue_workspace.js"></script><script src="/execution_debugger.js"></script>')
    page.route('http://fixture/**', route)
    page.goto('http://fixture/')
    page.wait_for_function("document.querySelector('[data-debugger-status]').textContent.includes('fresh observations')")
    # The installed Graph → Queue journey requires collapsed definition
    # controls after the debugger replaces the server-rendered node panels.
    assert page.locator('#queue-context').is_hidden()
    assert page.locator('[data-queue-definition]').first.get_attribute('aria-expanded') == 'false'
    page.locator('[data-queue-node]').focus()
    page.keyboard.press('Enter')
    assert page.locator('#queue-context').is_visible()
    page.get_by_text('Open exact Loop execution', exact=True).click()
    assert page.locator('[data-debugger-scope]').input_value() == 'child'
    assert page.locator('[data-queue-node-panel]:visible').count() == 1
    assert page.locator('[data-queue-node][aria-pressed=true]').count() == 1
    # The regenerated Loop panel must also reach authoritative Queue evidence,
    # focus its heading, and close back to the connected selected node.
    page.locator('[data-queue-node-panel] [data-queue-definition]').click()
    assert page.locator('[data-queue-definition-panel]').is_visible()
    assert page.locator('[data-queue-definition]').first.get_attribute('aria-expanded') == 'true'
    assert page.locator('#queue-context-title').evaluate('(e)=>e===document.activeElement')
    page.keyboard.press('Escape')
    assert page.locator('#queue-context').is_hidden()
    assert page.locator('[data-queue-node]').evaluate('(e)=>e===document.activeElement')
    page.keyboard.press('Enter')
    assert page.locator('#queue-context [data-debugger-node-history]').count() == 1
    assert page.locator('#unrelated [data-debugger-node-history]').count() == 0
    assert page.locator('[data-debugger-node-history]').count() == 1
    assert 'parent' in page.locator('[data-debugger-identity]').inner_text()
    page.locator('[data-debugger-follow]').click()
    page.locator('[data-debugger-replay]').fill('0')
    paused = page.locator('[data-debugger-history]').inner_text()
    # Server advances while presentation is paused; reload must not add facts.
    failed = dict(fact, sequence=2, digest='checkpoint2', kind='step_finish', outcome='failed', failure_code='doer_proposal_json_invalid')
    state['nodes'][0]['attempts'][0]['facts'].append(failed)
    state['version'] = 2
    page.reload()
    page.wait_for_function("document.querySelector('[data-debugger-status]').textContent.includes('fresh observations')")
    assert page.locator('[data-debugger-scope]').input_value() == 'child'
    assert page.locator('[data-debugger-replay]').input_value() == '0'
    assert 'Historical replay' in page.locator('[data-debugger-status]').inner_text()
    assert page.locator('[data-debugger-history]').inner_text() == paused
    assert page.locator('[data-queue-node-panel]:visible').count() == 1
    assert page.locator('[data-queue-node][aria-pressed=true]').count() == 1
    assert page.locator('.queue-node.current').count() == 0
    page.locator('[data-debugger-current]').click()
    assert 'reason doer_proposal_json_invalid' in page.locator('#queue-context').inner_text()
    # Escape closes context and restores meaningful focus to selected child.
    page.locator('#queue-context-title').focus()
    page.keyboard.press('Escape')
    assert page.locator('#queue-context').is_hidden()
    assert page.locator('[data-queue-node]').evaluate('(e)=>e===document.activeElement')
    page.keyboard.press('Enter')
    # New pending start followed by authoritative Queue cancellation, no disposition.
    pending = dict(fact, sequence=3, digest='checkpoint3', visit=2, **{'pass': 2})
    state['nodes'][0]['attempts'][0]['facts'].append(pending)
    state['version'] = 3
    page.wait_for_function("document.querySelector('.queue-node.current') !== null")
    state['state'] = 'cancelled'
    state['version'] = 4
    page.wait_for_function("document.querySelector('[data-debugger-status]').textContent.includes('Queue cancelled')")
    assert page.locator('.queue-node.current, .observed-now').count() == 0
    assert page.locator('[data-queue-workspace]').get_attribute('data-debugger-fresh') == 'false'
    assert 'finish unobserved' in page.locator('.queue-node-state').inner_text()
    assert 'no evidence-backed terminal disposition' in page.locator('[data-debugger-status]').inner_text()
    page.locator('[data-debugger-replay]').fill('1')
    assert page.locator('.queue-node-state').inner_text() == 'running'
    assert page.locator('.queue-node.current').count() == 0
    page.locator('[data-debugger-current]').click()
    # Delayed snapshots remain rejected, not installed as new truth.
    state['version'] = 0
    state['state'] = 'claimed'
    page.wait_for_function("document.querySelector('[data-debugger-status]').textContent.includes('stale / disconnected')")
    assert 'Queue cancelled' in page.locator('[data-debugger-status]').inner_text()
    assert page.locator('.queue-node.current').count() == 0
    # Exact run key prevents another snapshot borrowing selection or replay.
    state['queue_id'] = 'other-q'
    page.reload()
    assert page.locator('[data-debugger-scope]').input_value() == ''
    assert page.locator('[data-queue-node][aria-pressed=true]').count() == 0
    assert not errors, errors
    browser.close()
print('PASS synthetic reducer: contextual drill/keyboard, exact-run reload/selection/replay, per-step reason, cancellation/unobserved finish, delayed rejection, run isolation')
