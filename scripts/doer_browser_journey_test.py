#!/usr/bin/env python3
"""Real Chrome guided Doer BLOCKER proof; never native consent or live inference.

Called by the installed-console driver after its unchanged inventory checks.
The installed fixture's fake Hermes transport is synthetic, not human/operator
acceptance. Only rendered native forms and CDP keyboard/pointer input mutate UI.
Retained evidence deliberately excludes CSRF, cookies and fixture passwords.
"""
from __future__ import annotations

import base64
import json
import pathlib
import time
from urllib.parse import urlencode

BLOCKER = "session_selection_zero_authorized_matches"
OWNED = ("draft_id", "draft_version", "loop_id", "revision", "previous_digest", "publication_key")
INPUTS = ("publisher_id", "task", "workspace", "writable_files", "verify_file", "assert_text", "expected_text", "max_attempts")


def values(browser, selector, names):
    return browser.evaluate("(() => { const f=document.querySelector(" + json.dumps(selector) + "); return Object.fromEntries(" + json.dumps(list(names)) + ".map(n => [n,f?.elements.namedItem(n)?.value ?? null])); })()")


def facts(browser, selector):
    return browser.evaluate("Object.fromEntries([...document.querySelectorAll(" + json.dumps(selector + " dt") + ")].map(n=>[n.textContent.trim(),n.nextElementSibling?.textContent.trim()]))")


def validate_blocked_receipt(receipt, expected, require):
    require(receipt["outcome"] == "blocked", "Run did not return an authoritative blocker")
    require(receipt["facts"].get("Reason") == BLOCKER, "Run blocker differs from reviewed readiness")
    require(bool(receipt["facts"].get("Operation")), "Run did not reserve a request identity")
    require(receipt["form"] == expected, "Run key or immutable reference changed")
    require(receipt["queueLinks"] == 0, "blocked Run advertised an executable Queue result")


def receipt(browser):
    return {"outcome": browser.evaluate("document.querySelector('.operation-receipt')?.dataset.outcome"),
            "facts": facts(browser, ".operation-receipt"),
            "form": values(browser, 'form[action^="/console/loops/run?"]', ("revision", "digest", "idempotency_key")),
            "queueLinks": browser.evaluate("document.querySelectorAll('.operation-receipt a[href*=\"/console/queue\"]').length")}


def capture(browser, proof, phase, require, failures):
    """Keep full-page reviewable PNGs plus accessibility/overflow witnesses."""
    reports = []
    for width, height in ((1440, 1000), (390, 844)):
        browser.command("Emulation.setDeviceMetricsOverride", {"width": width, "height": height, "deviceScaleFactor": 1, "mobile": width == 390})
        browser.evaluate("scrollTo(0,0)")
        time.sleep(0.08)
        geometry = browser.evaluate("""(() => ({width: innerWidth, height: innerHeight,
          scrollWidth: document.documentElement.scrollWidth,
          controls: [...document.querySelectorAll('main input:not([type=hidden]), main textarea, main select, main button, main a')]
            .filter(n=>n.getBoundingClientRect().width>0).map(n=>({tag:n.tagName,name:n.name||'',
              label:n.labels?.[0]?.textContent.trim()||n.getAttribute('aria-label')||n.textContent.trim(),
              width:n.getBoundingClientRect().width,height:n.getBoundingClientRect().height}))}))()""")
        # Aggregate failures only after retaining every requested screenshot.
        # A visual defect does not prevent exercising later security invariants,
        # but still makes the entire proof fail at its final assertion.
        if geometry["width"] != width or geometry["scrollWidth"] > width:
            failures.append(f"{phase}-{width}: viewport overflow: layout width {geometry['width']}, scroll width {geometry['scrollWidth']}")
        if not geometry["controls"] or not all(c["label"] and c["width"] > 0 and c["height"] > 0 for c in geometry["controls"]):
            failures.append(phase + ": unnamed or zero-sized visible control")
        ax = browser.command("Accessibility.getFullAXTree")["nodes"]
        # Live status regions may legally have no accessible name; their
        # announced text is a separate requirement. Interactive controls and
        # headings must retain names. Do not equate status text with a label.
        names = [n.get("name", {}).get("value", "") for n in ax if not n.get("ignored") and n.get("role", {}).get("value") in ("heading", "button", "link", "textbox", "combobox", "spinbutton", "region")]
        require(bool(names) and all(names), phase + ": accessible controls missing names")
        status_text = browser.evaluate("[...document.querySelectorAll('main [role=status]')].filter(n=>n.getClientRects().length).map(n=>n.textContent.trim())")
        require(all(status_text), phase + ": visible status region has no announced text")
        metrics = browser.command("Page.getLayoutMetrics")["cssContentSize"]
        require(0 < metrics["height"] <= 24000, "screenshot height exceeds proof bound")
        png = base64.b64decode(browser.command("Page.captureScreenshot", {"format": "png", "captureBeyondViewport": True, "clip": {"x": 0, "y": 0, "width": width, "height": metrics["height"], "scale": 1}})["data"])
        require(png.startswith(b"\x89PNG\r\n\x1a\n") and len(png) > 1024, "missing real Chrome screenshot")
        target = proof / f"{phase}-{width}.png"
        target.write_bytes(png)
        reports.append({"path": str(target.resolve()), "url": target.resolve().as_uri(), "geometry": geometry, "accessible_names": names})
    browser.command("Emulation.setDeviceMetricsOverride", {"width": 1440, "height": 1000, "deviceScaleFactor": 1, "mobile": False})
    return reports


