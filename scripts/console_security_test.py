#!/usr/bin/env python3
"""Pinned, dependency-free console security/accessibility contract harness."""

from __future__ import annotations

import json
import pathlib
import sys
from html.parser import HTMLParser

HARNESS_NAME = "aegis-console-security"
HARNESS_VERSION = "1.0.0"
MINIMUM_PYTHON = (3, 11)
ROOT = pathlib.Path(__file__).resolve().parents[1]


class ConsoleHTMLParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.tags: set[str] = set()
        self.ids: set[str] = set()
        self.states: set[str] = set()
        self.inline_executable: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        self.tags.add(tag)
        values = dict(attrs)
        if identifier := values.get("id"):
            self.ids.add(identifier)
        if state := values.get("data-state"):
            self.states.add(state)
        if tag == "script" and not values.get("src"):
            self.inline_executable.append("inline-script")
        if tag == "style":
            self.inline_executable.append("inline-style")
        if any(name.lower().startswith("on") for name, _ in attrs):
            self.inline_executable.append("inline-event-handler")


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def main() -> int:
    require(sys.version_info >= MINIMUM_PYTHON, "Python 3.11+ is required")
    html = (ROOT / "web/console/index.html").read_text(encoding="utf-8")
    css = (ROOT / "web/console/app.css").read_text(encoding="utf-8")
    javascript = (ROOT / "web/console/app.js").read_text(encoding="utf-8")
    server = (ROOT / "internal/console/server.go").read_text(encoding="utf-8")
    api = (ROOT / "internal/api/server.go").read_text(encoding="utf-8")

    parser = ConsoleHTMLParser()
    parser.feed(html)
    require({"nav", "main", "aside", "form"}.issubset(parser.tags), "missing accessible shell landmark")
    require({"workspace", "authentication-status", "service-status", "inspector", "close-inspector"}.issubset(parser.ids), "missing focus/status primitive")
    require({"loading", "empty", "unavailable", "error"}.issubset(parser.states), "service states are incomplete")
    require(not parser.inline_executable, f"inline executable content: {parser.inline_executable}")
    require("prefers-reduced-motion" in css and "@media(max-width:700px)" in css, "responsive/reduced-motion CSS missing")
    for forbidden in ("innerHTML", "outerHTML", "document.write", "eval(", "localStorage", "sessionStorage"):
        require(forbidden not in javascript, f"unsafe browser API present: {forbidden}")
    for control in ("textContent", "Escape", "X-CSRF-Token", "credentials: 'same-origin'"):
        require(control in javascript, f"browser control missing: {control}")
    for header in ("Content-Security-Policy", "frame-ancestors 'none'", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control", "SameSiteStrictMode", "HttpOnly"):
        require(header in server, f"server security control missing: {header}")
    for route in ('"/console"', '"/console/session"', '"/console/api/state"', '"/v1"'):
        require(route in api, f"console route missing: {route}")
    require("Authorization" not in html and "Authorization" not in javascript, "reusable API bearer reached browser assets")

    evidence = {
        "harness": HARNESS_NAME,
        "version": HARNESS_VERSION,
        "python": f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}",
        "checks": {
            "accessibility_landmarks_and_states": "pass",
            "inline_execution_denied": "pass",
            "safe_text_rendering": "pass",
            "browser_storage_denied": "pass",
            "csrf_and_same_origin_client_contract": "pass",
            "security_header_and_cookie_source_contract": "pass",
            "responsive_and_reduced_motion": "pass",
            "reusable_bearer_absent_from_assets": "pass",
        },
        "network_contract": "exercised by internal/api TestConsoleAuthenticatedSessionCSRFHeadersAndPagination",
    }
    print(json.dumps(evidence, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as error:
        print(json.dumps({"harness": HARNESS_NAME, "version": HARNESS_VERSION, "error": str(error)}), file=sys.stderr)
        raise SystemExit(1)
