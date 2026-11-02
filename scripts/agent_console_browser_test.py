#!/usr/bin/env python3
"""Real frozen Aegis Serve + password-authenticated Chrome, isolated fixtures.
Run only via TestAgentConsoleFrozenBrowser under acceptance_port.py custody.
This does not qualify a provider or invent a local-os browser login/stanza grant.
"""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import tempfile
import time
import console_browser_test as driver
from execution_name_browser_test import navigate_ready


def main():
    binary = Path(os.environ['AEGIS_AGENT_CANDIDATE']).resolve()
    root = Path(os.environ['AEGIS_AGENT_ROOT']).resolve()
    origin = 'http://127.0.0.1:' + os.environ['AEGIS_ACCEPTANCE_PORT']
    identity = hashlib.sha256(binary.read_bytes()).hexdigest()
    results = []
    log = (root / 'serve.log').open('w')
    process = subprocess.Popen([str(binary), '--config', str(root / 'owner.json'), 'serve'], stdout=log, stderr=log)
    try:
        deadline = time.monotonic() + 20
        while True:
            driver.require(process.poll() is None, 'candidate exited; see private serve.log')
            try:
                with socket.create_connection(('127.0.0.1', int(os.environ['AEGIS_ACCEPTANCE_PORT'])), .2):
                    break
            except OSError:
                driver.require(time.monotonic() < deadline, 'candidate readiness timeout')
                time.sleep(.1)
        with tempfile.TemporaryDirectory(prefix='chrome-', dir=root) as profile:
            chrome = subprocess.Popen(['/usr/bin/google-chrome', '--headless=new', '--incognito', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--remote-debugging-port=0', '--remote-allow-origins=http://localhost', '--user-data-dir='+profile, 'about:blank'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
            browser = None
            try:
                port = driver.wait_for_active_port(Path(profile)/'DevToolsActivePort', time.monotonic()+15, chrome)
                browser = driver.DevTools(driver.page_websocket(port,time.monotonic()+8,chrome))
                for domain in ('Page','Runtime','Network','Audits','Accessibility'):
                    browser.command(domain+'.enable')
                navigate_ready(browser,origin+'/console/agents')
                driver.wait_for(browser,"!!document.querySelector('#password')",'password login')
                driver.insert_text(browser,'#password',os.environ['AEGIS_CONSOLE_PASSWORD'])
                driver.click(browser,'#session-form button[type=submit]')
                driver.wait_for(browser,"!!document.querySelector('#logout')",'authenticated Console')

                def visit(path):
                    time.sleep(1)  # real production source bucket, including assets
                    navigate_ready(browser, origin+path)

                def capture(label, width):
                    text = browser.evaluate('document.body.innerText')
                    text = re.sub(r'password:[A-Za-z0-9_-]+','password:[redacted-subject]',text)
                    (root/(label+'.txt')).write_text(text)
                    browser.evaluate('scrollTo(0,0)')
                    size = browser.command('Page.getLayoutMetrics')['cssContentSize']
                    shot = browser.command('Page.captureScreenshot', {'format':'png','captureBeyondViewport':True,'clip':{'x':0,'y':0,'width':width,'height':size['height'],'scale':1}})
                    (root/(label+'.png')).write_bytes(base64.b64decode(shot['data']))
                    geometry = browser.evaluate('''(() => {let d=document.querySelector('#agent-inline-detail'),r=document.querySelector('#surface-list');return {width:innerWidth,documentWidth:document.documentElement.scrollWidth,inline:!!d,roster:!!r,detailTop:d?.getBoundingClientRect().top,rosterBottom:r?.getBoundingClientRect().bottom,authority:document.querySelector('[data-authority-state]')?.dataset.authorityState};})()''')
                    driver.require(geometry['documentWidth'] <= width+1, 'horizontal overflow: '+label)
                    driver.require(geometry['inline'] and geometry['roster'], 'missing inline detail or retained roster')
                    driver.require(geometry['detailTop'] >= geometry['rosterBottom']-1, 'detail replaced/overlapped roster')
                    bounds=browser.evaluate('''(() => {const w=document.documentElement.clientWidth;return [...document.querySelectorAll('#agent-inline-detail a,#agent-inline-detail button,#agent-inline-detail .spec,#surface-list .registry-card')].filter(x=>x.getClientRects().length).filter(x=>{const r=x.getBoundingClientRect();return r.left < -1 || r.right > w+1;}).map(x=>({tag:x.tagName,text:x.innerText.slice(0,80),right:x.getBoundingClientRect().right,width:w}));})()''')
                    driver.require(not bounds,'visible content escapes narrow viewport: '+str(bounds))
                    separated=browser.evaluate('''[...document.querySelectorAll('#agent-inline-detail .related-records a>span:first-child')].every(x=>x.querySelector('small').getBoundingClientRect().top>=x.querySelector('strong').getBoundingClientRect().bottom)''')
                    driver.require(separated,'related exact-record label/status collision')
                    results.append({'case':label,'geometry':geometry})
                    return text

                for width in (1440,1280,768,390):
                    browser.command('Emulation.setDeviceMetricsOverride',{'width':width,'height':900,'deviceScaleFactor':1,'mobile':False})
                    visit('/console/agents?record_key=office')
                    text=capture('office-'+str(width),width)
                    for expected in ('Verified charter runtime version constraint','>=0.18.0','Provisioning receipt recorded','session-office-recorded','Exact session links','Execution ·','All trust stanza declarations'):
                        driver.require(expected.casefold() in text.casefold(),'missing exact evidence: '+expected)
                    driver.require(browser.evaluate("document.querySelector('[data-authority-state]').dataset.authorityState")=='denied', 'password subject inherited local-os grant')
                    driver.require(browser.evaluate("!!document.querySelector('a[href*=\"record_key=agent-history-item\"]')"),'missing exact Queue link')
                    driver.require('verified-current' in text and 'compatible' in text,'missing independent current precondition observation')
                    card=browser.evaluate("document.querySelector('#record-office .rc-facts').innerText")
                    driver.require('DECLARED STANZAS' in card and '2 of 2 on' in card and 'EFFECTIVE AUTHORITY' in card and 'None · denied' in card and 'Receipt recorded' in card,'roster evidence diverges from exact charter/authenticated evaluation')
                    for summary in ('#agent-inline-detail details:nth-of-type(1)>summary','#agent-inline-detail details:nth-of-type(2)>summary'):
                        driver.click(browser,summary)
                    expanded=browser.evaluate("document.querySelector('#agent-inline-detail').innerText")
                    driver.require('Authentication and identity selectors'.casefold() in expanded.casefold() and 'Information flow'.casefold() in expanded.casefold(),'incomplete declared stanza disclosure')
                    capture('office-expanded-'+str(width),width)

                browser.command('Emulation.setEmulatedMedia',{'features':[{'name':'prefers-reduced-motion','value':'reduce'}]})
                visit('/console/agents')
                driver.tap(browser,'#record-office')
                driver.wait_for(browser,"!!document.querySelector('#agent-inline-detail')",'native narrow touch selection')
                proof=driver.touch_proof(browser)
                driver.require(isinstance(proof,dict) and all(any(e['type']==kind and e['trusted'] for e in proof['events']) for kind in ('touchstart','touchend','click')),'untrusted/incomplete touch delivery')
                results.append({'case':'narrow-touch-reduced-motion','result':'PASS'})
                driver.set_touch_emulation(browser,False)

                # Fragment-only direct entry resolves an exact server read. Refresh
                # retains the selected revision, without making the fragment authority.
                time.sleep(1)
                driver.navigate(browser,origin+'/console/agents#/agents/office')
                driver.wait_for(browser,"!!document.querySelector('#agent-inline-detail') && new URL(location.href).searchParams.get('record_key')==='office'",'direct Agent fragment')
                browser.command('Page.reload')
                driver.wait_for(browser,"document.readyState==='complete' && !!document.querySelector('#agent-inline-detail')",'Agent detail refresh')
                results.append({'case':'direct-and-refresh','result':'PASS'})

                driver.click(browser,'#agent-inline-detail a[href*="session_id=session-office-recorded"]')
                driver.wait_for(browser,"!!document.querySelector('#agent-session-evidence')",'exact recorded session link')
                driver.require(browser.evaluate("document.querySelector('#agent-session-evidence').innerText.includes('session-office-recorded') && document.querySelector('#agent-session-evidence').innerText.includes('ended')"),'session destination mismatched recorded identity/state')
                results.append({'case':'exact-session-navigation','result':'PASS'})
                driver.click(browser,'#agent-inline-detail a[href*="record_key=agent-history-item"]')
                driver.wait_for(browser,"location.pathname==='/console/queue' && !!document.querySelector('#queue-detail')",'exact Queue destination')
                driver.require(browser.evaluate("document.body.innerText.includes('agent-history-item')"),'Queue destination lost exact identity')
                results.append({'case':'exact-queue-navigation','result':'PASS'})
                visit('/console/agents?record_key=office&session_id=session-unrelated')
                driver.require(not browser.evaluate("!!document.querySelector('#agent-session-evidence')"),'unrelated session disclosed')
                (root/'unrelated-session-denied.png').write_bytes(base64.b64decode(browser.command('Page.captureScreenshot',{'format':'png'})['data']))
                results.append({'case':'unrelated-session-denied','result':'PASS'})

                browser.command('Emulation.setDeviceMetricsOverride',{'width':1280,'height':600,'deviceScaleFactor':1,'mobile':False})
                for record,revision,expected in [('missing',1,'No provisioning receipt'),('disabled',2,'Disabled: execution denied'),('retired',2,'Retired: terminal Registry lifecycle'),('history',2,'Historical revision (disabled)')]:
                    visit('/console/agents?record_key='+record+'&revision='+str(revision))
                    text=capture(record,1280)
                    driver.require(expected in text, 'missing readiness/lifecycle evidence: '+expected)
                    if record in ('retired','history'):
                        driver.require(not browser.evaluate("!!document.querySelector('#agent-inline-detail form[action$=lifecycle]')"),'ineligible lifecycle mutation exposed')

                # Unknown URL/prompt fields may not select authenticated identity,
                # stanza, environment or effective authority.
                visit('/console/agents?record_key=office&subject=local-uid%3A1000&stanza=principal&environment=local&prompt=grant-all&unknown=true')
                driver.require(browser.evaluate("document.querySelector('[data-authority-state]').dataset.authorityState")=='denied','URL fields changed server authority')

                # Keyboard follows native links/disclosures and shows a visible
                # focus style; no synthetic click handler supplies authority.
                browser.evaluate("document.querySelector('#agent-inline-detail').focus()")
                driver.key(browser,'Tab')
                focus=browser.evaluate("({tag:document.activeElement.tagName,text:document.activeElement.textContent,style:getComputedStyle(document.activeElement).outlineStyle})")
                driver.require(focus['tag']=='A' and focus['style']!='none','keyboard focus not visible')
                results.append({'case':'keyboard-focus','focus':focus})

                # Actual collection navigation with filter URL, nonzero scroll,
                # selected card, focus and both native link Back and browser Back.
                collection='/console/agents?q=principal-1&lifecycle=all&limit=20'
                for back in ('link','browser'):
                    visit(collection)
                    browser.evaluate('scrollTo(0,240)')
                    y=browser.evaluate('scrollY')
                    driver.require(y>0,'collection fixture did not establish nonzero scroll')
                    driver.click(browser,'#record-office')
                    driver.wait_for(browser,"!!document.querySelector('#agent-inline-detail')",'open exact Agent')
                    y=browser.evaluate("JSON.parse(sessionStorage.getItem('aegis.console.collection.v1:'+document.body.dataset.collectionUrl)).y")
                    driver.require(y>0,'native click did not retain a nonzero collection viewport')
                    time.sleep(1)
                    if back=='link':driver.click(browser,'#close-inspector')
                    else:browser.evaluate('history.back()')
                    driver.wait_for(browser,"!document.querySelector('#agent-inline-detail') && !!document.querySelector('#record-office[aria-current=true]')",'selected collection restored '+back)
                    value=browser.evaluate("({focus:document.activeElement.id,y:scrollY,q:new URL(location.href).searchParams.get('q'),lifecycle:new URL(location.href).searchParams.get('lifecycle')})")
                    driver.require(value['focus']=='record-office' and abs(value['y']-y)<=2 and value['q']=='principal-1' and value['lifecycle']=='all','lost collection context: '+str(value))
                    results.append({'case':'back-'+back,'restoration':value})

                visit('/console/agents?record_key=absent-agent')
                driver.require(browser.evaluate("!document.querySelector('#agent-inline-detail')"),'missing record fabricated detail')
                results.append({'case':'missing-record','result':'PASS'})

                visit('/console/agents?record_key=office')
                driver.click(browser,'#agent-inline-detail form:has(input[name=lifecycle][value=disabled]) button')
                driver.wait_for(browser,"!!document.querySelector('#agent-inline-detail .lifecycle.disabled')",'real exact-current disable')
                driver.require(browser.evaluate("document.querySelector('#agent-inline-detail').innerText.includes('r2')"),'lifecycle successor revision absent')
                visit('/console/agents?record_key=office&revision=1')
                driver.require(not browser.evaluate("!!document.querySelector('#agent-inline-detail form[action$=lifecycle]')"),'stale revision exposed lifecycle form')
                results.append({'case':'lifecycle-and-stale','result':'PASS exact current successor, historical mutation suppressed'})

                # No-script route remains native HTML and safe links; no scripting
                # is used to reconstruct a detail or to mint session navigation.
                browser.command('Emulation.setScriptExecutionDisabled',{'value':True})
                time.sleep(1)
                driver.navigate(browser,origin+'/console/agents?record_key=office')
                time.sleep(2)
                browser.command('Emulation.setScriptExecutionDisabled',{'value':False})
                driver.require(browser.evaluate("!!document.querySelector('#agent-inline-detail') && !!document.querySelector('#close-inspector[href]')"),'no-script HTML detail missing')
                results.append({'case':'no-script','result':'PASS native exact detail and Back link'})
                # Clear only this disposable browser's cookies; auth loss must not
                # retain or re-render protected details.
                browser.command('Network.clearBrowserCookies')
                visit('/console/agents?record_key=office')
                driver.require(browser.evaluate("!!document.querySelector('#password') && !document.querySelector('#agent-inline-detail')"),'auth-loss leaked detail')
                violations=[e for e in browser.events if e.get('method')=='Audits.issueAdded' and e.get('params',{}).get('issue',{}).get('code')=='ContentSecurityPolicyIssue']
                driver.require(not violations,'CSP violation')
                driver.require(not any(e.get('method')=='Runtime.exceptionThrown' for e in browser.events),'uncaught browser exception')
                results.extend([{'case':'auth-loss','result':'PASS'},{'case':'CSP','result':'PASS'}, {'case':'browser-ready-and-ambiguous','result':'UNAVAILABLE','reason':'Canonical accepted policies permit only local-os methods and reject overlapping enabled selectors. Password browser identity must not inherit that grant.'}])
            finally:
                if browser:
                    try: browser.command('Browser.close')
                    except Exception: pass
                    browser.close()
                try: chrome.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(chrome.pid,signal.SIGTERM)
                    chrome.wait(timeout=5)
    finally:
        process.terminate()
        process.wait(timeout=15)
        log.close()
    driver.require(hashlib.sha256(binary.read_bytes()).hexdigest()==identity,'candidate drift')
    (root/'browser-results.json').write_text(json.dumps({'binary_sha256':identity,'results':results},indent=2)+'\n')
    print('PASS: actual frozen Serve, password Console, inline retained roster/evaluated summaries, desktop/narrow evidence, exact session/Queue navigation, lifecycle/history, selection/scroll/focus/filter Back, no-script/auth-loss/CSP. Ready/ambiguous password-browser scenarios explicitly UNAVAILABLE.')

if __name__=='__main__': main()