def run_journey(browser, origin, workspace, driver):
    proof = workspace.parent / (workspace.name + "-doer-journey")
    proof.mkdir(mode=0o700, exist_ok=True)
    report = {"schema_version": 1, "browser": "Google Chrome", "status": "running",
              "acceptance_scope": "isolated guided authoring/publication/explicit Run blocker only",
              "synthetic_provider_transport": True, "live_provider_acceptance": False,
              "human_operator_approval": False, "native_approval_invoked": False,
              "captures": {}, "failures": []}
    require = driver.require
    form = 'form[action="/console/loops/doer/preview"]'
    run_form = 'form[action^="/console/loops/run?"]'

    def settled(expression, description):
        driver.wait_for(browser, "document.readyState === 'complete' && (" + expression + ")", description)
        time.sleep(1.0)  # Preserve the source limiter, do not change production policy.

    def return_draft():
        driver.click(browser, 'a[href^="/console/loops/doer?draft_id="]')
        settled("!!document.querySelector('textarea[name=task]')", "same retained Doer draft")
        return values(browser, form, OWNED + INPUTS)

    def preview():
        driver.click(browser, form + ' button[type=submit]')
        settled("!!document.querySelector('[aria-label=\"Doer authoring and execution readiness\"]')", "Doer candidate readiness review")

    try:
        driver.navigate(browser, origin + "/console/loops/doer")
        settled("!!document.querySelector('textarea[name=task]')", "guided Doer composer")
        initial = values(browser, form, OWNED)
        require(all(initial[n] for n in ("loop_id", "revision", "publication_key")), "server-owned candidate identity missing")
        # Native select keyboard input; no JS value assignment or synthetic change.
        driver.click(browser, 'select[name="publisher_id"]')
        browser.command("Input.dispatchKeyEvent", {"type": "keyDown", "key": "p", "text": "p"})
        browser.command("Input.dispatchKeyEvent", {"type": "keyUp", "key": "p"})
        driver.key(browser, "Enter")
        require(values(browser, form, ("publisher_id",))["publisher_id"] == "proof-agent", "native Agent selection failed")
        disposable = workspace / "doer-workspace"
        require(disposable.is_dir() and not (disposable / "result.txt").exists(), "expected empty existing disposable workspace")
        expected_inputs = {"publisher_id": "proof-agent", "task": "Create result.txt containing exactly hello (five raw UTF-8 bytes, no newline).",
                           "workspace": str(disposable), "writable_files": "result.txt", "verify_file": "result.txt",
                           "assert_text": "bytes", "expected_text": "hello", "max_attempts": "2"}
        for name in ("task", "workspace", "writable_files", "verify_file", "expected_text", "max_attempts"):
            driver.replace_text(browser, '[name="' + name + '"]', expected_inputs[name])
        require(values(browser, form, INPUTS) == expected_inputs, "Doer operator inputs differ")
        require(values(browser, form, OWNED) == initial, "authoring changed server-owned refs/keys")
        report["captures"]["builder"] = capture(browser, proof, "builder", require, report["failures"])
        preview()
        review = facts(browser, ".exact-reference")
        readiness = browser.evaluate("document.querySelector('[aria-label=\"Doer authoring and execution readiness\"]').innerText")
        require("Can author: true" in readiness and "Can request execution: false" in readiness and BLOCKER in readiness and "not_checked" in readiness, "known readiness blocker not rendered before publication")
        report["readiness"] = readiness
        report["candidate"] = review
        report["captures"]["readiness"] = capture(browser, proof, "readiness", require, report["failures"])
        contract_review = facts(browser, '[aria-label="Exact Doer contract review"]')
        report["contract_review"] = contract_review
        retained = return_draft()
        require({n: retained[n] for n in INPUTS} == expected_inputs, "return lost operator inputs")
        require(all(retained[n] == initial[n] for n in ("loop_id", "revision", "previous_digest")), "return changed immutable candidate reference")
        # The first save converts the form's creation nonce to a server-owned
        # durable publication key. Thereafter it must match the exact review and
        # survive every unchanged preview/setup visit; never overwrite it.
        require(retained["publication_key"] == contract_review["Publication idempotency key"], "retained key differs from reviewed publication")
        report["creation_nonce"] = initial["publication_key"]
        require(retained["draft_id"] and retained["draft_version"], "retained draft missing durable version")
        preview()
        require(facts(browser, ".exact-reference")["Target digest"] == review["Target digest"], "unchanged draft changed candidate digest")
        driver.click(browser, 'a[href^="/console/loops/doer/setup?draft_id="]')
        settled("!!document.querySelector('[aria-label=\"Contextual Doer setup\"]')", "contextual independent Doer setup")
        setup = browser.evaluate("document.querySelector('[aria-label=\"Contextual Doer setup\"]').innerText")
        actions = browser.evaluate("[...document.querySelectorAll('main form input[name=action]')].map(n=>n.value)")
        require({"review", "host-review"}.issubset(set(actions)), "independent continuation/host review controls missing")
        require("No setup action publishes a Loop or creates a Run intent" in setup and "Independent host-write decision" in setup and "hello" in setup, "contextual setup lost independent contract")
        report["setup"] = {"text": setup, "available_review_actions": actions, "decisions_submitted": 0}
        report["captures"]["setup"] = capture(browser, proof, "setup", require, report["failures"])
        require(return_draft() == retained, "setup changed retained draft/version/inputs")
        preview()
        require(facts(browser, ".exact-reference")["Target digest"] == review["Target digest"], "setup changed candidate digest")
        report["retained_draft"] = retained
        driver.click(browser, 'form[action="/console/loops/execute"] button[type=submit]')
        settled("!!document.querySelector('#published-doer-run')", "explicit immutable Doer publication")
        published = values(browser, run_form, ("revision", "digest", "idempotency_key"))
        require(published["digest"] == contract_review["Loop digest"] and published["revision"] == retained["revision"] and bool(published["idempotency_key"]), "publication returned different immutable candidate")
        report["published"] = published
        driver.click(browser, run_form + ' button[type=submit]')
        settled("document.querySelector('.operation-receipt')?.dataset.outcome === 'blocked'", "explicit Run authoritative blocker")
        first = receipt(browser)
        validate_blocked_receipt(first, published, require)
        require("required_action: review_charter_successor_with_matching_authentication" in browser.evaluate("document.querySelector('.operation-receipt').innerText"), "blocker missing explicit next action")
        report["captures"]["outcome"] = capture(browser, proof, "outcome", require, report["failures"])
        driver.click(browser, run_form + ' button[type=submit]')
        settled("document.querySelector('.operation-receipt')?.dataset.outcome === 'blocked'", "same-key blocked request replay")
        replay = receipt(browser)
        validate_blocked_receipt(replay, published, require)
        require(replay == first, "replay changed reserved request identity")
        query = urlencode({"record_key": retained["loop_id"] + ":" + retained["revision"], "revision": retained["revision"]})
        driver.navigate(browser, origin + "/console/loops?" + query)
        settled("!!document.querySelector('[data-loop-workspace]')", "exact published Doer topology")
        require(values(browser, run_form, ("revision", "digest", "idempotency_key")) == published, "inspector duplicated or changed Run key")
        fixture = next(f for f in json.loads((workspace / "loop-geometry-manifest.json").read_text()) if f["loop_id"] == "proof-doer-cycle")
        for width in (1440, 390):
            browser.command("Emulation.setDeviceMetricsOverride", {"width": width, "height": 1000 if width == 1440 else 844, "deviceScaleFactor": 1, "mobile": width == 390})
            driver.measure_loop_geometry(browser, workspace, "guided-doer-" + str(width), fixture["steps"], fixture["transitions"], published["digest"], width, fixture["feedback"], fixture["conditions"])
        report["captures"]["published-topology"] = capture(browser, proof, "published-topology", require, report["failures"])
        browser.command("Page.reload", {"ignoreCache": True})
        settled("!!document.querySelector('[data-loop-workspace]')", "reloaded published Doer inspector")
        require(values(browser, run_form, ("revision", "digest", "idempotency_key")) == published, "reload created another Run key")
        driver.click(browser, run_form + ' button[type=submit]')
        settled("document.querySelector('.operation-receipt')?.dataset.outcome === 'blocked'", "inspector exact-key Run replay")
        inspector_replay = receipt(browser)
        validate_blocked_receipt(inspector_replay, published, require)
        require(inspector_replay == first, "inspector replay duplicated reserved request")
        require(not (disposable / "result.txt").exists(), "blocked Run unexpectedly wrote task output")
        report["runs"] = {"first": first, "receipt_replay": replay, "inspector_replay": inspector_replay, "unique_reserved_requests": 1, "unique_run_keys": 1, "executable_success": False}
        require(not report["failures"], "Doer visual/accessibility proof failed: " + "; ".join(report["failures"]))
        report["status"] = "pass"
    except Exception as error:
        report["status"] = "failed"
        report["failures"].append(str(error))
        try:
            report["captures"]["failure"] = capture(browser, proof, "failure", require, report["failures"])
        except Exception as capture_error:
            report["failures"].append("failure capture: " + str(capture_error))
        raise
    finally:
        (proof / "proof.json").write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
        browser.command("Emulation.clearDeviceMetricsOverride")
    return {"doer_guided_blocker": report["status"], "proof": str((proof / "proof.json").resolve()), "live_operator_acceptance": False}
