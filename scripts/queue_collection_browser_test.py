#!/usr/bin/env python3
"""Real principal-authenticated HTTP/CSP/Chrome proof with synthetic lifecycle
read-boundary fixtures, not runtime/provider execution. Never retain credentials.
"""
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time

import console_browser_test as driver

STATES = ["all", "queued", "claimable", "active", "retrying", "cancelled",
          "expired", "denied", "failed", "exhausted", "succeeded"]
EMPTY = os.environ.get("AEGIS_QUEUE_SCOPE_EMPTY") == "1"


def snapshot(browser):
    return browser.evaluate("""(() => {
      const band = id => {
        const root = document.querySelector('section[aria-labelledby="'+id+'"]');
        if (!root) return null;
        return {ids:[...root.querySelectorAll('a.op-row')].map(x=>x.id.slice(7)).sort(),
                count:root.querySelector('.count')?.textContent.trim(),
                links:[...root.querySelectorAll('a.op-row')].map(x=>x.getAttribute('href'))};
      };
      return {active:band('active-executions'),failed:band('failed-executions'),
              lifecycle:band('queue-lifecycle'),
              state:new URL(location.href).searchParams.get('state')||'all',
              selected:document.querySelector('section[aria-labelledby="queue-lifecycle"] a.seg.active')?.textContent.trim(),
              emptyMessage:document.querySelector('[data-state="empty"] strong')?.textContent.trim()||'',
              url:location.href};
    })()""")


def verify(browser, state, all_ids):
    driver.wait_for(browser, "document.readyState === 'complete' && !!document.querySelector('#queue-lifecycle') && (new URL(location.href).searchParams.get('state')||'all') === " + json.dumps(state), "Queue lifecycle " + state)
    value = snapshot(browser)
    for key in ("active", "failed"):
        expected = [f"scope-{key}-1", f"scope-{key}-2"]
        driver.require(value[key]["ids"] == expected and value[key]["count"] == "2 executions", key + " band changed with lifecycle " + state)
        for link in value[key]["links"]:
            driver.require("state=" not in link and "page=" not in link, "summary-band link retained lifecycle-table context")
    driver.require(value["selected"].lower() == state, "selected lifecycle readback differs")
    ids = value["lifecycle"]["ids"]
    if EMPTY and state == "succeeded":
        expected = []
        driver.require(value["emptyMessage"] == "No executions in this lifecycle view", "empty lifecycle message absent")
    elif state == "all":
        expected = all_ids
    else:
        expected = [f"scope-{state}-1", f"scope-{state}-2"]
        if state == "claimable":
            expected.append("agent-history-item")
    driver.require(ids == sorted(expected), "wrong lifecycle records for " + state + ": " + repr(ids))
    driver.require(value["lifecycle"]["count"] == f"{len(expected)} executions", "wrong bottom total for " + state)
    return value


