#!/usr/bin/env python3
"""Read-only Chrome inspection of two real fixture runs, synthetic runtime only.

Invoked by TestInstalledOnlineReusableDoerDirectAdmission with
AEGIS_QUEUE_RESULT_BROWSER_TEST=1. Uses the existing stdlib CDP driver; no
Playwright, production state or provider access. Only login mutates browser state.
"""
import json
import os
import pathlib
import subprocess
import tempfile
import time
from urllib.parse import quote

from console_browser_test import (DevTools, chrome_environment, click, navigate,
                                 page_websocket, require, stop_chrome,
                                 wait_for, wait_for_active_port)


def main():
    origin = os.environ["AEGIS_CONSOLE_BASE"]
    records = json.loads(pathlib.Path(os.environ["AEGIS_QUEUE_RESULT_RECORDS"]).read_text())
    require(len(records) == 2, "two exact run records required")
    with tempfile.TemporaryDirectory(prefix="aegis-qr-", dir=str(pathlib.Path.home())) as root:
        home = pathlib.Path(root) / "chrome"
        home.mkdir(mode=0o700)
        with open(pathlib.Path(root) / "stderr", "w") as errors:
            process = subprocess.Popen([
                "/usr/bin/google-chrome", "--headless=new", "--incognito", "--disable-gpu",
                "--no-first-run", "--no-default-browser-check", "--remote-debugging-port=0",
                "--remote-allow-origins=http://localhost", f"--user-data-dir={home}",
                origin + "/console/queue"], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                stderr=errors, env=chrome_environment(), start_new_session=True)
            dev = None
            try:
                deadline = time.monotonic() + 25
                port = wait_for_active_port(home / "DevToolsActivePort", deadline, process)
                dev = DevTools(page_websocket(port, deadline, process))
                dev.command("Page.enable")
                dev.command("Network.enable")
                dev.command("Runtime.enable")
                wait_for(dev, "!!document.querySelector('#session-form')", "real login form")
                require(not dev.evaluate("!!document.querySelector('[data-run-result-content]')"), "unauthenticated content leak")
                click(dev, "#password")
                dev.command("Input.insertText", {"text": os.environ["AEGIS_CONSOLE_PASSWORD"]})
                click(dev, "#session-form button[type=submit]")
                wait_for(dev, "!!document.querySelector('#logout')", "authenticated principal")
                start = len(dev.events)
                for record in records:
                    item = record["queue_item"]["id"]
                    navigate(dev, origin + "/console/queue?record_key=" + quote(item) + "#/queue/" + quote(item))
                    wait_for(dev, "document.readyState === 'complete' && document.querySelector('[data-run-result-content]')?.textContent === " + json.dumps(record["result"]["content"]), "exact stored result")
                    actual = dev.evaluate("({task: document.querySelector('[data-run-task]').textContent, text: document.querySelector('[data-queue-run-result]').textContent, contentChildren: document.querySelector('[data-run-result-content]').children.length})")
                    require(actual["task"] == record["task"] and actual["contentChildren"] == 0, "task/content text rendering changed")
                    for value in (item, record["queue_item"]["digest"], record["source"]["digest"], record["graph_run_id"], record["result"]["contract_digest"], record["result"]["artifact"]["id"], record["result"]["file_ref"], record["result"]["attempt_id"], "verified", "succeeded", "unavailable", record["elapsed"]["source"]):
                        require(value in actual["text"], "exact result fact omitted from DOM")
                mutations = [e for e in dev.events[start:] if e.get("method") == "Network.requestWillBeSent" and e.get("params", {}).get("request", {}).get("method") not in ("GET", "HEAD")]
                require(not mutations, "read-only result inspection issued a mutation")
                csp = [e["params"]["response"]["headers"] for e in dev.events if e.get("method") == "Network.responseReceived" and "/console/queue?" in e.get("params", {}).get("response", {}).get("url", "")]
                require(bool(csp) and all(any(k.lower() == "content-security-policy" for k in h) for h in csp), "production CSP missing")
                print(json.dumps({"status": "passed", "records": len(records), "runtime": "synthetic", "browser": "authenticated Chrome", "inspection": "read-only", "csp": "present"}))
            finally:
                stop_chrome(process, dev)


if __name__ == "__main__":
    main()
