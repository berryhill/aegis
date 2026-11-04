#!/usr/bin/env python3
"""Frozen actual-CLI Graph presentation fixture, not live-provider qualification.
Only TestGraphConsoleFrozenBrowser may seed its fresh private stores; it exits
before Serve. Execute under scripts/acceptance_port.py shared lifetime custody.
"""
import base64
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import console_browser_test as driver
from execution_name_browser_test import navigate_ready
from graph_workspace_assertions import assert_graph_geometry, assert_graph_full_fit


class Page:
    def __init__(self, browser): self.browser = browser
    def evaluate(self, expression):
        return self.browser.evaluate('('+expression+')()' if expression.lstrip().startswith('() =>') else expression)


def main():
    binary = Path(os.environ['AEGIS_GRAPH_CANDIDATE']).resolve()
    root = Path(os.environ['AEGIS_GRAPH_ROOT']).resolve()
    port = int(os.environ['AEGIS_ACCEPTANCE_PORT'])
    driver.require(8450 <= port <= 8469, 'candidate port outside shared pool')
    origin = 'http://127.0.0.1:'+str(port)
    identity = hashlib.sha256(binary.read_bytes()).hexdigest()
    manifest = json.loads((root/'fixture-manifest.json').read_text())
    results = []
    log = (root/'serve.log').open('w')
    process = subprocess.Popen([str(binary),'--config',str(root/'owner.json'),'serve'],stdout=log,stderr=log)
    try:
        deadline = time.monotonic()+20
        while True:
            driver.require(process.poll() is None,'candidate exited; see private serve.log')
            try:
                with socket.create_connection(('127.0.0.1',port),.2): break
            except OSError:
                driver.require(time.monotonic()<deadline,'candidate readiness timeout')
                time.sleep(.1)
        with tempfile.TemporaryDirectory(prefix='chrome-',dir=root) as profile:
            chrome = subprocess.Popen(['/usr/bin/google-chrome','--headless=new','--incognito','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0','--remote-allow-origins=http://localhost','--user-data-dir='+profile,'about:blank'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
            browser = None
            try:
                debug = driver.wait_for_active_port(Path(profile)/'DevToolsActivePort',time.monotonic()+15,chrome)
                browser = driver.DevTools(driver.page_websocket(debug,time.monotonic()+8,chrome))
                for domain in ('Page','Runtime','Network','Audits','Accessibility'): browser.command(domain+'.enable')
                page = Page(browser)
                def exact_definitions(value):
                    if isinstance(value, dict):
                        if 'graph_id' in value and 'nodes' in value and 'digest' in value:
                            yield value
                        for child in value.values():
                            yield from exact_definitions(child)
                    elif isinstance(value, list):
                        for child in value:
                            yield from exact_definitions(child)
                navigate_ready(browser,origin+'/console/graphs')
                driver.wait_for(browser,"!!document.querySelector('#password')",'password form')
                driver.insert_text(browser,'#password',os.environ['AEGIS_CONSOLE_PASSWORD'])
                driver.click(browser,'#session-form button[type=submit]')
                driver.wait_for(browser,"!!document.querySelector('#logout')",'authenticated Console')

                def visit(key, fragment=False):
                    time.sleep(1.2)  # preserve the real source limiter, assets included
                    path = '/console/graphs'+('#/graphs/'+key if fragment else '?record_key='+key)
                    if fragment:
                        previous = browser.evaluate('performance.timeOrigin')
                        driver.navigate(browser,origin+path)
                        # The product resolves the fragment through a second
                        # authenticated document with a canonical record_key.
                        driver.wait_for(browser,
                            'performance.timeOrigin !== '+json.dumps(previous)+
                            ' && document.readyState === "complete"'+
                            ' && location.pathname === "/console/graphs"'+
                            ' && new URL(location.href).searchParams.get("record_key") === '+json.dumps(key)+
                            ' && location.hash === '+json.dumps('#/graphs/'+key),
                            'canonical server-resolved Graph fragment')
                    else:
                        navigate_ready(browser,origin+path)
                    driver.wait_for(browser,"!!document.querySelector('[data-graph-workspace]') && !document.querySelector('[data-graph-workspace]').hidden",'authenticated exact Graph workspace')

                def capture(label):
                    browser.evaluate('scrollTo(0,0)')
                    (root/(label+'.png')).write_bytes(base64.b64decode(browser.command('Page.captureScreenshot',{'format':'png','captureBeyondViewport':True})['data']))
                    metrics=browser.evaluate("""(() => {const r=x=>{const b=x.getBoundingClientRect();return {x:b.x,y:b.y,width:b.width,height:b.height}};const c=document.querySelector('.gw-canvas'),i=document.querySelector('#graph-context');return {width:innerWidth,documentWidth:document.documentElement.scrollWidth,canvas:r(c),inspector:r(i),inspectorHidden:i.hidden,nodeCount:document.querySelectorAll('[data-graph-node]').length,edgeCount:document.querySelectorAll('.graph-edge-path').length,tokens:{gap:getComputedStyle(document.querySelector('.graph-workspace')).gap,nodeWidth:getComputedStyle(document.querySelector('[data-graph-node]')).width}}})()""")
                    driver.require(metrics['documentWidth']<=metrics['width']+1,'page horizontal overflow')
                    layout=browser.evaluate("""(() => {
                      const rect=x=>{const r=x.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height}};
                      return {rail:rect(document.querySelector('.nav-rail')),iconVisible:[...document.querySelectorAll('.nav-rail .nav-icon')].some(x=>getComputedStyle(x).display!=='none'),actions:[...document.querySelector('.graph-record-actions').children].map(rect),controls:[...document.querySelectorAll('.canvas-tools button')].map(rect),fields:[...document.querySelectorAll('[data-graph-node-panel]:not([hidden]) .spec dd')].map(rect),bodyWidth:document.querySelector('.gw-body').clientWidth};
                    })()""")
                    if 600<metrics['width']<=1080:
                        driver.require(abs(layout['rail']['width']-60)<=1 and layout['iconVisible'],'tablet Graph detail lost icon rail')
                    if metrics['width']<=600:
                        driver.require(abs(layout['actions'][0]['y']-layout['actions'][1]['y'])<=1,'mobile Graph actions stacked')
                        controls=layout['controls']
                        driver.require(max(x['y'] for x in controls)-min(x['y'] for x in controls)<=1 and min(x['width'] for x in controls)>80,'mobile canvas controls are not a broad single row')
                    for field in layout['fields']:
                        driver.require(field['width']>=layout['bodyWidth']-32,'inspector value column collapsed')
                    related=browser.evaluate("""[...document.querySelectorAll('[data-graph-node-panel]:not([hidden]) .related-records a, [data-graph-node-panel]:not([hidden]) .related-records a span, [data-graph-node-panel]:not([hidden]) .related-records strong, [data-graph-node-panel]:not([hidden]) .related-records small')].map(x=>({text:x.textContent,width:x.clientWidth,scrollWidth:x.scrollWidth,height:x.clientHeight,scrollHeight:x.scrollHeight,whiteSpace:getComputedStyle(x).whiteSpace}))""")
                    for row in related:
                        driver.require(row['scrollWidth']<=row['width']+1 and row['scrollHeight']<=row['height']+1 and row['whiteSpace']!='nowrap','related exact reference clipped')
                    metrics['relatedRecords']=related
                    metrics['readability']=layout
                    if not metrics['inspectorHidden']:
                        c,i=metrics['canvas'],metrics['inspector']
                        if metrics['width']<=1080:
                            driver.require(i['y']>=c['y']+c['height']-1,'narrow inspector did not stack')
                        else:
                            driver.require(i['x']>=c['x']+c['width']-1,'desktop inspector did not sit beside canvas')
                            driver.require(abs(i['width']-(312 if metrics['width']<=1280 else 340))<=2,'inspector width differs from accepted breakpoint')
                    results.append({'case':label,'metrics':metrics})

                for width in (1440,1280,768,390):
                    browser.command('Emulation.setDeviceMetricsOverride',{'width':width,'height':1000 if width>=1280 else 844,'deviceScaleFactor':1,'mobile':False})
                    browser.command('Emulation.setTouchEmulationEnabled',{'enabled':width==390,'maxTouchPoints':1})
                    for graph in manifest['graphs']:
                        key=graph['graph_id']+':'+str(graph['revision'])
                        visit(key)
                        driver.require(browser.evaluate("document.querySelectorAll('a[data-detail-link]').length")==0,'detail retained collection')
                        raw=browser.evaluate("document.querySelector('.graph-record-json').textContent")
                        driver.require(any(value==graph for value in exact_definitions(json.loads(raw))),'rendered definition differs from authoritative durable manifest')
                        driver.require(graph['digest'] in raw,'exact durable digest absent')
                        for node in graph['nodes']:
                            driver.require(node['participant']['digest'] in raw and node['loop']['digest'] in raw,'exact pins absent')
                        assert_graph_geometry(page)
                        # Every SVG endpoint is checked against its authoritative
                        # dependency's exact node identity, never just the first edge.
                        geometry=browser.evaluate('''(() => {const graph=JSON.parse('''+json.dumps(json.dumps(graph))+''');const nodes=[...document.querySelectorAll('[data-graph-node]')];const errors=[];const proof=[];for(const [i,e] of (graph.dependencies || []).entries()){const path=document.querySelectorAll('.graph-edge-path')[i];if(!path){errors.push(e.id);continue;}const m=path.getScreenCTM(),a=path.getPointAtLength(0).matrixTransform(m),b=path.getPointAtLength(path.getTotalLength()).matrixTransform(m);const from=nodes[graph.nodes.findIndex(n=>n.id===e.from_node_id)].getBoundingClientRect(),to=nodes[graph.nodes.findIndex(n=>n.id===e.to_node_id)].getBoundingClientRect();if(Math.abs(a.x-from.right)>2 || a.y<from.top || a.y>from.bottom || Math.abs(b.x-to.left)>2 || b.y<to.top || b.y>to.bottom)errors.push(e.id);proof.push({id:e.id,start:[a.x,a.y],end:[b.x,b.y]});}return {errors,proof};})()''')
                        driver.require(not geometry['errors'],'disconnected edges '+str(geometry))
                        results.append({'case':'all-edges-'+key+'-'+str(width),'geometry':geometry})
                        start=len(browser.events)
                        driver.click(browser,'[data-graph-fit]')
                        assert_graph_full_fit(page)
                        capture(key.replace(':','-')+'-'+str(width)+'-fit')
                        # Restore zoom with keyboard, then prove actual bounded pan.
                        browser.evaluate("document.querySelector('[data-graph-viewport]').focus()")
                        for _ in range(10):
                            for event_type in ('rawKeyDown','keyUp'):
                                browser.command('Input.dispatchKeyEvent',{'type':event_type,'key':'+','code':'Equal','modifiers':8,'windowsVirtualKeyCode':187,'nativeVirtualKeyCode':187})
                        driver.key(browser,'ArrowRight')
                        pan=browser.evaluate("(() => {const v=document.querySelector('[data-graph-viewport]');return {x:v.scrollLeft,max:v.scrollWidth-v.clientWidth}})()")
                        driver.require(0<=pan['x']<=pan['max']+1,'pan escaped bounds')
                        if graph['graph_id']=='graph-chain-128': driver.require(pan['x']>0,'keyboard did not pan large Graph')
                        browser.evaluate("document.querySelector('[data-graph-node]').focus()")
                        driver.key(browser,'Enter')
                        driver.wait_for(browser,"!document.querySelector('[data-graph-node-panel=\"0\"]').hidden",'keyboard node inspector')
                        focus=browser.evaluate("({id:document.activeElement.id,outline:getComputedStyle(document.activeElement).outlineStyle})")
                        driver.require(focus['id']=='graph-context-title','inspector focus missing')
                        capture(key.replace(':','-')+'-'+str(width)+'-selected')
                        driver.key(browser,'Escape')
                        mutations=[e for e in browser.events[start:] if e.get('method')=='Network.requestWillBeSent' and e['params']['request']['method'] not in ('GET','HEAD')]
                        driver.require(not mutations,'selection/pan/zoom mutated server')

                    # Native pointer/touch evidence is distinct from invoking handlers.
                    visit('graph-chain-128:1')
                    browser.evaluate("document.querySelector('[data-graph-viewport]').scrollIntoView({block:'center'});document.querySelector('[data-graph-viewport]').scrollLeft=0")
                    point=browser.evaluate("(() => {const r=document.querySelector('[data-graph-viewport]').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+8}})()")
                    browser.command('Input.dispatchMouseEvent',{'type':'mousePressed',**point,'button':'left','buttons':1,'clickCount':1})
                    browser.command('Input.dispatchMouseEvent',{'type':'mouseMoved','x':point['x']-60,'y':point['y'],'button':'left','buttons':1})
                    browser.command('Input.dispatchMouseEvent',{'type':'mouseReleased','x':point['x']-60,'y':point['y'],'button':'left','buttons':0,'clickCount':1})
                    driver.wait_for(browser,"document.querySelector('[data-graph-viewport]').scrollLeft>0",'native mouse drag pans actual canvas')
                    results.append({'case':'native-mouse-drag-'+str(width),'scrollLeft':browser.evaluate("document.querySelector('[data-graph-viewport]').scrollLeft")})
                    if width==390:
                        visit('graph-proof:2')
                        driver.tap(browser,'[data-graph-node="0"]')
                        driver.wait_for(browser,"!document.querySelector('[data-graph-node-panel=\"0\"]').hidden",'native touch selects inspector')
                        results.append({'case':'native-touch-node-selection','proof':json.loads(browser.evaluate('sessionStorage.getItem('+json.dumps(driver.TOUCH_PROOF_STORAGE_KEY)+')'))})

                visit('graph-proof:1',fragment=True)
                browser.command('Page.reload')
                driver.wait_for(browser,"!!document.querySelector('.graph-record-json') && !document.querySelector('[data-graph-workspace]').hidden",'exact refresh')
                driver.require(browser.evaluate("new URL(location.href).searchParams.get('record_key')")=='graph-proof:1','refresh changed pin')
                # Native exact Agent and Loop links must resolve real fixtures.
                links=browser.evaluate("[...document.querySelector('[data-graph-node-panel=\"0\"]').querySelectorAll('a')].map(a=>({text:a.textContent,url:a.href}))")
                for link in links:
                    time.sleep(1.2)
                    navigate_ready(browser,link['url'])
                    driver.require(browser.evaluate('document.body.innerText').find(manifest['agent']['id'] if '/agents' in link['url'] else manifest['loop']['id'])>=0,'exact destination absent')
                visit('graph-proof:2')
                driver.click(browser,'[data-graph-node="0"]')
                driver.require(manifest['agent']['digest'] in browser.evaluate("document.querySelector('[data-graph-node-panel=\"0\"]').textContent"),'inspector lost Agent digest')
                driver.click(browser,'a.primary[href]')
                driver.wait_for(browser,"location.pathname.includes('/run')",'prepare exact request')
                driver.require(not browser.evaluate("!!document.querySelector('.graph-edge-path')"),'prepare retained workspace')
                # Exact pins and CSRF are independently enforced by the real
                # owning server. Only negative requests are made: no Graph run.
                denials=browser.evaluate("""(async () => {
                  const u=new URL(location.href);u.searchParams.set('digest','sha256:'+ '0'.repeat(64));
                  const wrongPin=await fetch(u.href,{credentials:'same-origin',cache:'no-store'});
                  const body=new URLSearchParams({csrf:'invalid-fixture-csrf',graph:'graph-proof',revision:'2',digest:'sha256:'+ '0'.repeat(64)});
                  const statuses=[];
                  for(let i=0;i<2;i++) statuses.push((await fetch('/console/graphs/submit',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},body})).status);
                  const queue=await fetch('/console/queue',{credentials:'same-origin',cache:'no-store'});
                  const doc=new DOMParser().parseFromString(await queue.text(),'text/html');
                  return {wrongPin:wrongPin.status,csrf:statuses,queueState:doc.querySelector('#service-status')?.dataset.state,queueRecords:doc.querySelectorAll('a[data-record-key]').length,authenticated:!!doc.querySelector('#logout')};
                })()""")
                driver.require(denials['wrongPin']==409,'tampered exact Graph digest accepted')
                driver.require(denials['csrf']==[403,403],'invalid CSRF or its replay was not denied')
                driver.require(denials['authenticated'] and denials['queueState'] in ('empty','ready') and denials['queueRecords']==0,'denied requests did not retain authoritative empty Queue readback')
                results.append({'case':'exact-pin-CSRF-replay-denials-and-queue-readback','evidence':denials})
                # No actual Graph submission is made by this read-only fixture.
                visit('graph-proof:2')
                browser.command('Emulation.setScriptExecutionDisabled',{'value':True})
                time.sleep(1.2)
                driver.navigate(browser,origin+'/console/graphs?record_key=graph-proof:1')
                time.sleep(2)
                browser.command('Emulation.setScriptExecutionDisabled',{'value':False})
                driver.require(browser.evaluate("document.querySelectorAll('.graph-textual details').length")>=6,'no-script textual nodes absent')
                visit('graph-proof:2')
                driver.require(os.environ['AEGIS_CONSOLE_PASSWORD'] not in browser.evaluate('document.documentElement.outerHTML'),'password leaked to DOM')
                browser.command('Network.clearBrowserCookies')
                # Same open document, no navigation: actual auth GET must clear
                # JSON, controls, inspector and all other protected body content.
                browser.evaluate("dispatchEvent(new Event('focus'))")
                driver.wait_for(browser,"!!document.querySelector('[data-graph-auth-cleared]') && !document.querySelector('[data-graph-workspace],.graph-record-json,#logout')",'open-page auth loss clearing')
                driver.require(browser.evaluate("document.querySelector('[data-graph-auth-cleared]').dataset.graphAuthCleared")=='authentication-required','cookie loss misreported as network failure')
                violations=[e for e in browser.events if e.get('method')=='Audits.issueAdded' and e.get('params',{}).get('issue',{}).get('code')=='ContentSecurityPolicyIssue']
                driver.require(not violations,'strict CSP violation: '+json.dumps(violations))
                driver.require(not any(e.get('method')=='Runtime.exceptionThrown' for e in browser.events),'uncaught browser exception')
                results.append({'case':'open-page-auth-loss-and-CSP','result':'PASS'})
            finally:
                if browser:
                    try: browser.command('Browser.close')
                    except Exception: pass
                    browser.close()
                try: chrome.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(chrome.pid,signal.SIGTERM);chrome.wait(timeout=5)
    finally:
        process.terminate();process.wait(timeout=15);log.close()
    driver.require(hashlib.sha256(binary.read_bytes()).hexdigest()==identity,'candidate drift')
    (root/'browser-results.json').write_text(json.dumps({'binary_sha256':identity,'fixture_tier':manifest['tier'],'results':results},indent=2)+'\n')
    print('PASS actual frozen CLI Graph read-only browser journeys; see machine evidence. No live-provider or submission qualification.')


if __name__=='__main__': main()
