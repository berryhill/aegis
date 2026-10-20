#!/usr/bin/env python3
"""Actual candidate serve/CLI/browser/restart against private seeded history.
The historical interpreter/claim is controlled; no live provider is claimed.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.request
from playwright.sync_api import sync_playwright

binary, root = Path(sys.argv[1]), Path(sys.argv[2])
config = root / 'owner.json'
cfg = json.loads(config.read_text())
base = cfg['api']['console']['origin']
expected = json.loads((root / 'expected-debugger.json').read_text())
identity = hashlib.sha256(binary.read_bytes()).hexdigest()
assert not expected['direct'] and expected['nodes'][0]['execution_id'] == 'debug-execution'
assert expected['nodes'][0]['loop']['revision'] == 1
# The seed uses the production Console wrapper. Require real, complete pinned
# presentation data, then compare it together with every durable app field below.
assert set(expected['layouts']) == {node['node_id'] for node in expected['nodes']}
assert len(expected['layouts']['debug-node']['Nodes']) == 9
assert len(expected['layouts']['debug-node']['EdgeGeometry']) == 10
assert all(edge['Label'] for edge in expected['layouts']['debug-node']['EdgeGeometry'])
assert expected['graph_layout']['Nodes']
home = root / 'home'
home.mkdir(mode=0o700)
env = {'PATH': os.environ['PATH'], 'HOME': str(Path.home()), 'GOMAXPROCS': '2', 'GOMEMLIMIT': '512MiB'}
# The source-build provenance guard requires the authenticated operator home;
# all mutable product/runtime paths remain explicitly private in owner.json.
args = [str(binary), '--config', str(config)]
readbacks = []
errors = []
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    context = browser.new_context()
    page = context.new_page()
    page.on('pageerror', lambda e: errors.append(str(e)))
    for generation in range(2):
        with (root / f'serve-{generation}.log').open('wb') as log:
            daemon = subprocess.Popen(args + ['serve'], cwd=root, env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 15
                while True:
                    if daemon.poll() is not None:
                        raise AssertionError(f'candidate serve exited {daemon.returncode}; see {log.name}')
                    try:
                        with urllib.request.urlopen(base + '/console/agents', timeout=1):
                            break
                    except Exception:
                        if time.monotonic() >= deadline:
                            raise AssertionError('candidate serve readiness timeout')
                        time.sleep(.1)
                page.goto(base + '/console/agents')
                if page.locator('#password').count():
                    page.fill('#password', os.environ['AEGIS_CONSOLE_PASSWORD'])
                    page.click('#session-form button[type=submit]')
                page.goto(base + '/console/queue?record_key=debug-queue#/queue/debug-queue')
                page.wait_for_function("document.querySelector('[data-debugger-status]')?.textContent.includes('fresh observations')")
                if generation == 0:
                    assert page.locator('[data-debugger-scope]').input_value() == ''
                    page.locator('[data-queue-node]').click()
                    page.get_by_text('Open exact Loop execution', exact=True).click()
                    page.locator('[data-queue-node]').filter(has=page.locator('strong', has_text='implement')).first.click()
                    page.locator('[data-debugger-replay]').fill('2')
                assert page.locator('[data-debugger-scope]').input_value() == 'debug-node'
                assert page.locator('[data-debugger-replay]').input_value() == '2'
                assert page.locator('[data-queue-node][aria-pressed=true] strong').inner_text() == 'implement'
                assert 'debug-run' in page.locator('[data-debugger-identity]').inner_text()
                assert 'debug-execution' in page.locator('[data-debugger-identity]').inner_text()
                page.reload()
                page.wait_for_function("document.querySelector('[data-debugger-status]')?.textContent.includes('fresh observations')")
                assert page.locator('[data-debugger-replay]').input_value() == '2'
                actual = page.evaluate("async () => (await fetch(document.querySelector('[data-queue-workspace]').dataset.debuggerUrl)).json()")
                # observed_at is request freshness, intentionally excluded from the
                # server's durable-history fingerprint. Preserve every other field,
                # including watermark, version, lineage, facts and dispositions.
                (root / f'debugger-{generation}.json').write_text(json.dumps(actual, indent=2))
                assert actual['observed_at'] and actual['observed_at'] != expected['observed_at']
                durable_actual = {k: v for k, v in actual.items() if k != 'observed_at'}
                durable_expected = {k: v for k, v in expected.items() if k != 'observed_at'}
                assert durable_actual == durable_expected, 'restart/read-only inspection changed exact durable history'
                cli = subprocess.run(args + ['--target', base, 'queue', 'show', 'debug-queue'], cwd=root, env=env, capture_output=True, timeout=15)
                assert cli.returncode == 0, cli.stderr.decode()
                readbacks.append(json.loads(cli.stdout))
                (root / f'debugger-{generation}.json').write_text(json.dumps(actual, indent=2))
                assert 'no evidence-backed terminal disposition' in page.locator('[data-debugger-status]').inner_text()
            finally:
                daemon.terminate()
                try:
                    status = daemon.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    daemon.kill()
                    daemon.wait()
                    raise AssertionError('candidate did not terminate cleanly')
                assert status == 0, f'candidate shutdown exit {status}'
    assert readbacks[0] == readbacks[1], 'CLI restart reconstruction differs'
    assert len(readbacks[1]['attempts']) == 1
    assert not errors, errors
    browser.close()
assert hashlib.sha256(binary.read_bytes()).hexdigest() == identity
(root / 'acceptance.json').write_text(json.dumps({'binary_sha256': identity, 'generations': 2, 'exact_debugger_reconstruction': True, 'cli_unchanged': True, 'provider': 'controlled-none', 'live_provider_acceptance': False}, indent=2))
print('PASS actual candidate serve twice, public CLI exact readback, authenticated Graph drilldown, reload/replay/selection, identical durable reconstruction; no execution requests')
