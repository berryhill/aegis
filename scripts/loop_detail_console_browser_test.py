#!/usr/bin/env python3
"""Actual frozen Console definition acceptance; synthetic stores, never Run."""
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import time
from urllib.parse import quote, urlencode, urlsplit
from playwright.sync_api import sync_playwright, expect


def main():
    root = Path(os.environ['AEGIS_LOOP_DETAIL_ROOT'])
    binary = Path(os.environ['AEGIS_LOOP_DETAIL_CANDIDATE']).resolve()
    repo = Path(__file__).resolve().parents[1]
    assert binary == repo / 'aegis', 'candidate must stay at its real module root'
    identity = hashlib.sha256(binary.read_bytes()).hexdigest()
    config = root / 'owner.json'
    base = json.loads(config.read_text())['api']['console']['origin']
    fixtures = json.loads((root / 'fixtures.json').read_text())
    records = {(r['id'], r['revision']): r for r in fixtures['records']}
    results, errors, csp, mutations = [], [], [], []
    browser = None
    process = None

    def result(case, **fields):
        results.append({'case': case, 'result': 'PASS', **fields})
        save()

    def save():
        (root / 'browser-results.json').write_text(json.dumps({'binary_sha256': identity, 'results': results, 'errors': errors, 'csp': csp}, indent=2) + '\n')

    def attach(page):
        page.on('pageerror', lambda error: errors.append(str(error).replace(os.environ['AEGIS_CONSOLE_PASSWORD'], '[redacted-fixture-credential]')))
        page.on('console', lambda message: csp.append(message.text) if message.type == 'error' and any(term in message.text.lower() for term in ('content security policy', 'refused to', 'script-src', 'style-src')) else None)

    def route(request):
        assert request.request.url.startswith(base + '/'), 'unexpected off-origin browser request'
        time.sleep(.25)  # pace real assets below the unchanged source limiter
        request.continue_()

    def login(page):
        page.goto(base + '/console/agents')
        page.fill('#password', os.environ['AEGIS_CONSOLE_PASSWORD'])
        page.click('#session-form button[type=submit]')
        page.wait_for_url('**/console/agents**')

    def location(record):
        key = f"{record['id']}:{record['revision']}"
        return base + '/console/loops?' + urlencode({'record_key': key}) + '#/loops/' + quote(key, safe='')

    def open_record(page, record, width=1440):
        page.set_viewport_size({'width': width, 'height': 900 if width > 600 else 844})
        time.sleep(.5)
        target = location(record)
        # goto on an identical document with a fragment is a same-document
        # navigation (no HTTP response); reload to prove a fresh exact read.
        response = page.reload() if page.url == target else page.goto(target)
        assert response and response.status == 200, f'exact authenticated definition read failed: {response.status if response else "no HTTP response"} {page.url}'
        expect(page.locator('#inspector-title')).to_have_text(record['name'])
        expect(page.locator('[data-loop-node]')).to_have_count(len(record['steps']))
        expect(page.locator('.loop-edge-path')).to_have_count(len(record['transitions']))
        assert page.locator('#surface-list').count() == 0, 'collection displaced dedicated detail'
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'page overflow'

    def capture(page, label):
        # Auth observation briefly hides stale detail during a fresh read.
        if page.locator('[data-loop-workspace]').count():
            expect(page.locator('[data-loop-workspace]')).to_be_visible()
        page.wait_for_timeout(400)
        if page.locator('[data-loop-workspace]').count():
            geometry = page.evaluate('''() => {
                const box = selector => {const r=document.querySelector(selector).getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height};};
                return {header:box('.loop-record-head'),title:box('#inspector-title'),viewport:box('[data-loop-viewport]'),tabs:box('.loop-tab-nav'),panel:box('[data-loop-tab-panel]:not([hidden])')};
            }''')
            (root / (label + '.geometry.json')).write_text(json.dumps(geometry, indent=2))
            assert geometry['viewport']['height'] >= 200, 'narrow canvas collapsed below usable height'
            assert geometry['title']['width'] >= min(300, geometry['header']['width'] - 50), 'identity column collapsed'
            assert geometry['panel']['y'] >= geometry['tabs']['y'] + geometry['tabs']['height'] - 2, 'tabs stretched beside panel'
        page.screenshot(path=str(root / (label + '.png')), full_page=True)
        visible = page.locator('body').inner_text().replace(str(root), '[isolated-fixture-root]')
        (root / (label + '.txt')).write_text(visible)

    try:
        with (root / 'serve.log').open('wb') as log:
            process = subprocess.Popen([str(binary), '--config', str(config), 'serve'], cwd=repo, stdout=log, stderr=log)
            deadline = time.monotonic() + 15
            while True:
                assert process.poll() is None, 'Serve exited; private log retained'
                try:
                    with socket.create_connection(('127.0.0.1', int(os.environ['AEGIS_ACCEPTANCE_PORT'])), timeout=.2):
                        break
                except OSError:
                    assert time.monotonic() < deadline, 'Serve readiness timeout'
                    time.sleep(.1)
            with sync_playwright() as p:
                browser = p.chromium.launch(headless=True, executable_path='/usr/bin/google-chrome')
                context = browser.new_context(has_touch=True)
                context.route('**/*', route)
                page = context.new_page()
                attach(page)
                page.on('request', lambda request: mutations.append((request.method, urlsplit(request.url).path)) if request.method not in ('GET', 'HEAD') else None)
                login(page)
                mutations.clear()

                # Logical bookmarks are pinned by the authenticated server, not
                # a mutable browser guess. Refresh retains the exact chosen head.
                page.goto(base + '/console/loops#/loops/loop-detail')
                expect(page.locator('#inspector-title')).to_have_text(records[('loop-detail', 2)]['name'])
                assert 'record_key=loop-detail%3A2' in page.url
                pinned = page.url
                page.reload()
                assert page.url == pinned
                result('logical-bookmark-server-pin-and-refresh')

                for width in (1440, 390):
                    record = records[('loop-detail', 1)]
                    open_record(page, record, width)
                    assert 'r2 · viewing historical revision' in page.locator('[data-loop-workspace]').inner_text()
                    assert page.locator('form[action="/console/loops/lifecycle-preview"]').count() == 0
                    run = page.locator('form[action^="/console/loops/run?"]')
                    assert run.count() == 1 and 'loop_id=loop-detail' in run.get_attribute('action')
                    assert run.locator('input[name=revision]').input_value() == '1'
                    assert run.locator('input[name=digest]').input_value() == record['digest']
                    key = run.locator('input[name=idempotency_key]').input_value()
                    assert key
                    capture(page, f'default-{width}')
                    node = page.locator('[data-loop-node="0"]')
                    node.click()
                    selected = page.locator('[data-loop-node-host] [data-loop-node-panel="0"]')
                    expect(selected).to_be_visible()
                    assert 'Stored executable contract' in selected.inner_text()
                    assert 'Executable operation' in selected.inner_text()
                    assert page.locator('[data-loop-node-panel="0"]').count() == 1, 'step contract duplicated'
                    assert page.locator('#loop-context-title').evaluate('(n)=>n===document.activeElement')
                    capture(page, f'selected-{width}')
                    page.keyboard.press('Escape')
                    expect(page.locator('#loop-context')).to_be_hidden()
                    assert node.evaluate('(n)=>n===document.activeElement'), 'focus not returned'
                    expect(page.locator('[data-loop-textual-node="0"] [data-loop-node-panel="0"]')).to_have_count(1)
                    tabs = page.locator('[data-loop-tab]')
                    expect(tabs.nth(0)).to_have_text('Required inputs')
                    tabs.nth(0).focus()
                    page.keyboard.press('ArrowRight')
                    expect(page.locator('[data-loop-tab-panel=evidence]')).to_be_visible()
                    expect(page.locator('[data-loop-tab-panel=inputs]')).to_be_hidden()
                    capture(page, f'evidence-{width}')
                    page.keyboard.press('Home')
                    expect(page.locator('[data-loop-tab-panel=inputs]')).to_be_visible()
                    assert page.locator('[data-loop-tab-panel=inputs] .loop-sealed-contract').count() == 0
                    page.reload()
                    expect(page.locator('#inspector-title')).to_have_text(record['name'])
                    assert page.locator('form[action^="/console/loops/run?"] input[name=idempotency_key]').input_value() == key
                    result(f'historical-node-tabs-keyboard-refresh-{width}')

                # Actual branching ports/evidence/mappings and current CSRF.
                branch = records[('branch-loop', 1)]
                open_record(page, branch)
                table = page.locator('table[aria-label="Declared Loop inputs"]')
                expect(table).to_be_visible()
                assert table.locator('tbody tr').count() == 2
                choose = branch['steps'].index('choose')
                page.locator(f'[data-loop-node="{choose}"]').click()
                panel = page.locator('[data-loop-node-host]')
                assert 'choose-left' in panel.inner_text() and 'value' in panel.inner_text()
                page.locator('[data-loop-definition]').first.click()
                forms = page.locator('form[action="/console/loops/lifecycle-preview"]')
                assert forms.count() == 2
                logout_csrf = page.locator('form[action="/console/logout"] input[name=csrf]').input_value()
                assert all(forms.nth(i).locator('input[name=csrf]').input_value() == logout_csrf for i in range(forms.count())), 'lifecycle CSRF misbound'
                capture(page, 'branch-current-definition')
                result('branch-input-mappings-evidence-current-csrf')

                for identifier, expected_components in (('self-loop', 1), ('multi-loop', 2)):
                    open_record(page, records[(identifier, 1)])
                    details = page.locator('.cycle-band > details')
                    details.locator('summary').click()
                    assert page.locator('.cycle-band section[aria-label="Cyclic component"]').count() == expected_components
                    assert 'Not declared' in details.inner_text()
                    assert '999 iterations' not in details.inner_text()
                    capture(page, identifier)
                    result(identifier, components=expected_components)

                namespace = records[('team/loop:stage', 1)]
                page.goto(base + '/console/loops#/loops/' + quote(namespace['id'], safe=''))
                expect(page.locator('#inspector-title')).to_have_text(namespace['name'])
                result('namespace-bookmark')

                maximum = records[('maximum-loop', 1)]
                open_record(page, maximum)
                page.locator('[data-loop-fit]').click()
                probe = page.evaluate('''() => {const v=document.querySelector('[data-loop-viewport]').getBoundingClientRect();const nodes=[...document.querySelectorAll('[data-loop-node]')].map(n=>n.getBoundingClientRect());return {count:nodes.length,left:Math.min(...nodes.map(n=>n.left)),right:Math.max(...nodes.map(n=>n.right)),top:Math.min(...nodes.map(n=>n.top)),bottom:Math.max(...nodes.map(n=>n.bottom)),viewport:{left:v.left,right:v.right,top:v.top,bottom:v.bottom},scale:document.querySelector('[data-loop-scale]').textContent};}''')
                capture(page, 'maximum-fit')
                (root / 'maximum-fit.json').write_text(json.dumps(probe, indent=2))
                assert probe['left'] >= probe['viewport']['left']-2 and probe['right'] <= probe['viewport']['right']+2 and probe['top'] >= probe['viewport']['top']-2 and probe['bottom'] <= probe['viewport']['bottom']+2, 'Fit does not contain the complete admitted maximum definition'
                page.locator('[data-loop-node="0"]').click()
                expect(page.locator('[data-loop-node-host] [data-loop-node-panel="0"]')).to_be_visible()
                result('maximum-definition-fit-and-selection', steps=len(maximum['steps']), transitions=len(maximum['transitions']))

                assert fixtures['invalid_record_absent'] and any(i['code'] == 'entry.terminal' for i in fixtures['invalid_terminal_only'])
                for key in ('terminal-only:1', 'missing-loop:1'):
                    page.goto(base + '/console/loops?' + urlencode({'record_key': key}))
                    assert page.locator('[data-loop-workspace]').count() == 0
                    capture(page, key.replace(':', '-'))
                result('invalid-terminal-only-publication-denial-and-missing-record')

                open_record(page, branch)
                page.goto(location(branch).split('#')[0] + '&stanza=FORGED-STANZA&authority=FORGED-AUTHORITY&subject=FORGED-SUBJECT')
                expect(page.locator('#inspector-title')).to_have_text(branch['name'])
                assert all(value not in page.locator('body').inner_text() for value in ('FORGED-STANZA', 'FORGED-AUTHORITY', 'FORGED-SUBJECT', os.environ['AEGIS_CONSOLE_PASSWORD'], 'transport-secret'))
                assert not mutations, 'definition presentation submitted an action'
                result('no-authority-or-secret-from-browser-fields')

                # Browser-enforced no-script path, real native login and details.
                nojs = browser.new_context(java_script_enabled=False, viewport={'width':390,'height':844})
                nojs.route('**/*', route)
                textual = nojs.new_page()
                attach(textual)
                login(textual)
                textual.goto(location(branch))
                textual.locator('.loop-textual > summary').click()
                textual.locator('[data-loop-textual-node="0"] > summary').click()
                assert textual.locator('.loop-textual [data-loop-node-panel]').count() == len(branch['steps'])
                assert textual.locator('[data-loop-tab-panel]:visible').count() == 2
                capture(textual, 'no-script-390')
                nojs.close()
                result('no-script-native-textual-equivalent')

                page.set_viewport_size({'width':390,'height':844})
                page.emulate_media(reduced_motion='reduce')
                open_record(page, records[('self-loop',1)],390)
                page.locator('[data-loop-node="0"]').tap()
                expect(page.locator('#loop-context')).to_be_visible()
                result('native-touch-reduced-motion')
                # Revoke this session in a second real tab. The already-open
                # detail must clear, without navigation or a new render there.
                revoker = context.new_page()
                attach(revoker)
                revoker.goto(location(branch))
                revoker.locator('form[action="/console/logout"] button').click()
                revoker.close()
                page.bring_to_front()
                page.evaluate('window.dispatchEvent(new Event("focus"))')
                expect(page.locator('[data-loop-auth-cleared="authentication-required"]')).to_be_visible()
                assert page.locator('[data-loop-workspace]').count() == 0 and page.locator('form[action^="/console/loops/run"]').count() == 0
                capture(page, 'auth-loss-already-open')
                result('actual-cross-tab-logout-clears-already-open-detail')
                page.goto(location(branch))
                expect(page.locator('#password')).to_be_visible()
                assert page.locator('[data-loop-workspace]').count() == 0
                result('auth-loss-native-navigation-denied')
                assert not errors and not csp, 'browser exception or CSP violation'
                result('CSP-and-browser-errors')
                context.close()
                browser.close()
                browser = None
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == identity, 'candidate changed during browser acceptance'
        save()
        print('PASS real frozen Console: exact/history/namespace reads, selection/tabs, mappings/cycles/maximum Fit, keyboard/touch, native fallback, CSRF binding, auth loss and CSP; no Run or provider execution')
    except Exception as error:
        results.append({'case':'failure','result':'FAIL','reason':str(error).replace(os.environ['AEGIS_CONSOLE_PASSWORD'],'[redacted-fixture-credential]')})
        save()
        raise
    finally:
        # sync_playwright's context owns browser cleanup even on assertion
        # failure; calling close after it exited masks the original failure.
        if process is not None:
            process.terminate()
            process.wait(timeout=15)


if __name__ == '__main__':
    main()
