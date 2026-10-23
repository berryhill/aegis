#!/usr/bin/env python3
"""Actual candidate, private durable history, authenticated Chrome mutations.
Synthetic historical Graph fixture; explicitly not live-provider execution.
"""
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlsplit
import console_browser_test as d

binary, root = map(Path, sys.argv[1:3])
base = json.loads((root / 'owner.json').read_text())['api']['console']['origin']
identity = hashlib.sha256(binary.read_bytes()).hexdigest()
item = 'browser-actions-item'
results = []


def start(log):
    proc = subprocess.Popen([str(binary), '--config', str(root / 'owner.json'), 'serve'],
        cwd=root, env={'PATH': os.environ['PATH'], 'HOME': str(Path.home()),
                      'GOMAXPROCS': '2', 'GOMEMLIMIT': '512MiB'}, stdout=log, stderr=log)
    target = urlsplit(base)
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        d.require(proc.poll() is None, 'candidate exited; see isolated serve log')
        try:
            with socket.create_connection((target.hostname, target.port), timeout=.2):
                return proc
        except OSError:
            time.sleep(.1)
    raise RuntimeError('candidate readiness timeout')


def nav(b, path):
    time.sleep(1)
    old = b.command('Page.getFrameTree')['frameTree']['frame']['loaderId']
    d.navigate(b, base + path)
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        loader = b.command('Page.getFrameTree')['frameTree']['frame']['loaderId']
        if loader != old and b.evaluate("document.readyState === 'complete'"):
            return
        time.sleep(.05)
    raise RuntimeError('document navigation timeout')


def click(b, selector, predicate):
    time.sleep(1)
    d.click(b, selector)
    d.wait_for(b, predicate, selector)


def selected(b):
    return b.evaluate("[...document.querySelectorAll('form[aria-label=\"Filter Queue history\"] select')].map(x=>x.value)")


def forms(b):
    return b.evaluate("[...document.querySelectorAll('form[action^=\"/console/queue/\"]')].map(x=>new URL(x.action).pathname)")


