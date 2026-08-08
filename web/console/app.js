(() => {
  'use strict';
  let csrf = '';
  const auth = document.getElementById('authentication');
  const authStatus = document.getElementById('authentication-status');
  const fleet = document.getElementById('fleet');
  const status = document.getElementById('service-status');
  const list = document.getElementById('agent-list');
  const inspector = document.getElementById('inspector');
  const setState = (name) => document.querySelectorAll('.service-state').forEach((node) => { node.hidden = node.dataset.state !== name; });
  const showAgent = (name, button) => {
    document.getElementById('agent-name').textContent = name;
    inspector.hidden = false;
    inspector.dataset.returnFocus = button.id;
    inspector.focus();
  };
  const load = async () => {
    setState('loading');
    const response = await fetch('/console/api/state', {credentials: 'same-origin', headers: {'Accept': 'application/json'}});
    if (response.status === 401) {
      auth.hidden = false; fleet.hidden = true; document.getElementById('logout').hidden = true;
      authStatus.textContent = 'A short-lived browser session is required.'; return;
    }
    auth.hidden = true; fleet.hidden = false;
    authStatus.textContent = '';
    document.getElementById('logout').hidden = false;
    if (response.status === 503) { setState('unavailable'); status.textContent = 'Registry service unavailable.'; return; }
    if (!response.ok) { setState('error'); status.textContent = 'Fleet state failed to load.'; return; }
    const data = await response.json();
    csrf = typeof data.csrf === 'string' ? data.csrf : '';
    list.replaceChildren();
    const agents = Array.isArray(data.agents) ? data.agents : [];
    agents.forEach((name, index) => {
      const item = document.createElement('li');
      const button = document.createElement('button');
      button.type = 'button'; button.id = `agent-${index}`; button.textContent = String(name);
      button.addEventListener('click', () => showAgent(String(name), button));
      item.append(button); list.append(item);
    });
    setState(agents.length === 0 ? 'empty' : 'ready');
    status.textContent = agents.length === 0 ? 'Authoritative registry is empty.' : `${agents.length} authorized agent records.`;
  };
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
  load().catch(() => { fleet.hidden = false; setState('error'); status.textContent = 'Fleet state failed to load.'; });
})();
