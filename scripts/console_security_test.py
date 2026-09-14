#!/usr/bin/env python3
"""Pinned, dependency-free console security/accessibility source contract."""

from __future__ import annotations

import hashlib
import json
import pathlib
import sys

HARNESS_NAME = "aegis-console-security"
HARNESS_VERSION = "2.3.0"
MINIMUM_PYTHON = (3, 11)
ROOT = pathlib.Path(__file__).resolve().parents[1]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def bounded_navigation_source(source: str) -> str:
    """Remove only exact reviewed fragment adapters, rejecting any drift.

    These hashes pin source, not runtime authorization. Changes require review
    of the adapter and its dynamic navigation tests before updating the pin.
    Presentation-only code outside the adapters remains subject to the normal
    forbidden-primitive checks. Never derive expected hashes from input source.
    """
    require(source.count('const credentials = location.pathname === "/console/credentials";') == 1,
            "credential fragment route guard changed")
    adapters = (
        ('  const resolveGraph = () => {',
         '  window.addEventListener("hashchange", resolveGraph);',
         '1163f088a34a655360fdf67ea175498ffbb1de4aa85600b173b51648ec3e0e43'),
        ('  const resolveAgentFragment = () => {',
         '  addEventListener("hashchange", resolveAgentFragment);',
         '1ec3ec5d56958b6ef381f80fc312da98af06c8cadac03a2dd0fd7a6875402002'),
        ('  const reconcileFragment = () => {',
         '    } else reconcileFragment();\n  });',
         '52e5555338b3150491220ddde404a1ac36be9df597acdd62669aef97553657fa'),
    )
    remainder = source
    for start, end, digest in adapters:
        require(remainder.count(start) == 1 and remainder.count(end) == 1,
                "fragment adapter missing or duplicated")
        begin = remainder.index(start)
        finish = remainder.index(end) + len(end)
        require(finish > begin, "fragment adapter boundaries reordered")
        block = remainder[begin:finish]
        require(hashlib.sha256(block.encode("utf-8")).hexdigest() == digest,
                "reviewed fragment adapter changed")
        remainder = remainder[:begin] + remainder[finish:]
    for forbidden in ("location.hash", "location.replace"):
        require(forbidden not in remainder,
                f"unreviewed fragment primitive: {forbidden}")
    return remainder


