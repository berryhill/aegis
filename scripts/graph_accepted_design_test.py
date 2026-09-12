#!/usr/bin/env python3
"""Offline readback of the operator-approved comparison input, not product authority."""
import hashlib
import json
from pathlib import Path
from playwright.sync_api import sync_playwright

SOURCE = Path('/home/silas/.hermes/.scratch/xander-accepted-design-recovery/index.html')
DIGEST = '634e23fafafb5cd032a86ac80dbc3809d8e5bd9939cda80c16584beb4d4d2a9b'
ROUTE = '#/graphs/gr-contract-intake'

def main():
    data = SOURCE.read_bytes()
    assert len(data) == 755716, 'accepted design byte count mismatch'
    assert hashlib.sha256(data).hexdigest() == DIGEST
    proof = Path(__file__).resolve().parents[1] / '.aegis-graph-proof'
    proof.mkdir(exist_ok=True)
    results = []
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path='/usr/bin/google-chrome', headless=True)
        for width in (1440, 390):
            context = browser.new_context(viewport={'width': width, 'height': 900}, has_touch=True, offline=True)
            page = context.new_page()
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.goto(SOURCE.as_uri() + ROUTE)
            nodes = page.locator('button.node:visible')
            assert nodes.count() == 6
            assert page.get_by_text('Back to Graphs', exact=False).is_visible()
            assert page.get_by_role('button', name='Definition details', exact=True).is_visible()
            initial = page.locator('body').inner_text()
            assert 'Contract Review Intake' in initial and 'Not submittable:' in initial
            assert 'Definition details' in initial
            page.screenshot(path=str(proof / f'accepted-{width}.png'), full_page=True)
            nodes.first.tap()
            selected = page.locator('body').inner_text()
            assert selected != initial, 'node must open contextual detail'
            page.screenshot(path=str(proof / f'accepted-selected-{width}.png'), full_page=True)
            assert not errors, errors
            results.append({'width': width, 'nodes': nodes.count(), 'initial_text': initial, 'selected_text': selected, 'page_errors': errors})
            context.close()
        browser.close()
    result = {'source': str(SOURCE), 'sha256': DIGEST, 'bytes': len(data), 'route': ROUTE, 'offline': True, 'observations': results}
    (proof / 'accepted-design.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'source': str(SOURCE), 'sha256': DIGEST, 'bytes': len(data), 'route': ROUTE, 'widths': [r['width'] for r in results], 'status': 'passed'}))

if __name__ == '__main__':
    main()
