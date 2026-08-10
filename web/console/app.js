(() => {
  'use strict';
  let csrf = '';
  let surface = {agents: [], loops: [], graphs: [], queue: [], readiness: {}};
  let domain = 'agents';
  const titles = {agents: 'Agent Registry', loops: 'Loops', graphs: 'Graphs', queue: 'Execution Queue'};
  const auth = document.getElementById('authentication');
  const authStatus = document.getElementById('authentication-status');
  const fleet = document.getElementById('fleet');
  const status = document.getElementById('service-status');
  const list = document.getElementById('surface-list');
  const inspector = document.getElementById('inspector');
  const setState = (name) => document.querySelectorAll('.service-state').forEach((node) => { node.hidden = node.dataset.state !== name; });
  const records = () => Array.isArray(surface[domain]) ? surface[domain] : [];
  const identity = (record) => {
    if (domain === 'agents') return `${record.registration?.agent_id || 'unknown'} · revision ${record.revision?.revision || '?'}`;
    if (domain === 'loops') return `${record.revision?.loop_id || 'unknown'} · revision ${record.revision?.revision || '?'}`;
    if (domain === 'graphs') return `${record.revision?.graph_id || 'unknown'} · revision ${record.revision?.revision || '?'}`;
    return `${record.item?.item_id || 'unknown'} · ${record.projection?.state || 'unknown'}`;
  };
  const showRecord = (record, button) => {
    document.getElementById('inspector-summary').textContent = identity(record);
    document.getElementById('inspector-record').textContent = JSON.stringify(record, null, 2);
    inspector.hidden = false;
    inspector.dataset.returnFocus = button.id;
    inspector.focus();
  };
  const render = () => {
    document.getElementById('surface-title').textContent = titles[domain];
    document.querySelectorAll('nav [data-domain]').forEach((node) => node.setAttribute('aria-current', node.dataset.domain === domain ? 'page' : 'false'));
    list.replaceChildren();
    const values = records();
    values.forEach((record, index) => {
      const item = document.createElement('li');
      const button = document.createElement('button');
      button.type = 'button'; button.id = `${domain}-${index}`; button.textContent = identity(record);
      button.addEventListener('click', () => showRecord(record, button));
      item.append(button); list.append(item);
    });
    const readiness = surface.readiness?.[domain === 'agents' ? 'registry' : domain];
    const authoritative = readiness?.authoritative === true;
    const state = authoritative && values.length === 0 ? 'empty' : (authoritative ? 'ready' : 'unavailable');
    setState(state);
    const total = Number.isInteger(readiness?.count) ? readiness.count : values.length;
    const count = values.length === total ? `${total}` : `showing ${values.length} of ${total}`;
    status.textContent = authoritative ? `${count} authoritative ${titles[domain].toLowerCase()} record${total === 1 ? '' : 's'}. Exact revisions and digests shown.` : `${titles[domain]} state is unavailable.`;
  };
  const load = async () => {
    setState('loading');
    const response = await fetch('/console/api/state', {credentials: 'same-origin', headers: {'Accept': 'application/json'}});
    if (response.status === 401) {
      auth.hidden = false; fleet.hidden = true; document.getElementById('logout').hidden = true;
      authStatus.textContent = 'A short-lived browser session is required.'; return;
    }
    auth.hidden = true; fleet.hidden = false; authStatus.textContent = ''; document.getElementById('logout').hidden = false;
    if (response.status === 503) { setState('unavailable'); status.textContent = 'Fleet control unavailable. No collection was treated as empty.'; return; }
    if (!response.ok) { setState('error'); status.textContent = 'Fleet state failed to load.'; return; }
    const data = await response.json();
    csrf = typeof data.csrf === 'string' ? data.csrf : '';
    surface = data.surface && typeof data.surface === 'object' ? data.surface : {agents: [], loops: [], graphs: [], queue: [], readiness: {}};
    render();
  };
  const selectHashDomain = () => {
    const requested = window.location.hash.slice(1);
    if (Object.hasOwn(titles, requested)) domain = requested;
    inspector.hidden = true;
    render();
  };
  window.addEventListener('hashchange', selectHashDomain);
  document.querySelectorAll('nav [data-domain]').forEach((node) => node.addEventListener('click', () => { domain = node.dataset.domain; inspector.hidden = true; render(); }));
  document.getElementById('session-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const input = document.getElementById('bootstrap');
    const response = await fetch('/console/session', {method: 'POST', credentials: 'same-origin', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({bootstrap: input.value})});
    input.value = '';
    if (!response.ok) { authStatus.textContent = 'Authentication denied.'; input.focus(); return; }
    await load();
  });
  document.getElementById('logout').addEventListener('click', async () => {
    await fetch('/console/session', {method: 'DELETE', credentials: 'same-origin', headers: {'X-CSRF-Token': csrf}});
    csrf = ''; await load();
  });
  document.getElementById('close-inspector').addEventListener('click', () => {
    inspector.hidden = true;
    const prior = document.getElementById(inspector.dataset.returnFocus || '');
    if (prior) prior.focus();
  });
  document.addEventListener('keydown', (event) => { if (event.key === 'Escape' && !inspector.hidden) document.getElementById('close-inspector').click(); });
  const requested = window.location.hash.slice(1);
  if (Object.hasOwn(titles, requested)) domain = requested;
  load().catch(() => { fleet.hidden = false; setState('error'); status.textContent = 'Fleet state failed to load.'; });
})();
