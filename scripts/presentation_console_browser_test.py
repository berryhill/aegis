#!/usr/bin/env python3
"""Real disposable-browser Archive journeys; never operates an owner deployment."""
import json, os, time
from pathlib import Path
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect

root=Path(os.environ['AEGIS_PRESENTATION_PROOF'])
base=os.environ['AEGIS_PRESENTATION_ORIGIN']
results=[]
clock=0

def stage(seconds):
    global clock
    clock=seconds
    (root/'stage.json').write_text(json.dumps(seconds))
    time.sleep(.12)

def record(case, **extra):
    results.append(dict(case=case,**extra))
    (root/'browser-results.json').write_text(json.dumps(results,indent=2))

with sync_playwright() as p:
    browser=p.chromium.launch(headless=True, executable_path='/usr/bin/google-chrome')
    context=browser.new_context()
    context.route('**/*',lambda route:(time.sleep(.3),route.continue_()))
    page=context.new_page()
    errors=[]
    page.on('pageerror',lambda error:errors.append(str(error)))

    def login():
        page.goto(base+'/console/reauthenticate')
        page.fill('#password','api-principal-password')
        page.click('#session-form button[type=submit]')
        page.wait_for_url('**/console/agents**')

    def row(kind, archived=False):
        if kind=='loop':
            page.goto(base+'/console/loops'+('?availability=archived' if archived else ''))
            return page.locator('#surface-list > li').filter(has=page.locator('a[data-record-key="presentation-loop:2"]'))
        page.goto(base+'/console/queue?record_key=presentation-item')
        return page.locator('body')

    def click_action(kind, archived=False):
        target=row(kind,archived)
        label=('Restore' if kind=='loop' else 'Unarchive') if archived else 'Archive'
        button=target.get_by_role('button',name=label,exact=True)
        expect(button).to_be_visible()
        with page.expect_response(lambda r:r.request.method=='POST' and ('availability' in r.url or '/archive' in r.url or '/unarchive' in r.url)) as response:
            button.click()
        status=response.value.status
        page.wait_for_load_state('domcontentloaded')
        return status

    def available(kind, archived=False):
        target=row(kind,archived)
        label=('Restore' if kind=='loop' else 'Unarchive') if archived else 'Archive'
        expect(target.get_by_role('button',name=label,exact=True)).to_be_visible()

    def pending(kind):
        login()
        stage(clock+7201)
        assert click_action(kind)==303
        expect(page.locator('#presentation-reauth')).to_be_visible()
        assert urlparse(page.url).path.startswith('/console/presentation/')
        return page.url

    def authenticate_pending():
        page.fill('#password','api-principal-password')
        page.get_by_role('button',name='Sign in and review',exact=True).click()
        expect(page.get_by_role('button',name='Confirm retained action',exact=True)).to_be_visible()

    def form_data(form):
        return form.evaluate('(f)=>Object.fromEntries(new FormData(f))')

    def rejected_post(action,data,status):
        response=context.request.post(action,form=data,headers={'Origin':base,'Accept':'text/html'},max_redirects=0)
        assert response.status==303,(response.status,response.text())
        destination=response.headers['location']
        assert destination.startswith('/console/presentation-error?')
        recovery=context.request.get(base+destination)
        assert recovery.status==status,(recovery.status,recovery.text())
        assert 'Return to records' in recovery.text()

    login()
    stage(3600)
    for kind in ('loop','queue'):
        status=click_action(kind)
        if os.environ.get('AEGIS_PRESENTATION_BASELINE')=='1':
            assert status==403,(kind,status)
            record(kind+'-signed-in-post-baseline',status=status)
            continue
        assert status==303,(kind,status,page.locator('body').inner_text())
        available(kind,True)
        assert click_action(kind,True)==303
        available(kind,False)
        record(kind+'-valid-owner-beyond-generic-principal-window',status=status)
    if os.environ.get('AEGIS_PRESENTATION_BASELINE')!='1':
        for kind in ('loop','queue'):
            pending_url=pending(kind)
            # Cancellation is not authentication, admission or a presentation effect.
            page.get_by_role('button',name='Cancel retained action',exact=True).click()
            expect(page.get_by_role('heading',name='Retained action cancelled')).to_be_visible()
            available(kind,False)
            record(kind+'-reauth-cancel-no-effect')

            pending_url=pending(kind)
            page.fill('#password','wrong-fixture-password')
            page.get_by_role('button',name='Sign in and review',exact=True).click()
            expect(page.locator('#presentation-reauth')).to_be_visible()
            assert 'Nothing executed' in page.locator('body').inner_text()
            authenticate_pending()
            confirm=page.get_by_role('button',name='Confirm retained action',exact=True).locator('..')
            saved=form_data(confirm)
            action=confirm.get_attribute('action')
            # Reauthentication itself must not apply the retained mutation.
            observation=context.request.get(base+('/console/loops' if kind=='loop' else '/console/queue?record_key=presentation-item'))
            assert '>Archive</button>' in observation.text()
            page.get_by_role('button',name='Confirm retained action',exact=True).click()
            page.wait_for_load_state('domcontentloaded')
            available(kind,True)
            rejected_post(base+action,saved,403)
            available(kind,True)
            assert click_action(kind,True)==303
            record(kind+'-reauth-review-confirm-no-silent-or-replayed-effect')

            # A concurrent successful mutation makes the exact retained CAS stale.
            pending_url=pending(kind)
            authenticate_pending()
            saved=form_data(page.get_by_role('button',name='Confirm retained action',exact=True).locator('..'))
            action=page.get_by_role('button',name='Confirm retained action',exact=True).locator('..').get_attribute('action')
            assert click_action(kind)==303
            available(kind,True)
            rejected_post(base+action,saved,409)
            available(kind,True)
            assert click_action(kind,True)==303
            record(kind+'-retained-CAS-conflict-no-second-effect')

            login()
            target=row(kind)
            form=target.get_by_role('button',name='Archive',exact=True).locator('..')
            saved=form_data(form)
            action=form.get_attribute('action')
            rejected_post(base+action,{**saved,'csrf':'stale-fixture-csrf'},403)
            available(kind,False)
            record(kind+'-stale-CSRF-safe-recovery')

        login()
        own_loop=row('loop')
        loop_form=own_loop.get_by_role('button',name='Archive',exact=True).locator('..')
        rejected_post(base+loop_form.get_attribute('action'),{**form_data(loop_form),'agent_id':'presentation-foreign'},400)
        available('loop',False)
        record('browser-cannot-substitute-loop-owning-Agent')
        own=row('queue')
        own_form=own.get_by_role('button',name='Archive',exact=True).locator('..')
        foreign_data=form_data(own_form)
        foreign_data['digest']=json.loads((root/'foreign-fixture.json').read_text())['foreign_queue_digest']
        rejected_post(base+'/console/queue/foreign-presentation-item/archive',foreign_data,403)
        record('foreign-queue-owner-scoped-post-denied')

        # A stale already-rendered form may recover after the identity deadline,
        # but it never authenticates or applies an effect until a fresh login.
        login()
        stale_page=row('loop')
        stage(clock+21601)
        stale_page.get_by_role('button',name='Archive',exact=True).click()
        expect(page.locator('#presentation-reauth')).to_be_visible()
        authenticate_pending()
        page.get_by_role('button',name='Confirm retained action',exact=True).click()
        available('loop',True)
        assert click_action('loop',True)==303
        record('expired-login-retained-form-fresh-login-explicit-confirmation')

        # Custody expiry cannot refresh login or replay the deferred operation.
        pending('loop')
        stage(clock+121)
        page.reload()
        assert 'Presentation action not completed' in page.locator('body').inner_text()
        available('loop',False)
        record('retained-review-expiry-no-effect')
        assert not errors,errors
        record('browser-page-errors-empty')
    context.close()
    browser.close()
print(json.dumps(results))