for generation in range(2):
    with (root / f'serve-{generation}.log').open('wb') as log:
        daemon = start(log)
        try:
            with tempfile.TemporaryDirectory(prefix='chrome-', dir=root) as profile:
                with (root / f'chrome-{generation}.log').open('wb') as diagnostics:
                    chrome = subprocess.Popen(['/usr/bin/google-chrome', '--headless=new', '--incognito',
                        '--disable-gpu', '--no-first-run', '--no-default-browser-check',
                        '--remote-debugging-port=0', '--remote-allow-origins=http://localhost',
                        '--user-data-dir='+profile, base+'/console'], stdout=subprocess.DEVNULL,
                        stderr=diagnostics, env=d.chrome_environment(), start_new_session=True)
                    b = None
                    try:
                        port = d.wait_for_active_port(Path(profile)/'DevToolsActivePort', time.monotonic()+20, chrome)
                        b = d.DevTools(d.page_websocket(port,time.monotonic()+20,chrome))
                        for domain in ('Page','Runtime','Network','Audits'):
                            b.command(domain+'.enable')
                        nav(b,'/console')
                        d.wait_for(b,"!!document.querySelector('#password')",'login')
                        d.insert_text(b,'#password',os.environ['AEGIS_CONSOLE_PASSWORD'])
                        click(b,'#session-form button[type=submit]',"!!document.querySelector('#logout')")
                        nav(b,'/console/queue')
                        d.require(selected(b)==(['succeeded','unarchived'] if generation==0 else ['failed','archived']), 'durable defaults differ: '+repr(selected(b)))
                        if generation == 0:
                            nav(b,'/console/queue?state=all&visibility=all')
                            d.require(b.evaluate("document.querySelectorAll('a form, a button, form form').length") == 0,'nested row controls')
                            failed_card = 'section[aria-labelledby="failed-executions"] li:has(#record-'+item+')'
                            d.require(b.evaluate("!!document.querySelector('"+failed_card+"')"),'FAILED item absent from Failed Executions')
                            d.require(b.evaluate("document.querySelector('"+failed_card+" a').dataset.state === 'failed'"),'card is not failed')
                            d.require('/console/queue/'+item+'/requeue' not in forms(b),'unsupported Graph requeue visible')
                            # Submit an explicitly unsupported requeue using real browser cookies,
                            # CSRF and same-origin fetch. No UI implies no supported positive path.
                            status = b.evaluate("(async()=>{const f=document.querySelector('form[action$=\"/archive\"]'); const r=await fetch('/console/queue/"+item+"/requeue',{method:'POST',body:new URLSearchParams(new FormData(f))}); return r.status})()")
                            d.require(status==403,'unsupported Requeue must deny exactly 403: '+str(status))
                            click(b,failed_card+' form[action$="/archive"] button',"document.readyState==='complete' && location.pathname==='/console/queue' && location.search==='' && location.hash==='' && !!document.querySelector('#queue-lifecycle')")
                            d.require(not b.evaluate("!!document.querySelector('"+failed_card+"')"),'archived item remains in Failed Executions')
                            d.require(b.evaluate("location.href") == base+'/console/queue','Archive opened detail or retained query/fragment')
                            nav(b,'/console/queue?state=all&visibility=unarchived')
                            d.require(not b.evaluate("!!document.querySelector('#record-"+item+"')"),'archived row still unarchived')
                            nav(b,'/console/queue?state=failed&visibility=archived')
                            d.require(b.evaluate("!!document.querySelector('#record-"+item+"')"),'archived row absent')
                            click(b,'form[action="/console/queue/preferences"] button',"document.readyState==='complete' && location.search==='' && !!document.querySelector('#queue-lifecycle')")
                            d.require(selected(b)==['failed','archived'],'saved default not read back')
                            d.refresh_registration_document(b)
                            d.require(selected(b)==['failed','archived'],'reload lost default')
                        else:
                            d.require(b.evaluate("!!document.querySelector('#record-"+item+"')"),'restart lost archive')
                            nav(b,'/console/queue?record_key='+item)
                            d.require(b.evaluate("document.querySelector('form[action$=\"/unarchive\"] input[name=expected_revision]').value")=='1','restart lost revision')
                            click(b,'form[action$="/unarchive"] button',"!!document.querySelector('form[action$=\"/archive\"]')")
                            d.require(b.evaluate("document.querySelector('form[action$=\"/archive\"] input[name=expected_revision]').value")=='2','unarchive revision')
                            d.require(b.evaluate("location.search") == '?record_key='+item and b.evaluate("location.hash") == '#/queue/'+item,'Unarchive no longer returns to detail')
                            nav(b,'/console/queue')
                            d.require(not b.evaluate("!!document.querySelector('section[aria-labelledby=\"queue-lifecycle\"] #record-"+item+"')"),'unarchived row still in saved archived lifecycle view')
                            d.require(b.evaluate("!!document.querySelector('section[aria-labelledby=\"failed-executions\"] #record-"+item+"')"),'unarchived FAILED item missing from independent Failed band')
                            nav(b,'/console/queue?state=all&visibility=unarchived')
                            d.require(b.evaluate("!!document.querySelector('#record-"+item+"')"),'unarchive row absent')
                        violations = [e for e in b.events if e.get('method')=='Audits.issueAdded' and e.get('params',{}).get('issue',{}).get('code')=='ContentSecurityPolicyIssue']
                        d.require(not violations,'CSP violation')
                        results.append({'generation':generation,'defaults':selected(b),'csp':'pass'})
                    finally:
                        if b: b.close()
                        if chrome.poll() is None: os.killpg(chrome.pid,signal.SIGTERM)
                        chrome.wait(timeout=15)
        finally:
            daemon.terminate()
            d.require(daemon.wait(timeout=20)==0,'candidate shutdown failed')

d.require(hashlib.sha256(binary.read_bytes()).hexdigest()==identity,'binary changed')
(root/'acceptance.json').write_text(json.dumps({'binary_sha256':identity,'generations':results,
    'archive_collection_url':base+'/console/queue', 'failed_card_archive':True,
    'failed_band_removed_on_archive':True, 'unarchive_detail_preserved':True,
    'archive_unarchive_cas':[1,2], 'saved_default_restart':True,
    'unsupported_graph_requeue_status':403,
    'evidence_tier':'real candidate authenticated Chrome and process restart; synthetic historical Graph; no provider execution'},indent=2)+'\n')
print('PASS: actual candidate authenticated Archive/save-default, new process + fresh login durable readback, Unarchive, unsupported Requeue denial and CSP')
