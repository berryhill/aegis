"""Trusted behavioral parity assertions; no replacement layout algorithm."""
import math

def geometry(page, kind):
    return page.evaluate('''kind => {
      const stage=document.querySelector(`[data-${kind}-stage]`), box=stage.getBoundingClientRect();
      const transform=getComputedStyle(stage).transform;
      const zoom=transform==='none'?1:new DOMMatrix(transform).a;
      const nodes=[...stage.querySelectorAll(`[data-${kind}-node]`)].map(n=>{
        const b=n.getBoundingClientRect();return {index:n.dataset[kind+'Node'],column:n.dataset.gridColumn,row:n.dataset.gridRow,
        rect:[(b.x-box.x)/zoom,(b.y-box.y)/zoom,b.width/zoom,b.height/zoom]};});
      const edges=[...stage.querySelectorAll('path[d][marker-end]')].map(e=>({
        path:e.getAttribute('d'),arrow:!!document.querySelector(e.getAttribute('marker-end').slice(4,-1)),
        feedback:e.classList.contains('back-edge')||e.classList.contains('feedback'),
        id:e.dataset.transitionId||e.dataset.edgeId||e.querySelector('title')?.textContent.split(' · ')[0]
      }));
      const labels=[...stage.querySelectorAll('text')].map(e=>({text:e.textContent,x:e.getAttribute('x'),y:e.getAttribute('y')}));
      return {nodes,edges,labels,viewBox:stage.querySelector('svg').getAttribute('viewBox'),columns:stage.dataset.columns,rows:stage.dataset.rows};
    }''',kind)

def baseline(context,base):
    page=context.new_page()
    page.goto(base+'/console/loops?record_key=debug-loop:1#/loops/debug-loop:1')
    page.wait_for_selector('[data-loop-node]')
    result=geometry(page,'loop')
    assert len(result['nodes'])==9 and len(result['edges'])==10,result
    assert len({n['row'] for n in result['nodes']})>1,result
    assert any('retry' in l['text'] and 'max 2' in l['text'] for l in result['labels']),result
    page.close()
    return result

def parity(page,expected):
    got=geometry(page,'queue')
    assert got['viewBox']==expected['viewBox'],('SVG dimensions',got,expected)
    assert got['columns']==expected['columns'] and got['rows']==expected['rows'],('cell bounds',got,expected)
    assert len(got['nodes'])==len(expected['nodes']),got
    for a,b in zip(got['nodes'],expected['nodes']):
        assert (a['index'],a['column'],a['row'])==(b['index'],b['column'],b['row']),('cells',a,b)
        assert all(math.isclose(x,y,abs_tol=0.6) for x,y in zip(a['rect'],b['rect'])),('normalized rectangles',a,b)
    assert sorted(e['path'] for e in got['edges'])==sorted(e['path'] for e in expected['edges']),('directed paths/feedback',got,expected)
    assert all(e['arrow'] for e in got['edges']),('missing arrowheads',got)
    assert sorted((l['text'],l['x'],l['y']) for l in got['labels'])==sorted((l['text'],l['x'],l['y']) for l in expected['labels']),('branch/bound labels',got,expected)
    return got

def initial(context,base,detail,expected):
    # Initial no-JavaScript HTML has the same authoritative geometry, not just
    # a client-side correction after the first poll. Graph overview is distinct.
    nojs=context.browser.new_context(java_script_enabled=False,storage_state=context.storage_state())
    page=nojs.new_page();page.goto(detail)
    parity(page,expected)
    nojs.close()

def reduced_motion(page):
    durations=page.locator('.queue-edge-path, .queue-node').evaluate_all('''es => es.map(e=>{
      const s=getComputedStyle(e);return [s.animationDuration,s.transitionDuration];})''')
    for pair in durations:
        for values in pair:
            assert all(float(v.strip().removesuffix('ms').removesuffix('s'))<=0.01 for v in values.split(',')),durations
