#!/usr/bin/env python3
"""Opt-in real frozen-candidate logical Loop discovery; never executes a Loop."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import time
from urllib.parse import urlencode
from playwright.sync_api import sync_playwright, expect


def main():
    root = Path(os.environ['AEGIS_LOOP_DETAIL_ROOT'])
    binary = Path(os.environ['AEGIS_LOOP_DETAIL_CANDIDATE']).resolve()
    repo = Path(__file__).resolve().parents[1]
    assert binary == repo / 'aegis'
    assert os.environ['AEGIS_ACCEPTANCE_PORT']
    identity = hashlib.sha256(binary.read_bytes()).hexdigest()
    config = json.loads((root / 'owner.json').read_text())
    base = config['api']['console']['origin']
    records = json.loads((root / 'fixtures.json').read_text())['records']
    revisions = [r for r in records if r['id'] == 'loop-detail']
    total = len({r['id'] for r in records})
    results, errors = [], []

    def result(case):
        results.append({'case': case, 'result': 'PASS'})
        (root / 'logical-browser-results.json').write_text(json.dumps({'binary_sha256': identity, 'results': results, 'errors': errors}, indent=2))

    def read(id, revision):
        connection = http.client.HTTPConnection('localhost')
        connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        connection.sock.connect(config['api']['unix_socket'])
        connection.request('GET', f'/v1/loops/{id}/{revision}', headers={'Authorization':'Bearer '+config['api']['token']})
        response = connection.getresponse()
        assert response.status == 200, response.status
        value = json.loads(response.read())
        connection.close()
        return value

    def stable(value):
        return {key:value[key] for key in ('revision','validations','provenance','lifecycle','lifecycle_history')}

    def post(context, form, status):
        time.sleep(.6)
        response = context.request.post(base + '/console/loops/availability?loop_id=loop-detail', form=form, headers={'Origin':base}, max_redirects=0)
        assert response.status == status, (response.status, status)

    with (root / 'logical-serve.log').open('wb') as log:
        daemon = subprocess.Popen([str(binary), '--config', str(root / 'owner.json'), 'serve'], cwd=repo, stdout=log, stderr=log)
        try:
            deadline = time.monotonic()+20
            while True:
                assert daemon.poll() is None, 'candidate exited'
                try:
                    with socket.create_connection(('127.0.0.1',int(os.environ['AEGIS_ACCEPTANCE_PORT'])),timeout=.2): break
                except OSError:
                    assert time.monotonic()<deadline
                    time.sleep(.1)
            with sync_playwright() as p:
                browser = p.chromium.launch(headless=True, executable_path='/usr/bin/google-chrome')
                context = browser.new_context()
                context.route('**/*', lambda route: (time.sleep(.3),route.continue_()))
                page = context.new_page()
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.on('console', lambda message: errors.append(message.text) if message.type=='error' and any(t in message.text.lower() for t in ('content security policy','refused to')) else None)
                page.goto(base+'/console/agents')
                page.fill('#password',os.environ['AEGIS_CONSOLE_PASSWORD'])
                page.click('#session-form button[type=submit]')
                page.wait_for_url('**/console/agents**')
                page.goto(base+'/console/loops')
                expect(page.locator('#surface-list > li')).to_have_count(total)
                row = page.locator('#surface-list > li').filter(has=page.locator('a[data-record-key="loop-detail:2"]'))
                expect(row).to_have_count(1)
                assert 'Newer authored Loop detail fixture' in row.inner_text()
                assert 'Latest published revision · r2' in row.inner_text() and 'Active revision · loop-detail r1 @' in row.inner_text()
                assert f'{total} authoritative logical Loops' in page.locator('body').inner_text()
                history = row.locator('.loop-revision-history')
                history.locator('summary').click()
                expect(history.locator('li a')).to_have_count(2)
                form = row.locator('form').evaluate('(f)=>Object.fromEntries(new FormData(f))')
                assert form['revision']=='2'
                before = {r['revision']:read(r['id'],r['revision']) for r in revisions}
                control = read('branch-loop',1)
                # Fresh no-cookie client, wrong CSRF, and stale publication-head fail closed.
                outsider = browser.new_context()
                post(outsider,form,401)
                outsider.close()
                post(context,{**form,'csrf':'wrong'},403)
                post(context,{**form,'revision':'1','digest':before[1]['revision']['digest']},409)
                assert {n:read('loop-detail',n) for n in before}==before
                result('unauthenticated-CSRF-and-stale-publication-head-denied-unchanged')
                for revision in (1,2):
                    page.goto(base+'/console/loops?'+urlencode({'record_key':f'loop-detail:{revision}'}))
                    expect(page.locator('[data-loop-workspace]')).to_be_visible()
                    expect(page.locator('[data-loop-node]')).to_have_count(len(revisions[revision-1]['steps']))
                    page.locator('[data-loop-definition]').first.click()
                    expect(page.locator('#loop-provenance')).to_be_visible()
                    assert before[revision]['revision']['digest'] in page.locator('body').inner_text()
                    assert page.locator('form[action^="/console/loops/availability"]').count()==0
                result('one-logical-entry-separate-second-loop-active-one-latest-two-both-exact-diagrams-provenance')
                page.goto(base+'/console/loops')
                row = page.locator('#surface-list > li').filter(has=page.locator('a[data-record-key="loop-detail:2"]'))
                assert 'ALL revisions' in row.inner_text() and 'does not stop execution' in row.inner_text()
                row.get_by_role('button',name='Archive',exact=True).click()
                expect(page.locator('#surface-list > li')).to_have_count(1)
                archived = page.locator('#surface-list > li')
                expect(archived.get_by_role('button',name='Restore',exact=True)).to_be_visible()
                assert archived.locator('.loop-revision-history li a').count()==2
                post(context,form,409)
                for revision in (1,2):
                    value = read('loop-detail',revision)
                    assert value['availability']['state']=='archived'
                    assert stable(value)==stable(before[revision])
                assert read('branch-loop',1)==control
                result('archive-all-revisions-once-selected-stable-ID-only-history-and-active-lifecycle-unchanged')
                for revision in (1,2):
                    page.goto(base+'/console/loops?'+urlencode({'record_key':f'loop-detail:{revision}'}))
                    expect(page.locator('[data-loop-workspace]')).to_be_visible()
                    page.locator('[data-loop-definition]').first.click()
                    expect(page.locator('#loop-provenance')).to_be_visible()
                    assert before[revision]['revision']['digest'] in page.locator('body').inner_text()
                page.goto(base+'/console/loops?availability=archived')
                restore = page.locator('#surface-list form').evaluate('(f)=>Object.fromEntries(new FormData(f))')
                page.get_by_role('button',name='Restore',exact=True).click()
                expect(page.locator('[data-loop-workspace]')).to_be_visible()
                post(context,restore,409)
                page.goto(base+'/console/loops')
                expect(page.locator('#surface-list > li')).to_have_count(total)
                for revision in (1,2):
                    value=read('loop-detail',revision)
                    assert value['availability']['state']=='published' and value['availability']['sequence']==2
                    assert stable(value)==stable(before[revision])
                assert read('branch-loop',1)==control
                result('archived-exact-links-preserved-restore-once-all-revisions-no-history-or-control-mutation')
                assert not errors, errors
                page.screenshot(path=str(root/'logical-collection.png'),full_page=True)
                result('browser-CSP-and-page-errors-empty')
                context.close()
                browser.close()
        finally:
            daemon.terminate()
            daemon.wait(timeout=20)
    assert hashlib.sha256(binary.read_bytes()).hexdigest()==identity
    result('candidate-identity-unchanged')
    print(json.dumps({'results':results,'binary_sha256':identity}))


if __name__=='__main__':
    main()