def main():
    origin = os.environ["AEGIS_CONSOLE_BASE"]
    password = os.environ["AEGIS_CONSOLE_PASSWORD"]
    root = Path(__file__).resolve().parents[1]
    artifacts = root / (".issue327-browser-proof-empty-v2" if EMPTY else ".issue327-browser-proof-v2")
    artifacts.mkdir(mode=0o700, exist_ok=True)
    requests, checked, details = [], [], []
    with tempfile.TemporaryDirectory(prefix="chrome-", dir=artifacts) as profile:
        with (artifacts / "chrome.stderr").open("w") as diagnostics:
            chrome = subprocess.Popen([
                "/usr/bin/google-chrome", "--headless=new", "--incognito",
                "--disable-gpu", "--no-first-run", "--no-default-browser-check",
                "--remote-debugging-port=0", "--remote-allow-origins=http://localhost",
                "--user-data-dir=" + profile, origin + "/console"],
                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                stderr=diagnostics, env=driver.chrome_environment(), start_new_session=True)
            browser = None
            try:
                port = driver.wait_for_active_port(Path(profile) / "DevToolsActivePort", time.monotonic() + driver.CHROME_START_TIMEOUT, chrome)
                browser = driver.DevTools(driver.page_websocket(port, time.monotonic() + driver.PAGE_TARGET_TIMEOUT, chrome))
                for domain in ("Page", "Runtime", "Log", "Network", "Audits"):
                    browser.command(domain + ".enable")
                driver.navigate(browser, origin + "/console")
                driver.wait_for(browser, "!!document.querySelector('#session-form')", "principal login")
                driver.insert_text(browser, "#password", password)
                driver.click(browser, "#session-form button[type=submit]")
                driver.wait_for(browser, "document.readyState === 'complete' && !!document.querySelector('#logout')", "authenticated collection")
                time.sleep(1)
                driver.navigate(browser, origin + "/console/queue#/queue")
                driver.wait_for(browser, "!!document.querySelector('#queue-lifecycle')", "Queue collection")
                baseline = snapshot(browser)["lifecycle"]["ids"]
                driver.require(len(baseline) == 21, "unexpected baseline fixture")
                event_start = len(browser.events)
                history = []
                for state in (["succeeded"] if EMPTY else STATES):
                    time.sleep(1)  # Respect production rate limiting.
                    selector = 'section[aria-labelledby="queue-lifecycle"] a.seg'
                    selector += ':not([href*="state="])' if state == "all" else '[href*="state=' + state + '"]'
                    if state == "queued":
                        browser.evaluate("document.querySelector(" + json.dumps(selector) + ").focus()")
                        driver.require(browser.evaluate("document.activeElement.matches(" + json.dumps(selector) + ")"), "lifecycle keyboard focus failed")
                        for event_type in ("rawKeyDown", "keyUp"):
                            browser.command("Input.dispatchKeyEvent", {"type": event_type, "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13})
                    else:
                        driver.click(browser, selector)
                    checked.append({"phase": "keyboard" if state == "queued" else "click", **verify(browser, state, baseline)})
                    history.append(state)
                    time.sleep(1)
                    driver.refresh_registration_document(browser)
                    checked.append({"phase": "reload-new-document", **verify(browser, state, baseline)})
                for state in reversed(history[:-1]):
                    time.sleep(1)
                    browser.evaluate("history.back()")
                    checked.append({"phase": "back", **verify(browser, state, baseline)})
                for state in history[1:]:
                    time.sleep(1)
                    browser.evaluate("history.forward()")
                    checked.append({"phase": "forward", **verify(browser, state, baseline)})
                requests.extend(e["params"]["request"]["method"] for e in browser.events[event_start:] if e.get("method") == "Network.requestWillBeSent")
                # Follow all four summary links from incompatible lifecycle context.
                for key, index in ((key, index) for key in ("active", "failed") for index in range(2)):
                    time.sleep(1)
                    driver.navigate(browser, origin + "/console/queue?state=succeeded&limit=1&page=" + ("1" if EMPTY else "2") + "#/queue")
                    driver.wait_for(browser, "!!document.querySelector('#queue-lifecycle')", "filtered paginated collection")
                    value = snapshot(browser)
                    link = value[key]["links"][index]
                    expected_id = f"scope-{key}-{index+1}"
                    time.sleep(1)
                    driver.navigate(browser, origin + link)
                    driver.wait_for(browser, "!!document.querySelector('[data-queue-workspace]')", "exact summary-band detail")
                    actual_id = browser.evaluate("document.querySelector('#inspector-title').textContent.trim()")
                    driver.require(actual_id == expected_id, "wrong exact summary-band detail identity")
                    details.append({"expected": expected_id, "actual": actual_id})
                driver.require(bool(requests) and all(method == "GET" for method in requests), "filter controls issued a mutation request")
                violations = [e for e in browser.events if e.get("method") == "Audits.issueAdded" and e.get("params", {}).get("issue", {}).get("code") == "ContentSecurityPolicyIssue"]
                driver.require(not violations, "CSP violation during lifecycle navigation")
                (artifacts / "results.json").write_text(json.dumps({"states": checked, "empty_fixture": EMPTY, "band_detail_links": details, "read_only_filter_requests": len(requests), "csp": "pass", "evidence_tier": "real authenticated HTTP/CSP/Chrome with synthetic lifecycle read fixtures; not runtime/provider execution"}, indent=2) + "\n")
                print("PASS: lifecycle counts/identities, new-document reload, native history/keyboard and exact band details; empty_fixture=" + str(EMPTY))
            finally:
                if browser is not None:
                    browser.close()
                if chrome.poll() is None:
                    os.killpg(chrome.pid, signal.SIGTERM)
                chrome.wait(timeout=15)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
