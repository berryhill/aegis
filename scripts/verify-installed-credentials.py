#!/usr/bin/env python3
"""Generated-material-only Credentials proof; no Hermes execution or discovery.

Contract: an existing candidate serves an isolated authenticated console; every
record mutation uses real review/execute routes. No rendered fixture grants
identity. This is not runtime qualification or release/visual approval.
"""
import argparse
import atexit
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import socket
import subprocess
import time
import traceback
import urllib.request

from playwright.sync_api import expect, sync_playwright

DESIGN_SHA = '634e23fafafb5cd032a86ac80dbc3809d8e5bd9939cda80c16584beb4d4d2a9b'
DESIGN_BYTES = 755716


class ProofError(Exception):
    """Only fixed, material-free diagnostics may enter this exception."""


def require(condition, message):
    if not condition:
        raise ProofError(message)


def private_json(path, value):
    with path.open('x', encoding='utf-8') as stream:
        os.chmod(path, 0o600)
        json.dump(value, stream)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', type=Path)
    parser.add_argument('workspace', type=Path)
    parser.add_argument('design', type=Path)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parent.parent
    binary = args.binary.resolve(strict=True)
    root = args.workspace.absolute()
    require(root.is_relative_to(repo) and root.parent.resolve(strict=True).is_relative_to(repo) and not root.exists() and not root.is_symlink(), 'fresh repository-local workspace required')
    require(not args.binary.is_symlink() and binary.is_file() and os.access(binary, os.X_OK), 'regular executable required')
    design = args.design.resolve(strict=True)
    data = design.read_bytes()
    require(len(data) == DESIGN_BYTES and hashlib.sha256(data).hexdigest() == DESIGN_SHA, 'accepted design identity mismatch')
    root.mkdir(mode=0o700)
    root = root.resolve()
    def cleanup_material():
        for name in ('auth-input.json', 'api.token', 'state/credentials/authority.kek'):
            (root / name).unlink(missing_ok=True)
    atexit.register(cleanup_material)
    home = root / 'home'
    home.mkdir(mode=0o700)
    state = root / 'state'
    state.mkdir(mode=0o700)
    (state / 'persistence').mkdir(mode=0o700)
    env = {k: os.environ[k] for k in ('PATH', 'LANG', 'LC_ALL') if k in os.environ}
    env['HOME'] = str(home)
    # Initializers establish only fresh authority/password/custody. All credential
    # records are created through the extracted binary's authenticated HTTP API.
    password = secrets.token_urlsafe(32)
    auth_input = root / 'auth-input.json'
    private_json(auth_input, {'principal_id': 'credentials-proof', 'password': password})
    for helper, arguments in [
        ('demo-authority-init', [state / 'persistence/authority-v1']),
        ('demo-principal-auth-init', [auth_input, state]),
        ('demo-credential-custody-init', [state / 'credentials']),
    ]:
        result = subprocess.run(['go', 'run', './scripts/' + helper, *map(str, arguments)], cwd=repo, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=120)
        require(result.returncode == 0, helper + ' failed (diagnostics withheld)')
    auth_input.unlink()
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 0))
        port = probe.getsockname()[1]
    origin = f'http://127.0.0.1:{port}'
    socket_dir = Path(os.environ.get('AEGIS_PROOF_SOCKET_DIR', str(repo))).resolve(strict=True)
    socket_path = socket_dir / f'.credentials-proof-{port}.sock'
    require(len(str(socket_path).encode()) < 104, 'use existing durable short AEGIS_PROOF_SOCKET_DIR')
    token = root / 'api.token'
    with token.open('x') as stream:
        os.chmod(token, 0o600)
        stream.write(secrets.token_urlsafe(48) + '\n')
    user = pwd.getpwuid(os.getuid())
    cfg = {
        'state_dir': str(state), 'runtime_default': 'hermes',
        'principal': {'id': 'credentials-proof', 'name': 'Credentials Proof', 'uid': str(os.getuid()), 'user': user.pw_name, 'auth_ttl': '15m'},
        'audit': {'checkpoint_dir': str(root / 'checkpoints')},
        'api': {'listen': f'127.0.0.1:{port}', 'unix_socket': str(socket_path), 'token_file': str(token), 'read_timeout': '5s', 'write_timeout': '5s', 'shutdown_timeout': '2s', 'max_body_bytes': 1048576, 'console': {'origin': origin, 'session_ttl': '15m', 'max_page_size': 100}},
        'credentials': {'references': {}, 'provider_auth': {}, 'authority': {'database': str(state / 'credentials/authority.db'), 'deployment_id': 'credentials-proof', 'custody': 'host-file', 'kek_file': str(state / 'credentials/authority.kek')}},
    }
    config = root / 'aegis.json'
    private_json(config, cfg)
    report = {'failures': [], 'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'design_sha256': DESIGN_SHA, 'design_bytes': DESIGN_BYTES, 'runtime_qualification': False, 'candidate_acceptance': False, 'widths': []}
    log = (root / 'server.log').open('x')
    os.chmod(root / 'server.log', 0o600)
    server = subprocess.Popen([str(binary), '--config', str(config), 'serve'], cwd=home, env=env, stdout=log, stderr=log)
    material = [secrets.token_urlsafe(32) for _ in range(35)]
    try:
        deadline = time.monotonic() + 20
        while True:
            require(server.poll() is None, 'candidate serve exited; inspect private server log')
            try:
                urllib.request.urlopen(origin + '/console', timeout=1).close()
                break
            except OSError:
                require(time.monotonic() < deadline, 'candidate readiness timeout')
                time.sleep(.1)
        with sync_playwright() as p:
            browser = p.chromium.launch(executable_path='/usr/bin/google-chrome', args=['--no-sandbox'])
            context = browser.new_context(viewport={'width': 1440, 'height': 900})
            def local_route(route):
                if not route.request.url.startswith(origin + '/'):
                    route.abort()
                    return
                time.sleep(.23)
                route.continue_()
            context.route('**/*', local_route)
            page = context.new_page()
            def post(path, values, wanted, origin_header=origin):
                time.sleep(.23)  # production source-rate limit, not a bypass
                response = context.request.post(origin + path, form=values, headers={'Origin': origin_header})
                body = response.text()
                require(response.status == wanted, f'HTTP proof expected {wanted}, got {response.status}')
                require(all(value not in body for value in material), 'generated material disclosed')
                return body
            # Denial before login, then authenticate the configured principal.
            post('/console/credentials/operation/review', {'csrf': secrets.token_hex(32), 'operation': 'create', 'reference': 'denied-proof', 'kind': 'api-token', 'value': material[0]}, 401)
            page.goto(origin + '/console')
            page.locator('input[name=password]').fill(password)
            page.locator('button[type=submit]').click()
            expect(page.locator('input[name=password]')).to_have_count(0)
            response = context.request.get(origin + '/console/api/state')
            require(response.status == 200, 'authenticated state unavailable')
            csrf = response.json()['csrf']
            def review(values):
                body = post('/console/credentials/operation/review', {'csrf': csrf, **values}, 200)
                match = re.search(r'name="receipt" value="([a-f0-9]{64})"', body)
                require(match is not None, 'review receipt missing')
                assert match is not None
                return match.group(1)
            def execute(receipt, wanted=200):
                return post('/console/credentials/operation/execute', {'csrf': csrf, 'receipt': receipt}, wanted)
            create = {'operation': 'create', 'reference': 'proof-provider-00', 'kind': 'api-token', 'value': material[0]}
            post('/console/credentials/operation/review', create, 400)
            post('/console/credentials/operation/review', {'csrf': csrf, **create}, 403, 'https://invalid.example')
            receipt = review(create)
            page.goto(origin + '/console/credentials?q=proof-provider')
            require(page.locator('#surface-list tbody tr').count() == 0, 'review mutated inventory')
            execute(receipt)
            execute(receipt, 403)
            for index in range(1, 30):
                execute(review({**create, 'reference': f'proof-provider-{index:02d}', 'value': material[index]}))
            storage = context.storage_state()
            collection = origin + '/console/credentials?q=proof-provider&status=active&limit=100#/credentials'
            page.goto(collection)
            links = page.locator('#surface-list tbody a[data-record-link]')
            if links.count() == 0:
                links = page.locator('#surface-list tbody a')
            require(links.count() >= 25, 'populated inventory missing')
            key_value = links.nth(20).get_attribute('id')
            require(key_value is not None, 'record link identity missing')
            assert key_value is not None
            key = key_value.removeprefix('record-')
            for _ in range(25):
                execute(review({'operation': 'rotate', 'record_id': key, 'value': material[30]}))
            # Independent generated record proves revoke without removing the
            # selected active record from the restoration journey.
            other_key = links.first.get_attribute('id')
            require(other_key is not None, 'revoke record identity missing')
            assert other_key is not None
            execute(review({'operation': 'revoke', 'record_id': other_key.removeprefix('record-'), 'version': '1', 'reason': 'generated-proof'}))
            for width in [1440, 900, 390]:
                page.set_viewport_size({'width': width, 'height': 900})
                page.goto(collection)
                link = page.locator('#record-' + key)
                inventory = page.locator('#credential-inventory')
                size = inventory.evaluate('e=>{const s=getComputedStyle(e);return e.clientWidth-parseFloat(s.paddingLeft)-parseFloat(s.paddingRight)}')
                headers = ['Reference'] + (['Kind'] if size > 420 else []) + ['Status'] + (['Version'] if size > 540 and width > 760 else [])
                expect(page.locator('#surface-list').get_by_role('columnheader')).to_have_text(headers)
                expect(page.locator('#surface-list tbody tr').first.locator('th:visible,td:visible')).to_have_count(len(headers))
                # Authenticated header controls must remain visible and usable;
                # clipping the document is not an overflow repair.
                logout = page.locator('#logout')
                expect(logout).to_be_visible()
                require(logout.evaluate('e=>{const r=e.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth}'), 'authenticated sign-out control exceeds viewport')
                require(page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'collection exceeds viewport')
                link.scroll_into_view_if_needed()
                before = inventory.evaluate('e=>e.scrollTop')
                link.click()
                expect(page.locator('.credential-detail')).to_be_visible()
                require('#/credentials/' in page.url, 'fragment route missing')
                detail = page.locator('.credential-detail')
                if width > 900:
                    expect(inventory).to_be_visible()
                    a, b = inventory.bounding_box(), detail.bounding_box()
                    require(a is not None and b is not None, 'pane geometry unavailable')
                    assert a is not None and b is not None
                    require(inventory.evaluate('e=>e.scrollHeight>e.clientHeight'), 'inventory does not scroll')
                    require(detail.evaluate('e=>e.scrollHeight>e.clientHeight'), 'version history does not scroll')
                    old_scroll = inventory.evaluate('e=>e.scrollTop')
                    detail.evaluate('e=>e.scrollTop=200')
                    require(inventory.evaluate('e=>e.scrollTop') == old_scroll, 'pane scrolling is coupled')
                    require(abs(a['width'] / (a['width'] + b['width']) - .42) < .02, 'desktop proportions differ')
                else:
                    expect(inventory).not_to_be_visible()
                page.screenshot(path=str(root / f'credentials-{width}.png'))
                private_json(root / f'geometry-{width}.json', page.evaluate('''() => ({width:innerWidth, scrollWidth:document.documentElement.scrollWidth, overflow:[...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().right>innerWidth).map(e=>({tag:e.tagName, classes:e.className, width:e.getBoundingClientRect().width, right:e.getBoundingClientRect().right}))})'''))
                if not page.evaluate('document.documentElement.scrollWidth <= innerWidth'):
                    report['failures'].append(f'horizontal overflow at width {width}')
                page.go_back()
                expect(page.locator('#record-' + key)).to_be_focused()
                require(abs(inventory.evaluate('e=>e.scrollTop') - before) < 2, 'scroll restoration failed')
                require(page.locator('[name=q]').input_value() == 'proof-provider', 'filter restoration failed')
                page.go_forward()
                page.locator('#close-inspector').click()
                expect(page.locator('#record-' + key)).to_be_focused()
                with page.expect_response(lambda response: 'record_key=missing' in response.url) as denied:
                    page.evaluate("location.hash='#/credentials/missing'")
                require(denied.value.status == 400, 'unknown record did not fail closed')
                expect(page.locator('.credential-detail')).to_have_count(0)
                report['widths'].append({'width': width, 'authenticated_navigation': True})
            for mode in ['no-script', 'denied-storage']:
                fallback = browser.new_context(storage_state=storage, viewport={'width': 390, 'height': 900}, java_script_enabled=mode != 'no-script')
                fallback.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
                if mode == 'denied-storage':
                    fallback.add_init_script("Storage.prototype.getItem=Storage.prototype.setItem=function(){throw Error('denied')}")
                native = fallback.new_page()
                native.goto(collection)
                native.locator('#record-' + key).click()
                expect(native.locator('.credential-detail')).to_be_visible()
                native.locator('#close-inspector').click()
                expect(native.locator('#credential-inventory')).to_be_visible()
                fallback.close()
            # Offline artifact inspection blocks all network requests. Screenshots
            # are evidence for independent visual review, not automatic approval.
            reference = browser.new_context()
            reference.route('**/*', lambda route: route.abort() if route.request.url.startswith(('http:', 'https:')) else route.continue_())
            reference_page = reference.new_page()
            for width in [1440, 900, 390]:
                reference_page.set_viewport_size({'width': width, 'height': 900})
                reference_page.goto(design.as_uri() + '#/credentials')
                reference_page.screenshot(path=str(root / f'accepted-{width}.png'))
            reference.close()
            context.close()
            browser.close()
        report['generated_material_operations'] = 'create allowed; unauthenticated, missing-CSRF, cross-origin and replay denied'
        report['native_fallback'] = ['no-script', 'denied-storage']
        private_json(root / 'result.json', report)
        print(json.dumps(report, sort_keys=True))
        require(not report['failures'], 'installed Credentials proof has recorded failures')
    finally:
        server.terminate()
        try:
            server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait(timeout=5)
        log.close()
        # Never retain plaintext password input, transport token or generated KEK.
        token.unlink(missing_ok=True)
        (state / 'credentials/authority.kek').unlink(missing_ok=True)
        socket_path.unlink(missing_ok=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Playwright exceptions can contain request bodies; never print them.
        print(json.dumps({'status': 'failed', 'error_type': type(error).__name__, 'harness_lines': [frame.lineno for frame in traceback.extract_tb(error.__traceback__) if frame.filename == __file__], 'details': str(error) if isinstance(error, ProofError) else 'withheld to protect generated authentication material'}))
        raise SystemExit(1)
