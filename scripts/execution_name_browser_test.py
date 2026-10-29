#!/usr/bin/env python3
"""Frozen executable proof; invoke from TestExecutionLoopNameSeed's harness.
Synthetic isolated state only. Password comes from the test fixture environment.
No provider execution, production credentials, or authority changes.
"""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import console_browser_test as driver


def navigate_ready(browser, url):
    """Reject the previous same-URL document before waiting for layout."""
    previous = browser.evaluate('performance.timeOrigin')
    driver.navigate(browser, url)
    driver.wait_for(browser,
                    'performance.timeOrigin !== ' + json.dumps(previous) +
                    ' && location.href === ' + json.dumps(url) +
                    ' && document.readyState === "complete"',
                    'new complete execution-name document')
    result = browser.command('Runtime.evaluate', {
        'expression': '(async () => {await document.fonts.ready; '
                      'await new Promise(requestAnimationFrame); '
                      'await new Promise(requestAnimationFrame); return true;})()',
        'awaitPromise': True, 'returnByValue': True,
    })
    driver.require(not result.get('exceptionDetails') and
                   result.get('result', {}).get('value') is True,
                   'execution-name render readiness failed')


def record_geometry(root, results, value):
    # Persist the offending operands before any assertion can abort the proof.
    results.append(value)
    (root / 'browser-results.json').write_text(json.dumps(results, indent=2) + '\n')
    print('execution-name geometry: ' + json.dumps(value, sort_keys=True), flush=True)


def main():
    binary = Path(os.environ['AEGIS_EXECUTION_NAME_CANDIDATE']).resolve()
    root = Path(os.environ['AEGIS_EXECUTION_NAME_ROOT']).resolve()
    origin = 'http://127.0.0.1:' + os.environ['AEGIS_ACCEPTANCE_PORT']
    log = (root / 'serve.log').open('w')
    process = subprocess.Popen([str(binary), '--config', str(root / 'owner.json'), 'serve'], stdout=log, stderr=log)
    try:
        deadline = time.monotonic() + 20
        while True:
            driver.require(process.poll() is None, 'candidate exited; see serve.log')
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
                navigate_ready(browser,origin+'/console/queue')
                driver.wait_for(browser,"!!document.querySelector('#password')",'login')
                driver.insert_text(browser,'#password',os.environ['AEGIS_CONSOLE_PASSWORD'])
                driver.click(browser,'#session-form button[type=submit]')
                driver.wait_for(browser,"!!document.querySelector('#logout')",'authenticated')
                results=[]
                for width in (1280,390):
                    time.sleep(1)
                    browser.command('Emulation.setDeviceMetricsOverride',{'width':width,'height':900,'deviceScaleFactor':1,'mobile':False})
                    navigate_ready(browser,origin+'/console/queue?visibility=all&limit=1')
                    driver.wait_for(browser,"!!document.querySelector('#record-debug-queue')",'pinned list')
                    value=browser.evaluate('''(() => {const a=document.querySelector('#record-debug-queue'); const b=a.getBoundingClientRect();return {name:a.querySelector('strong').textContent,href:a.getAttribute('href'),width:innerWidth,left:b.left,right:b.right,injection:a.querySelector('changes')!==null};})()''')
                    record_geometry(root, results, value)
                    driver.require(value['name']=='Review <changes> & "ship"','wrong pinned archived name')
                    driver.require('record_key=debug-queue' in value['href'],'changed detail identity')
                    driver.require(not value['injection'] and value['left']>=0 and value['right']<=width+1,'escaping or narrow viewport failure')
                    ax=browser.command('Accessibility.getFullAXTree')['nodes']
                    driver.require(any(n.get('role',{}).get('value')=='link' and 'Review <changes>' in n.get('name',{}).get('value','') for n in ax),'name absent from accessible link')
                driver.require(not [e for e in browser.events if e.get('method')=='Audits.issueAdded' and e.get('params',{}).get('issue',{}).get('code')=='ContentSecurityPolicyIssue'],'CSP violation')
                (root/'browser-results.json').write_text(json.dumps(results,indent=2)+'\n')
                print('PASS: frozen candidate authenticated pinned archived name; 1280/390px, escaping, accessible link and identity')
            finally:
                if browser: browser.close()
                os.killpg(chrome.pid,signal.SIGTERM)
                chrome.wait(timeout=15)
    finally:
        process.terminate()
        process.wait(timeout=15)
        log.close()

if __name__=='__main__':
    main()
