"""Real browser regression over Go-rendered synthetic metadata. No external network."""
import sys
from playwright.sync_api import expect, sync_playwright
from urllib.parse import parse_qs, urlsplit

base = sys.argv[1]
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path="/usr/bin/google-chrome", args=["--no-sandbox"])
    for width in [1440, 900, 390]:
        context = browser.new_context(viewport={"width": width, "height": 900})
        context.route("**/*", lambda route: route.continue_() if route.request.url.startswith(base + "/") else route.abort())
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(base + "/console/credentials?q=provider&status=active#/credentials")
        inventory = page.locator("#credential-inventory")
        link = page.locator('#record-secret-20')
        link.scroll_into_view_if_needed()
        before = inventory.evaluate("e=>e.scrollTop")
        link.click()
        page.wait_for_url(lambda url: 'record_key=secret-20' in url)
        assert page.locator('#inspector-title').inner_text() == 'provider/test'
        detail = page.locator('.credential-detail')
        if width > 900:
            assert inventory.is_visible() and detail.is_visible()
            a, b = inventory.bounding_box(), detail.bounding_box()
            assert a is not None and b is not None
            assert abs(a['width'] / (a['width'] + b['width']) - .42) < .02
            assert inventory.evaluate('e=>e.scrollHeight>e.clientHeight')
            assert detail.evaluate('e=>e.scrollHeight>e.clientHeight')
            old = inventory.evaluate('e=>e.scrollTop')
            detail.evaluate('e=>e.scrollTop=200')
            assert inventory.evaluate('e=>e.scrollTop') == old
        else:
            assert not inventory.is_visible() and detail.is_visible()
        page.go_back()
        expect(link).to_be_focused()
        assert inventory.is_visible()
        assert page.locator('[name=q]').input_value() == 'provider'
        assert page.locator('[name=status]').input_value() == 'active'
        assert link.get_attribute('aria-current') == 'true'
        assert abs(inventory.evaluate('e=>e.scrollTop') - before) < 2
        page.go_forward()
        page.locator('#close-inspector').click()
        expect(link).to_be_focused()
        assert inventory.is_visible()
        assert abs(inventory.evaluate('e=>e.scrollTop') - before) < 2
        page.evaluate("location.hash='#/credentials/secret-03'")
        page.wait_for_url(lambda url: 'record_key=secret-03' in url)
        assert page.locator('#inspector-title').inner_text() == 'provider/test'
        page.evaluate("location.hash='#/credentials/missing'")
        page.wait_for_url(lambda url: 'record_key=missing' in url)
        assert page.locator('#inspector-title').inner_text() == 'Credential unavailable'
        assert page.locator('a:has-text("Prepare rotation")').count() == 0
        page.evaluate("location.hash=''")
        page.wait_for_url(lambda url: 'record_key' not in parse_qs(urlsplit(url).query))
        # Invalid syntax/encoding and oversized keys must never cause a server read.
        for fragment in ['#/credentials/%ZZ', '#/credentials/a/b', '#/credentials/' + 'x' * 1025,
                         '#/credentials/' + 'x' * 13000, '#/credentials-evil/key', '#https://evil.invalid/']:
            page.evaluate('(fragment) => { history.replaceState(null, "", fragment); window.dispatchEvent(new HashChangeEvent("hashchange")); }', fragment)
            page.wait_for_timeout(80)
            assert page.evaluate('new URL(location.href).searchParams.get("record_key")') is None
        # Encoded URL/markup is a data key, never navigation authority or HTML.
        for key in ['https://evil.invalid/', '<img src=x onerror=alert(1)>', '../secret-03', 'a&status=revoked']:
            page.evaluate('(key) => { location.hash = "#/credentials/" + encodeURIComponent(key); }', key)
            page.wait_for_url(lambda url: parse_qs(urlsplit(url).query).get('record_key') == [key])
            assert page.url.startswith(base + '/console/credentials?')
            assert page.locator('#inspector-title').inner_text() == 'Credential unavailable'
            assert page.locator('[name=status]').input_value() == 'active'
            page.evaluate("location.hash=''")
            page.wait_for_url(lambda url: 'record_key' not in parse_qs(urlsplit(url).query))
        assert not errors, errors
        print(f'PASS width={width}: panes, proportions/scroll, Back/Forward, explicit Back, filters/focus/selection, fragment routing, missing record')
        context.close()
    # Presentation storage failure must leave native authenticated routes usable.
    for mode in ['corrupt-storage', 'denied-storage', 'no-script']:
        context = browser.new_context(viewport={"width": 390, "height": 900}, java_script_enabled=mode != 'no-script')
        context.route("**/*", lambda route: route.continue_() if route.request.url.startswith(base + "/") else route.abort())
        if mode == 'corrupt-storage':
            context.add_init_script('''
                const key = 'aegis.console.collection.v1:/console/credentials?q=provider&status=active#/credentials';
                sessionStorage.setItem(key, '{broken');
                sessionStorage.setItem(key + ':detail', '{broken');
            ''')
        elif mode == 'denied-storage':
            context.add_init_script('''
                Storage.prototype.getItem = function() { throw new Error('storage denied'); };
                Storage.prototype.setItem = function() { throw new Error('storage denied'); };
            ''')
        page = context.new_page()
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(base + '/console/credentials?q=provider&status=active#/credentials')
        page.locator('#record-secret-03').click()
        page.wait_for_url(lambda url: 'record_key=secret-03' in url)
        expect(page.locator('#inspector-title')).to_have_text('provider/test')
        expect(page.locator('#credential-inventory')).not_to_be_visible()
        page.locator('#close-inspector').click()
        expect(page.locator('#credential-inventory')).to_be_visible()
        assert page.locator('[name=q]').input_value() == 'provider'
        assert page.locator('[name=status]').input_value() == 'active'
        assert not errors, errors
        print(f'PASS fallback={mode}: native selection, narrow Back, filters, no page errors')
        context.close()
    browser.close()
