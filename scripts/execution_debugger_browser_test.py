#!/usr/bin/env python3
"""Controlled no-provider interpreter -> durable cursor -> Serve -> browser proof.
No model output, Queue success or installed-provider qualification is claimed.
"""
import os
from playwright.sync_api import sync_playwright
from issue330_browser_assertions import baseline, parity, initial, reduced_motion

base = os.environ['AEGIS_CONSOLE_BASE']
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    context = browser.new_context(viewport={'width': 1440, 'height': 900})
    page = context.new_page()
    errors = []
    page.on('pageerror', lambda e: errors.append(str(e)))
    page.on('console', lambda m: errors.append(m.text) if 'Content Security Policy' in m.text else None)
    page.goto(base + '/console/agents')
    page.fill('#password', os.environ['AEGIS_CONSOLE_PASSWORD'])
    page.click('#session-form button[type=submit]')
    detail = base + '/console/queue?record_key=debug-queue#/queue/debug-queue'
    expected_geometry = baseline(context, base)
    if os.environ.get('AEGIS_DEBUGGER_GRAPH') != '1':
        initial(context, base, detail, expected_geometry)
    page.goto(detail)
    page.wait_for_function("() => document.querySelector('[data-debugger-status]')?.textContent.includes('fresh observations')")
    if os.environ.get('AEGIS_DEBUGGER_GRAPH') == '1':
        assert page.locator('[data-debugger-scope]').input_value() == ''
        assert page.locator('[data-queue-node]').count() == 1
        page.locator('[data-queue-node]').click()
        assert 'debug-execution' in page.locator('#queue-context').inner_text()
        page.get_by_text('Open exact Loop execution', exact=True).click()
        assert page.locator('[data-debugger-scope]').input_value() == 'debug-node'
        assert 'debug-run' in page.locator('[data-debugger-identity]').inner_text()
    page.wait_for_function("() => document.querySelector('.queue-node.current strong')?.textContent === 'implement'")
    assert page.locator('[data-queue-node]').count() > 5
    assert 'r1' in page.locator('[data-debugger-scope]').inner_text()
    assert 'secret-canary' not in page.content()
    assert 'debug-execution' in page.locator('[data-debugger-identity]').inner_text()
    parity(page, expected_geometry)
    node = page.locator('[data-queue-node]').filter(has=page.locator('strong', has_text='implement')).first
    node.focus()
    page.keyboard.press('Enter')
    assert page.locator('#queue-context').is_visible()
    page.locator('[data-debugger-follow]').click()
    page.locator('[data-debugger-replay]').fill('2')
    paused = page.locator('[data-debugger-history]').inner_text()
    # Actual authenticated Serve reload while the interpreter is still blocked.
    page.reload()
    page.wait_for_function("() => document.querySelector('[data-debugger-status]')?.textContent.includes('fresh observations')")
    assert 'Historical replay' in page.locator('[data-debugger-status]').inner_text()
    assert page.locator('[data-debugger-replay]').input_value() == '2'
    assert page.locator('[data-debugger-history]').inner_text() == paused
    assert node.get_attribute('aria-pressed') == 'true'
    assert page.locator('#queue-context').is_visible()
    assert page.locator('[data-queue-node-panel]:visible').count() == 1
    assert page.locator('#queue-context [data-debugger-node-history]').count() == page.locator('[data-debugger-node-history]').count()
    parity(page, expected_geometry)
    page.locator('[data-queue-zoom=in]').click()
    before_zoom = page.locator('[data-queue-scale]').inner_text()
    print('RELEASE_CONTROLLED_EXECUTOR', flush=True)
    page.wait_for_timeout(3500)
    assert page.locator('[data-debugger-history]').inner_text() == paused
    assert 'Historical replay' in page.locator('[data-debugger-status]').inner_text()
    page.locator('[data-debugger-current]').click()
    page.wait_for_function("() => document.querySelector('[data-debugger-history]')?.textContent.includes('interpreter_terminal')")
    history = page.locator('[data-debugger-history]').inner_text()
    assert 'verification: failed' in history and 'implementation pass 1' in history
    assert 'verification: passed' in history and 'implementation pass 2' in history
    assert 'Queue attempt 1' in history and 'Queue attempt 2' not in history
    assert 'Queue claimed' in page.locator('[data-debugger-status]').inner_text()
    assert 'no evidence-backed terminal disposition' in page.locator('[data-debugger-status]').inner_text()
    assert page.locator('[data-queue-scale]').inner_text() == before_zoom
    assert node.get_attribute('aria-pressed') == 'true'
    if os.environ.get('AEGIS_DEBUGGER_TERMINAL') == '1':
        page.locator('[data-debugger-follow]').click()
        paused_terminal = page.locator('[data-debugger-history]').inner_text()
        print('RECORD_CONTROLLED_DISPOSITION', flush=True)
        success = os.environ.get('AEGIS_DEBUGGER_SUCCESS') == '1'
        reason = 'evidence_satisfied' if success else 'implementation_failed'
        state = 'succeeded' if success else 'failed'
        page.wait_for_function("reason => document.querySelector('[data-queue-outcome]')?.textContent.includes(reason)", arg=reason)
        outcome = page.locator('[data-queue-outcome]').inner_text()
        assert 'Current execution outcome · ' + state in outcome
        assert ('A more detailed cause was not recorded' in outcome) == (not success)
        assert 'verified acceptance' not in outcome
        assert 'Verification · not independently established' in outcome
        assert page.locator('[data-current-queue-state]').text_content().strip() == state
        assert 'Historical replay' in page.locator('[data-debugger-status]').inner_text()
        assert page.locator('[data-debugger-history]').inner_text() == paused_terminal
        assert page.locator('[data-queue-scale]').inner_text() == before_zoom
        assert node.get_attribute('aria-pressed') == 'true'
        page.locator('[data-debugger-current]').click()
        history = page.locator('[data-debugger-history]').inner_text()
        assert 'debug-disposition' in history
    # Disconnect real transport; no DOM updates are fabricated.
    parity(page, expected_geometry)
    context.set_offline(True)
    page.wait_for_function("() => document.querySelector('[data-debugger-status]')?.textContent.includes('stale / disconnected')")
    assert page.locator('[data-queue-workspace]').get_attribute('data-debugger-fresh') == 'false'
    context.set_offline(False)
    page.wait_for_function("() => document.querySelector('[data-debugger-status]')?.textContent.includes('fresh observations')")
    assert page.locator('[data-debugger-history]').inner_text() == history
    page.reload()
    page.wait_for_function("() => document.querySelector('[data-debugger-history]')?.textContent.includes('interpreter_terminal')")
    assert page.locator('[data-debugger-history]').inner_text() == history
    page.set_viewport_size({'width':390,'height':844})
    page.emulate_media(reduced_motion='reduce')
    parity(page, expected_geometry)
    reduced_motion(page)
    page.locator('[data-debugger-replay]').fill('2')
    assert 'Historical replay' in page.locator('[data-debugger-status]').inner_text()
    overflow = page.evaluate('''() => [...document.querySelectorAll('body *')].filter(e => {const b=e.getBoundingClientRect(); return b.right>innerWidth && getComputedStyle(e).position!=='absolute' && !e.closest('.queue-stage')}).map(e=>[e.tagName,e.className,e.getBoundingClientRect().width]).slice(0,20)''')
    sizes = page.evaluate('''() => ({width:innerWidth,doc:document.documentElement.scrollWidth,boxes:[...document.querySelectorAll('body *')].filter(e=>e.scrollWidth>e.clientWidth+2 && !e.closest('.queue-viewport')).map(e=>[e.tagName,e.className,e.getBoundingClientRect().right,e.scrollWidth,e.clientWidth]).slice(0,25)})''')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), f'horizontal page overflow at 390px: {overflow} {sizes}'
    page.locator('[data-debugger-current]').click()
    assert 'verification: failed' in page.locator('[data-debugger-history]').inner_text()
    # Auth boundary: remove the actual cookie, then the next owning-service
    # request must lock and clear the protected execution workspace.
    context.clear_cookies()
    page.wait_for_function("() => document.querySelector('[data-queue-workspace]')?.textContent.includes('Session expired')")
    assert page.locator('[data-queue-node]').count() == 0
    assert 'debug-execution' not in page.locator('[data-queue-workspace]').inner_text()
    assert page.locator('[data-queue-workspace]').get_attribute('data-debugger-initial') is None
    assert page.locator('[data-run-result-content], [data-queue-outcome]').count() == 0
    assert not errors, errors
    print('PASS controlled durable live start, exact pinned Loop, retry history, pause/current/replay, reconnect, keyboard, zoom/selection, 390px, auth loss, CSP', flush=True)
    browser.close()
