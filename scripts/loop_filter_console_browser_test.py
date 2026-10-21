#!/usr/bin/env python3
"""Issue 334: authenticated built-candidate navigation and durable restart.
Only isolated fixture state is used; no Loop mutation or execution is requested.
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


def collection(page, state, query=""):
    expect(page.locator('select[name="availability"]')).to_have_value(state)
    expect(page.locator('input[name="q"]')).to_have_value(query)
    keys = page.locator('a[data-detail-link][data-record-key]').evaluate_all(
        "links => links.map(link => link.dataset.recordKey)"
    )
    expected = ["filter-archived:1", "filter-archived:2"] if state == "archived" else ["filter-published:1"]
    assert sorted(keys) == expected, keys
    assert page.locator("[data-loop-workspace]").count() == 0


def login(page):
    page.goto(BASE + "/console/loops")
    expect(page.locator("#password")).to_be_visible()
    page.fill("#password", os.environ["AEGIS_CONSOLE_PASSWORD"])
    page.click("#session-form button[type=submit]")
    page.wait_for_url("**/console/agents**")


def pace(route):
    # Pace the actual network, including external CSP scripts, below the real
    # five-request/second source limiter. Never widen it or retry a 429.
    time.sleep(.23)
    if route.request.method != "GET" and not route.request.url.endswith("/console/login"):
        mutations.append((route.request.method, urlsplit(route.request.url).path))
    baseline = os.environ.get("AEGIS_FILTER_BASELINE_SCRIPT")
    if baseline and urlsplit(route.request.url).path.endswith("/navigation.js"):
        route.fulfill(path=baseline, content_type="text/javascript")
    else:
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
                    # The daemon is a new OS process with freshly reopened stores.
                    # Reauthenticate, then reconstruct the saved filtered URL.
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
                    # Actual native control: do not click Apply for JS navigation.
                    page.select_option('select[name="availability"]', "archived")
                    expect(page).to_have_url(re.compile(r"availability=archived"))
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
                    expect(page.get_by_role("button", name="Restore", exact=True)).to_be_visible()
                    page.locator("#close-inspector").click()
                    collection(page, "archived", "filter-")
                    assert "record_key" not in parse_qs(urlsplit(page.url).query)
                    # Exact detail is a replacement page (no collection controls).
                    # Return from historical detail, then change the filter: the
                    # GET form retains q, not selection/revision/page.
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
assert hashlib.sha256(BINARY.read_bytes()).hexdigest() == IDENTITY
(ROOT / "acceptance.json").write_text(json.dumps({
    "binary_sha256": IDENTITY, "generations": 2, "mixed_availability": True,
    "archived_successor_revision": 2, "query_detail_reload": True,
    "no_js_apply": True, "non_login_mutations": mutations,
    "browser_errors": errors, "http_errors": bad_responses,
}, indent=2))
print("PASS: actual candidate serve twice; authenticated Published/Archived native selection, mixed logical IDs and archived successor, query/detail/close, stale selection clearing, reload, durable restart and no-JS Apply; no lifecycle/runtime mutations")