def main() -> int:
    require(sys.version_info >= MINIMUM_PYTHON, "Python 3.11+ is required")
    component = (ROOT / "web/console/components.templ").read_text(encoding="utf-8")
    model = (ROOT / "web/console/model.go").read_text(encoding="utf-8")
    css = (ROOT / "web/console/app.css").read_text(encoding="utf-8")
    embed = (ROOT / "web/console/embed.go").read_text(encoding="utf-8")
    navigation = (ROOT / "web/console/navigation.js").read_text(encoding="utf-8")
    console = (ROOT / "internal/console/server.go").read_text(encoding="utf-8")
    handlers = (ROOT / "internal/api/console.go").read_text(encoding="utf-8")
    api = (ROOT / "internal/api/server.go").read_text(encoding="utf-8")

    for landmark in ("<nav", "<main", "<aside", "<form"):
        require(landmark in component, f"missing accessible shell landmark: {landmark}")
    for identifier in ("workspace", "authentication-status", "service-status", "inspector", "close-inspector"):
        require(f'id="{identifier}"' in component, f"missing focus/status primitive: {identifier}")
    for state in ("loading", "empty", "unavailable", "error"):
        require(f'data-state="{state}"' in component or f'case "{state}"' in component, f"service state missing: {state}")
    require("prefers-reduced-motion" in css and "@media(max-width:768px)" in css, "responsive/reduced-motion CSS missing")

    first_party_active_source = "\n".join((component, model, handlers))
    for forbidden in (
        "templ.Raw", "SafeURL", "SafeCSS", "ExecuteScript", "innerHTML", "outerHTML",
        "document.write", "eval(", "new Function", "localStorage", "sessionStorage", "<script>",
    ):
        require(forbidden not in first_party_active_source, f"unsafe active-content primitive present: {forbidden}")
    require("https://" not in component and "http://" not in component, "external URL present in console template")
    navigation_tag = '<script src="/console/assets/navigation.js" defer></script>'
    require(component.count("<script") == 1 and component.count(navigation_tag) == 1,
            "console template must load exactly the bounded same-origin navigation asset")
    require(
        'if model.Authenticated {\n\t\t\t\t' + navigation_tag in component,
        "navigation enhancement must be emitted only for an authenticated console",
    )
    require("data-on:" not in component and "data-bind:" not in component,
            "console template still requires inline script execution")

    bounded_navigation_source(navigation)
    for forbidden in (
        "fetch(", "XMLHttpRequest", "WebSocket", "EventSource", "sendBeacon", "localStorage",
        "document.cookie", "location.search", "URLSearchParams", "innerHTML",
        "outerHTML", "document.write", "eval(", "new Function", "window.open", "form.submit",
    ):
        require(forbidden not in navigation, f"navigation asset exceeds presentation-only authority: {forbidden}")
    # Fragments may select only an Agent identifier; authentication and exact
    # revision resolution remain server-side. Dynamic cases live alongside this
    # source contract in scripts/agent_navigation_test.cjs.
    for required in (
        'location.hash.match(/^#\\/agents\\/([^/?#]+)$/)',
        '/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/.test(id)',
        'target.pathname = "/console/agents"',
        'target.searchParams.set("record_key", id)',
        'if (current !== id) target.searchParams.delete("revision")',
    ):
        require(required in navigation, f"bounded Agent fragment control missing: {required}")
    for required in (
        "sessionStorage.setItem(storageKey", "sessionStorage.getItem(storageKey)",
        'body.dataset.detailOpen === "true"', "context.recordKey.length > 1024",
        'link.dataset.recordKey === context.recordKey', "selected.focus({preventScroll: true})",
    ):
        require(required in navigation, f"bounded navigation restoration control missing: {required}")
    require(
        "Number.isFinite(value) && value >= 0 && value <= 10000000" in navigation,
        "navigation viewport restoration is not bounded",
    )
    for native_control in (
        'method="post" action="/console/login"',
        'method="post" action="/console/password"',
        'method="post" action="/console/logout"',
        'path := fmt.Sprintf("/console/%s", domain)',
    ):
        require(native_control in component, f"native console interaction missing: {native_control}")
    require("vendor/datastar-v1.0.2.js" in embed and "go:embed" in embed, "Datastar bundle is not embedded")
    require("go:generate go run github.com/a-h/templ/cmd/templ@v0.3.1020" in model, "templ generator is not exactly pinned")

    for control in ("DisallowUnknownFields", "maxConsolePatchBytes", "Context", "text/event-stream", "consoleDomain"):
        require(control in handlers or control in api, f"bounded typed console control missing: {control}")
    for header in ("Content-Security-Policy", "frame-ancestors 'none'", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control", "SameSiteStrictMode", "HttpOnly"):
        require(header in console, f"server security control missing: {header}")
    for route in (
        '"/console"', '"/console/login"', '"/console/password"', '"/console/session"', '"/console/api/state"',
        '"/console/fragments/surface"', '"/console/fragments/inspect"',
        '"/console/assets/datastar-v1.0.2.js"', '"/console/assets/navigation.js"', '"/v1"',
    ):
        require(route in api, f"console route missing: {route}")
    require("script-src 'self'" in console, "CSP does not authorize the same-origin navigation asset")
    for weakening in ("'unsafe-inline'", "'unsafe-eval'", "script-src *", "script-src http:", "script-src https:"):
        require(weakening not in console, f"CSP script policy was weakened: {weakening}")
    require("Authorization" not in component and "Authorization" not in model, "reusable API bearer reached browser source")
    require(not (ROOT / "web/console/app.js").exists(), "imperative console controller remains")
    require(not (ROOT / "web/console/index.html").exists(), "static console document remains")

    evidence = {
        "harness": HARNESS_NAME,
        "version": HARNESS_VERSION,
        "python": f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}",
        "checks": {
            "typed_templ_landmarks_and_states": "pass",
            "unsafe_active_content_denied": "pass",
            "native_interactions_need_no_script_execution": "pass",
            "same_origin_navigation_asset_exactly_scoped": "pass",
            "presentation_storage_bounded_non_authoritative": "pass",
            "strict_bounded_signal_and_sse_contract": "pass",
            "security_header_and_cookie_source_contract": "pass",
            "responsive_and_reduced_motion": "pass",
            "reusable_bearer_absent_from_browser_source": "pass",
            "imperative_renderer_removed": "pass",
        },
        "runtime_contract": "exercised by web/console and internal/api focused Go tests",
    }
    print(json.dumps(evidence, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as error:
        print(json.dumps({"harness": HARNESS_NAME, "version": HARNESS_VERSION, "error": str(error)}), file=sys.stderr)
        raise SystemExit(1)
