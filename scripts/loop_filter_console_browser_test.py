#!/usr/bin/env python3
"""Issue 334: actual root candidate, private state, native GET navigation.
The Go fixture supplies candidate/root argv and protected synthetic login input.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import time
from urllib.parse import parse_qs, urlsplit

from playwright.sync_api import expect, sync_playwright

BINARY, ROOT = map(Path, sys.argv[1:3])
CONFIG = ROOT / "owner.json"
BASE = json.loads(CONFIG.read_text())["api"]["console"]["origin"]
IDENTITY = hashlib.sha256(BINARY.read_bytes()).hexdigest()
ENV = {"PATH": os.environ["PATH"], "HOME": str(Path.home()),
       "GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"}
errors, bad_responses, mutations = [], [], []
lifecycle_posts = []
lifecycle_phase = False


def collection(page, state, query=""):
    expect(page.locator('select[name="availability"]')).to_have_value(state)
    expect(page.locator('input[name="q"]')).to_have_value(query)
    keys = page.locator('a[data-detail-link][data-record-key]').evaluate_all(
        "links => links.map(link => link.dataset.recordKey)"
    )
    expected = (["filter-archived:1", "filter-archived:2"]
                if state == "archived" else ["filter-published:1"])
    assert keys == expected, keys
    assert page.locator("[data-loop-workspace]").count() == 0
    for key in expected:
        row = page.locator("#surface-list li").filter(
            has=page.locator(f'a[data-detail-link][data-record-key="{key}"]')
        )
        expect(row.get_by_role("button", name="Restore" if state == "archived" else "Archive", exact=True)).to_be_visible()
        assert row.locator("a form, a button, form a, form form").count() == 0


def login(page):
    page.goto(BASE + "/console/loops")
    expect(page.locator("#password")).to_be_visible()
    page.fill("#password", os.environ["AEGIS_CONSOLE_PASSWORD"])
    page.click("#session-form button[type=submit]")
    page.wait_for_url("**/console/agents**")


def pace(route):
    # Pace real requests below the source limiter; never widen/retry a 429.
    time.sleep(.23)
    if route.request.method != "GET" and not (
        route.request.method == "POST" and
        urlsplit(route.request.url).path in ("/console/login", "/console/session")
    ):
        request = route.request
        target = urlsplit(request.url)
        if lifecycle_phase:
            values = parse_qs(request.post_data or "")
            lifecycle_posts.append((request.method, target.path,
                                    parse_qs(target.query).get("loop_id"),
                                    values.get("state"), values.get("revision")))
        else:
            mutations.append((request.method, target.path))
    route.continue_()


def start(log):
    daemon = subprocess.Popen([str(BINARY), "--config", str(CONFIG), "serve"],
                              cwd=ROOT, env=ENV, stdout=log, stderr=log)
    target = urlsplit(BASE)
    deadline = time.monotonic() + 15
    while True:
        assert daemon.poll() is None, f"serve exited {daemon.returncode}; see {log.name}"
        try:
            with socket.create_connection((target.hostname, target.port), timeout=.2):
                return daemon
        except OSError:
            if time.monotonic() >= deadline:
                daemon.terminate()
                daemon.wait(timeout=15)
                raise AssertionError("candidate readiness timeout")
            time.sleep(.1)


with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    archived_url = ""
    for generation in range(2):
        with (ROOT / f"serve-{generation}.log").open("wb") as log:
            daemon = start(log)
            try:
                context = browser.new_context()
                context.route("**/*", pace)
                page = context.new_page()
                page.on("pageerror", lambda error: errors.append(str(error)))
                page.on("response", lambda response: bad_responses.append(
                    (response.status, urlsplit(response.url).path)) if response.status >= 400 else None)
                login(page)
                if generation == 1:
                    page.goto(archived_url)
                    collection(page, "archived", "filter-")
                    page.reload()
                    collection(page, "archived", "filter-")
                    page.select_option('select[name="availability"]', "published")
                    expect(page).to_have_url(re.compile(r"availability=published"))
                    collection(page, "published", "filter-")
                else:
                    page.goto(BASE + "/console/loops")
                    collection(page, "published")
                    page.select_option('select[name="availability"]', "archived")
                    expect(page).to_have_url(re.compile(r"availability=archived"))
                    collection(page, "archived")
                    page.go_back()
                    collection(page, "published")
                    page.go_forward()
                    collection(page, "archived")
                    page.reload()
                    collection(page, "archived")
                    page.locator('input[name="q"]').fill("filter-")
                    page.get_by_role("button", name="Apply", exact=True).click()
                    collection(page, "archived", "filter-")
                    archived_url = page.url
                    page.locator('a[data-record-key="filter-archived:2"]').click()
                    expect(page.locator("[data-loop-workspace]")).to_be_visible()
                    assert parse_qs(urlsplit(page.url).query)["availability"] == ["archived"]
                    expect(page.locator("[data-loop-workspace]").get_by_role("button", name="Restore", exact=True)).to_be_visible()
                    page.locator("#close-inspector").click()
                    collection(page, "archived", "filter-")
                    assert "record_key" not in parse_qs(urlsplit(page.url).query)
                    page.goto(archived_url + "&record_key=filter-archived%3A1&revision=1&page=1")
                    expect(page.locator("[data-loop-workspace]")).to_be_visible()
                    page.locator("#close-inspector").click()
                    collection(page, "archived", "filter-")
                    page.select_option('select[name="availability"]', "published")
                    expect(page).to_have_url(re.compile(r"availability=published"))
                    collection(page, "published", "filter-")
                    params = parse_qs(urlsplit(page.url).query)
                    assert not {"record_key", "revision", "page"}.intersection(params), params
                    page.reload()
                    collection(page, "published", "filter-")
                    page.select_option('select[name="availability"]', "archived")
                    expect(page).to_have_url(re.compile(r"availability=archived"))
                    collection(page, "archived", "filter-")
                    state = context.storage_state()
                    assert not mutations, mutations
                    # Real row clicks: restoring r1 must also restore its r2
                    # successor, because availability is logical-Loop scoped.
                    before = page.locator('#surface-list form').evaluate_all(
                        "forms => forms.map(form => [form.querySelector('[name=revision]').value, form.querySelector('[name=digest]').value])")
                    lifecycle_phase = True
                    row = page.locator("#surface-list li").filter(
                        has=page.locator('a[data-record-key="filter-archived:1"]'))
                    row.get_by_role("button", name="Restore", exact=True).click()
                    expect(page.locator("[data-loop-workspace]")).to_be_visible()
                    page.locator("#close-inspector").click()
                    keys = page.locator('a[data-detail-link][data-record-key]').evaluate_all(
                        "links => links.map(link => link.dataset.recordKey)")
                    assert sorted(keys) == ["filter-archived:1", "filter-archived:2", "filter-published:1"], keys
                    row = page.locator("#surface-list li").filter(
                        has=page.locator('a[data-record-key="filter-archived:2"]'))
                    row.get_by_role("button", name="Archive", exact=True).click()
                    expect(page.locator("[data-loop-workspace]")).to_be_visible()
                    page.locator("#close-inspector").click()
                    lifecycle_phase = False
                    collection(page, "archived")
                    after = page.locator('#surface-list form').evaluate_all(
                        "forms => forms.map(form => [form.querySelector('[name=revision]').value, form.querySelector('[name=digest]').value])")
                    assert after == before, (before, after)
                    # Original discoverability restored; history remains append-only.
                    page.goto(archived_url)
                    collection(page, "archived", "filter-")
                    context.close()
                    context = browser.new_context(storage_state=state, java_script_enabled=False)
                    context.route("**/*", pace)
                    page = context.new_page()
                    page.goto(BASE + "/console/loops")
                    page.select_option('select[name="availability"]', "archived")
                    page.get_by_role("button", name="Apply", exact=True).click()
                    expect(page).to_have_url(re.compile(r"availability=archived"))
                    collection(page, "archived")
                context.close()
            finally:
                daemon.terminate()
                try:
                    status = daemon.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    daemon.kill()
                    daemon.wait()
                    raise AssertionError("candidate did not terminate cleanly")
                assert status == 0, f"candidate shutdown exit {status}"
    browser.close()
assert not errors, errors
assert not bad_responses, bad_responses
assert not mutations, mutations
assert lifecycle_posts == [
    ("POST", "/console/loops/availability", ["filter-archived"], ["published"], ["2"]),
    ("POST", "/console/loops/availability", ["filter-archived"], ["archived"], ["2"]),
], lifecycle_posts
assert hashlib.sha256(BINARY.read_bytes()).hexdigest() == IDENTITY
(ROOT / "acceptance.json").write_text(json.dumps({
    "binary_sha256": IDENTITY, "generations": 2, "mixed_availability": True,
    "archived_successor_revision": 2, "query_detail_reload": True,
    "back_forward": True, "row_controls_sibling_to_links": True,
    "no_js_apply": True, "non_login_mutations": mutations,
    "lifecycle_posts": lifecycle_posts, "row_click_roundtrip": True,
    "browser_errors": errors, "http_errors": bad_responses,
}, indent=2))
print("PASS: actual candidate serve twice; authenticated Published/Archived native selection, back/forward, mixed logical IDs and archived successor, query/detail/close, stale selection clearing, reload, durable restart and no-JS Apply; row Restore/Archive clicks affect both logical revisions without changing definitions; exactly two lifecycle POSTs, zero navigation/runtime mutations")
