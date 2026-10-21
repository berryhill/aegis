#!/usr/bin/env python3
"""Incompatible Console auth is explicit at render and denied on forced POST.
Real authenticated HTTP/Chrome; synthetic runtime predecessor, no live provider.
"""
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import console_browser_test as d

base, item = os.environ['AEGIS_CONSOLE_BASE'], os.environ['AEGIS_REQUEUE_ITEM']
root = Path(os.environ['AEGIS_REQUEUE_EVIDENCE'])
with tempfile.TemporaryDirectory(prefix='chrome-',dir=root) as profile:
    with (root/'chrome.log').open('wb') as log:
        chrome = subprocess.Popen(['/usr/bin/google-chrome','--headless=new','--incognito','--disable-gpu',
            '--no-first-run','--no-default-browser-check','--remote-debugging-port=0',
            '--remote-allow-origins=http://localhost','--user-data-dir='+profile,base+'/console'],
            stdout=subprocess.DEVNULL,stderr=log,env=d.chrome_environment(),start_new_session=True)
        b = None
        try:
            port = d.wait_for_active_port(Path(profile)/'DevToolsActivePort',time.monotonic()+20,chrome)
            b = d.DevTools(d.page_websocket(port,time.monotonic()+20,chrome))
            for domain in ('Page','Runtime','Network','Audits'): b.command(domain+'.enable')
            d.navigate(b,base+'/console')
            d.wait_for(b,"!!document.querySelector('#password')",'login')
            d.insert_text(b,'#password',os.environ['AEGIS_CONSOLE_PASSWORD'])
            d.click(b,'#session-form button[type=submit]')
            d.wait_for(b,"!!document.querySelector('#logout')",'authenticated')
            time.sleep(1)
            d.navigate(b,base+'/console/queue?record_key='+item)
            d.wait_for(b,"document.body.innerText.includes('Requeue unavailable: independent_setup_required') && document.body.innerText.includes('session_selection_zero_authorized_matches')",'explicit authentication eligibility')
            d.require(b.evaluate("document.querySelectorAll('form[action$=\"/requeue\"]').length")==0,'incompatible authentication offered Requeue')
            d.require(b.evaluate("document.querySelectorAll('a form, form form').length")==0,'nested form')
            time.sleep(1)
            # Adversarial direct POST must remain denied even without a control.
            # Reuse only the real archive form's public CSRF/digest/CAS fields;
            # no executor subject or approval is supplied or synthesized.
            b.evaluate("(() => { const f=document.querySelector('form[action$=\"/archive\"],form[action$=\"/unarchive\"]'); if(!f) throw Error('missing owned archive form'); const p=f.cloneNode(true); p.action=f.action.replace(/\\/(unarchive|archive)$/, '/requeue'); document.body.append(p); p.submit(); })()")
            d.wait_for(b,"document.title === 'Requeue admission' && document.body.innerText.includes('independent_setup_required') && document.body.innerText.includes('session_selection_zero_authorized_matches')",'fresh authority rejection')
            violations = [e for e in b.events if e.get('method')=='Audits.issueAdded' and e.get('params',{}).get('issue',{}).get('code')=='ContentSecurityPolicyIssue']
            (root/'csp-issues.json').write_text(json.dumps(violations, indent=2)+'\n')
            d.require(not violations,'CSP violation: '+json.dumps(violations))
            (root/'acceptance.json').write_text(json.dumps({'eligible_control':False,'real_click':False,'forced_browser_post':True,'explicit_render_gate':True,
                'reason':'independent_setup_required; session_selection_zero_authorized_matches',
                'csp':'pass','evidence_tier':'real authenticated HTTP/Chrome; synthetic-runtime predecessor; no provider execution'},indent=2)+'\n')
            print('PASS: explicit incompatible-auth render gate and exact forced browser POST denial')
        finally:
            if b: b.close()
            if chrome.poll() is None: os.killpg(chrome.pid,signal.SIGTERM)
            chrome.wait(timeout=15)
