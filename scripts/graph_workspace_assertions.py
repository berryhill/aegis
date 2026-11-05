"""Graph-only browser assertions shared by component/Console proof scripts.
These assertions do not create a listener or supply authenticated acceptance.
"""


def assert_graph_geometry(page):
    result = page.evaluate("""() => {
      const stage = document.querySelector('[data-graph-stage]');
      const r = stage.getBoundingClientRect();
      const scale = r.width / stage.offsetWidth;
      const errors = [];
      for (const n of stage.querySelectorAll('[data-graph-node]')) {
        const b = n.getBoundingClientRect();
        const x = r.left + (24 + (Number(n.dataset.gridColumn)-1)*290)*scale;
        const y = r.top + (24 + (Number(n.dataset.gridRow)-1)*132)*scale;
        if (Math.abs(b.left-x)>1 || Math.abs(b.top-y)>1 ||
            Math.abs(b.width-212*scale)>1 || Math.abs(b.height-102*scale)>1)
          errors.push({node:n.dataset.graphNode, expected:[x,y], actual:[b.left,b.top,b.width,b.height]});
      }
      const svg = stage.querySelector('svg').getBoundingClientRect();
      if (Math.abs(svg.width-r.width)>1 || Math.abs(svg.height-r.height)>1)
        errors.push('SVG and cards scaled differently');
      return errors;
    }""")
    assert not result, f"Graph projected grid/card/SVG mismatch: {result}"


def assert_graph_zoom_floor(page):
    result = page.evaluate("""() => {
      const s = document.querySelector('[data-graph-stage]');
      const v = document.querySelector('[data-graph-viewport]');
      const fit = Math.min(1,(v.clientWidth-8)/s.scrollWidth,(v.clientHeight-8)/s.scrollHeight);
      const z = new DOMMatrix(getComputedStyle(s).transform).a;
      return {z, expected:Math.min(.3,fit)};
    }""")
    assert abs(result['z']-result['expected']) < 0.00001, result


def assert_graph_full_fit(page):
    result = page.evaluate("""() => {
      const s = document.querySelector('[data-graph-stage]');
      const v = document.querySelector('[data-graph-viewport]');
      const r = v.getBoundingClientRect();
      const inside = b => b.left>=r.left-1 && b.top>=r.top-1 &&
        b.right<=r.left+v.clientWidth+1 && b.bottom<=r.top+v.clientHeight+1;
      const omitted = [...s.querySelectorAll('[data-graph-node]')].filter(n=>!inside(n.getBoundingClientRect()));
      return {stage:inside(s.getBoundingClientRect()), omitted:omitted.map(n=>n.dataset.graphNode),
        panX:v.scrollWidth-v.clientWidth,panY:v.scrollHeight-v.clientHeight};
    }""")
    assert result['stage'] and not result['omitted'], f"Fit did not include complete Graph: {result}"
    assert result['panX'] <= 1 and result['panY'] <= 1, f"Fit retained unscaled phantom pan extent: {result}"
    assert_graph_geometry(page)
