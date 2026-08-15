#!/usr/bin/env python3
"""Pinned, dependency-free console security/accessibility source contract."""

from __future__ import annotations

import json
import pathlib
import sys

HARNESS_NAME = "aegis-console-security"
HARNESS_VERSION = "2.1.0"
MINIMUM_PYTHON = (3, 11)
ROOT = pathlib.Path(__file__).resolve().parents[1]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def main() -> int:
    require(sys.version_info >= MINIMUM_PYTHON, "Python 3.11+ is required")
    component = (ROOT / "web/console/components.templ").read_text(encoding="utf-8")
    model = (ROOT / "web/console/model.go").read_text(encoding="utf-8")
    css = (ROOT / "web/console/app.css").read_text(encoding="utf-8")
    embed = (ROOT / "web/console/embed.go").read_text(encoding="utf-8")
    console = (ROOT / "internal/console/server.go").read_text(encoding="utf-8")
    handlers = (ROOT / "internal/api/console.go").read_text(encoding="utf-8")
    api = (ROOT / "internal/api/server.go").read_text(encoding="utf-8")

    for landmark in ("<nav", "<main", "<aside", "<form"):
        require(landmark in component, f"missing accessible shell landmark: {landmark}")
    for identifier in ("workspace", "authentication-status", "service-status", "inspector", "close-inspector"):
        require(f'id="{identifier}"' in component, f"missing focus/status primitive: {identifier}")
    for state in ("loading", "empty", "unavailable", "error"):
        require(f'data-state="{state}"' in component or f'case "{state}"' in component, f"service state missing: {state}")
    require("prefers-reduced-motion" in css and "@media(max-width:700px)" in css, "responsive/reduced-motion CSS missing")

    first_party_active_source = "\n".join((component, model, handlers))
    for forbidden in (
        "templ.Raw", "SafeURL", "SafeCSS", "ExecuteScript", "innerHTML", "outerHTML",
        "document.write", "eval(", "new Function", "localStorage", "sessionStorage", "<script>",
    ):
        require(forbidden not in first_party_active_source, f"unsafe active-content primitive present: {forbidden}")
    require("https://" not in component and "http://" not in component, "external URL present in console template")
    require("<script" not in component and "data-on:" not in component and "data-bind:" not in component, "console template still requires CSP-blocked script execution")
    for native_control in (
        'method="post" action="/console/session"',
        'method="post" action="/console/logout"',
        'href={ fmt.Sprintf("/console?domain=%s"',
    ):
        require(native_control in component, f"native console interaction missing: {native_control}")
    require("vendor/datastar-v1.0.2.js" in embed and "go:embed" in embed, "Datastar bundle is not embedded")
    require("go:generate go run github.com/a-h/templ/cmd/templ@v0.3.1020" in model, "templ generator is not exactly pinned")

    for control in ("DisallowUnknownFields", "maxConsolePatchBytes", "Context", "text/event-stream", "consoleDomain"):
        require(control in handlers or control in api, f"bounded typed console control missing: {control}")
    for header in ("Content-Security-Policy", "frame-ancestors 'none'", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control", "SameSiteStrictMode", "HttpOnly"):
        require(header in console, f"server security control missing: {header}")
    for route in (
        '"/console"', '"/console/session"', '"/console/api/state"',
        '"/console/fragments/surface"', '"/console/fragments/inspect"',
        '"/console/assets/datastar-v1.0.2.js"', '"/v1"',
    ):
        require(route in api, f"console route missing: {route}")
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
            "browser_storage_denied": "pass",
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
