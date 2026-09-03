#!/usr/bin/env python3
"""Exercise the installed Aegis console in a real CSP-enforcing Chrome process."""

from __future__ import annotations

import base64
import hashlib
import http.client
import json
import pathlib
import secrets
import socket
import struct
import subprocess
import sys
import time
from typing import Any, Protocol


class ProcessState(Protocol):
    def poll(self) -> int | None: ...


CHROME_START_TIMEOUT = 15
PAGE_TARGET_TIMEOUT = 8


def require(condition: bool, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


class DevTools:
    def __init__(self, websocket_url: str) -> None:
        require(websocket_url.startswith("ws://"), "Chrome exposed an unsupported DevTools URL")
        authority, path = websocket_url[5:].split("/", 1)
        host, port_text = authority.rsplit(":", 1)
        self.sock = socket.create_connection((host, int(port_text)), timeout=5)
        key = base64.b64encode(secrets.token_bytes(16)).decode("ascii")
        request = (
            f"GET /{path} HTTP/1.1\r\nHost: {authority}\r\nUpgrade: websocket\r\n"
            f"Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n"
            "Origin: http://localhost\r\n\r\n"
        )
        self.sock.sendall(request.encode("ascii"))
        response = self._read_http_headers()
        expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")).digest()).decode("ascii")
        require(response.startswith("HTTP/1.1 101 "), "Chrome rejected the DevTools WebSocket")
        require(f"sec-websocket-accept: {expected}".lower() in response.lower(), "Chrome returned an invalid DevTools handshake")
        self.next_id = 1
        self.events: list[dict[str, Any]] = []

    def _read_http_headers(self) -> str:
        data = bytearray()
        while b"\r\n\r\n" not in data:
            chunk = self.sock.recv(4096)
            require(bool(chunk), "Chrome closed the DevTools handshake")
            data.extend(chunk)
        return data.decode("latin-1")

    def _frame(self, payload: bytes) -> bytes:
        mask = secrets.token_bytes(4)
        size = len(payload)
        header = bytearray([0x81])
        if size < 126:
            header.append(0x80 | size)
        elif size < 65536:
            header.append(0x80 | 126)
            header.extend(struct.pack("!H", size))
        else:
            header.append(0x80 | 127)
            header.extend(struct.pack("!Q", size))
        header.extend(mask)
        header.extend(byte ^ mask[index % 4] for index, byte in enumerate(payload))
        return bytes(header)

    def _recv_exact(self, size: int) -> bytes:
        data = bytearray()
        while len(data) < size:
            chunk = self.sock.recv(size - len(data))
            require(bool(chunk), "Chrome closed the DevTools connection")
            data.extend(chunk)
        return bytes(data)

    def receive(self) -> dict[str, Any]:
        while True:
            first, second = self._recv_exact(2)
            opcode = first & 0x0F
            size = second & 0x7F
            if size == 126:
                size = struct.unpack("!H", self._recv_exact(2))[0]
            elif size == 127:
                size = struct.unpack("!Q", self._recv_exact(8))[0]
            mask = self._recv_exact(4) if second & 0x80 else b""
            payload = self._recv_exact(size)
            if mask:
                payload = bytes(byte ^ mask[index % 4] for index, byte in enumerate(payload))
            if opcode == 0x8:
                raise RuntimeError("Chrome closed the DevTools connection")
            if opcode == 0x9:
                self.sock.sendall(bytes([0x8A, len(payload)]) + payload)
                continue
            if opcode == 0x1:
                return json.loads(payload)

    def command(self, method: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
        identifier = self.next_id
        self.next_id += 1
        message: dict[str, Any] = {"id": identifier, "method": method}
        if params is not None:
            message["params"] = params
        self.sock.sendall(self._frame(json.dumps(message, separators=(",", ":")).encode("utf-8")))
        while True:
            received = self.receive()
            if received.get("id") == identifier:
                require("error" not in received, f"DevTools command {method} failed")
                return received.get("result", {})
            self.events.append(received)

    def evaluate(self, expression: str) -> Any:
        result = self.command("Runtime.evaluate", {"expression": expression, "returnByValue": True, "awaitPromise": True})
        value = result.get("result", {})
        exception = result.get("exceptionDetails")
        require(exception is None, f"browser evaluation raised an exception: {exception}")
        return value.get("value")

    def close(self) -> None:
        self.sock.close()


def page_websocket(port: int, deadline: float, process: ProcessState) -> str:
    while time.monotonic() < deadline:
        require(process.poll() is None, "Chrome exited before exposing a debuggable console page")
        try:
            connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
            connection.request("GET", "/json/list")
            response = connection.getresponse()
            payload = json.loads(response.read())
            connection.close()
            pages = [target for target in payload if target.get("type") == "page"]
            if pages:
                return pages[0]["webSocketDebuggerUrl"]
        except (OSError, ValueError, KeyError, json.JSONDecodeError):
            pass
        time.sleep(0.05)
    raise RuntimeError("Chrome did not expose a debuggable console page")


_BROWSER_PROOF_START = time.monotonic()


def console_auth_requests(events: list[dict[str, Any]]) -> list[dict[str, str]]:
    """Return sanitized authentication transitions without form bodies or secrets."""
    auth_paths = ("/console/login", "/console/session", "/console/logout", "/console/password")
    requests: list[dict[str, str]] = []
    for event in events:
        if event.get("method") != "Network.requestWillBeSent":
            continue
        params = event.get("params", {})
        request = params.get("request", {})
        url = str(request.get("url", ""))
        if not url.endswith(auth_paths):
            continue
        requests.append({
            "url": url,
            "method": str(request.get("method", "")),
            "type": str(params.get("type", "")),
        })
    return requests


def require_authenticated_console(devtools: DevTools, description: str) -> None:
    state = devtools.evaluate(
        "({authenticated: !!document.querySelector('#logout'), login: !!document.querySelector('#session-form'), "
        "path: location.pathname + location.search, ready: document.readyState})"
    )
    require(
        state == {"authenticated": True, "login": False, "path": "/console/agents", "ready": "complete"},
        f"browser lost authentication {description}; state={state}; auth_requests={console_auth_requests(devtools.events)}",
    )


def navigate(devtools: DevTools, url: str) -> None:
    result = devtools.command("Page.navigate", {"url": url})
    require(not result.get("errorText"), f"browser navigation failed: {result.get('errorText')}")


def wait_for(
    devtools: DevTools,
    expression: str,
    description: str,
    timeout: float = 15,
    *,
    diagnostic_event_start: int = 0,
) -> None:
    print(f"elapsed={time.monotonic() - _BROWSER_PROOF_START:.2f}s wait: {description}", file=sys.stderr, flush=True)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            if devtools.evaluate(expression):
                return
        except (OSError, RuntimeError):
            pass
        time.sleep(0.05)
    state: Any = None
    try:
        state = devtools.evaluate("({path: location.pathname + location.search, ready: document.readyState, title: document.querySelector('#surface-title')?.textContent || '', auth: document.querySelector('#authentication-status')?.textContent.trim() || '', active: document.activeElement?.id || document.activeElement?.tagName || '', modal: document.querySelector(':modal')?.id || '', body: document.body?.innerText.slice(0, 1200) || ''})")
    except (OSError, RuntimeError):
        state = "unavailable"
    requests = console_auth_requests(devtools.events[diagnostic_event_start:])
    document_requests = sum(
        1
        for event in devtools.events[diagnostic_event_start:]
        if event.get("method") == "Network.requestWillBeSent"
        and event.get("params", {}).get("type") == "Document"
    )
    raise RuntimeError(
        f"browser did not reach {description}; elapsed={time.monotonic() - _BROWSER_PROOF_START:.2f}s "
        f"timeout={timeout}s document_requests={document_requests}; state={state}; requests={requests}"
    )


def click(devtools: DevTools, selector: str) -> None:
    present = devtools.evaluate(
        "(() => { const node = document.querySelector(" + json.dumps(selector) + ");"
        "if (!node) return false; node.scrollIntoView({behavior: 'instant', block: 'center', inline: 'center'}); return true; })()"
    )
    require(present is True, f"browser control missing: {selector}")
    # Measure only after the browser has applied the scroll. A single evaluation
    # can retain the pre-scroll rectangle while the hit test uses post-scroll
    # layout, producing a false obscured-control result for lower inspector rows.
    time.sleep(0.05)
    point = devtools.evaluate(
        "(() => { const node = document.querySelector(" + json.dumps(selector) + ");"
        "if (!node) return null; const box = node.getBoundingClientRect();"
        "const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);"
        "return {x: box.left + box.width / 2, y: box.top + box.height / 2, width: box.width, height: box.height, hit: hit?.id || hit?.tagName || '', target: hit === node || node.contains(hit)}; })()"
    )
    require(isinstance(point, dict), f"browser control missing after scroll: {selector}")
    require(point["width"] > 0 and point["height"] > 0, f"browser control is not visible: {selector}; state={point}")
    require(bool(point["target"]), f"browser control is obscured: {selector}; state={point}")
    for event_type in ("mousePressed", "mouseReleased"):
        devtools.command("Input.dispatchMouseEvent", {
            "type": event_type,
            "x": point["x"],
            "y": point["y"],
            "button": "left",
            "clickCount": 1,
        })


def insert_text(devtools: DevTools, selector: str, text: str) -> None:
    """Enter text through Chrome and require focus and exact DOM retention."""
    click(devtools, selector)
    wait_for(
        devtools,
        "document.activeElement === document.querySelector(" + json.dumps(selector) + ")",
        f"focused browser control {selector}",
    )
    devtools.command("Input.insertText", {"text": text})
    retained = devtools.evaluate(
        "(() => { const node = document.querySelector(" + json.dumps(selector) + ");"
        "return !!node && document.activeElement === node && node.value === " + json.dumps(text) + "; })()"
    )
    require(retained is True, f"browser control did not retain text: {selector}")


def replace_text(devtools: DevTools, selector: str, text: str) -> None:
    """Replace an existing native form value using keyboard selection."""
    click(devtools, selector)
    for event_type in ("rawKeyDown", "keyUp"):
        devtools.command("Input.dispatchKeyEvent", {
            "type": event_type, "key": "a", "code": "KeyA", "modifiers": 2,
            "windowsVirtualKeyCode": 65, "nativeVirtualKeyCode": 65,
        })
    devtools.command("Input.insertText", {"text": text})
    retained = devtools.evaluate(
        "document.querySelector(" + json.dumps(selector) + ")?.value === " + json.dumps(text)
    )
    require(retained is True, f"browser control did not replace text: {selector}")


def tap(devtools: DevTools, selector: str) -> None:
    """Send real browser touch events for controls under mobile emulation."""
    point = devtools.evaluate(
        "(() => { const node = document.querySelector(" + json.dumps(selector) + ");"
        "if (!node) return null; node.scrollIntoView({block: 'center', inline: 'center'}); const box = node.getBoundingClientRect();"
        "const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);"
        "return {x: box.left + box.width / 2, y: box.top + box.height / 2, width: box.width, height: box.height, target: hit === node || node.contains(hit)}; })()"
    )
    require(isinstance(point, dict), f"browser control missing: {selector}")
    require(point["width"] > 0 and point["height"] > 0, f"browser control is not visible: {selector}; state={point}")
    require(bool(point["target"]), f"browser control is obscured: {selector}; state={point}")
    devtools.command("Input.dispatchTouchEvent", {
        "type": "touchStart",
        "touchPoints": [{"x": point["x"], "y": point["y"], "radiusX": 1, "radiusY": 1, "force": 1}],
    })
    devtools.command("Input.dispatchTouchEvent", {"type": "touchEnd", "touchPoints": []})


def key(devtools: DevTools, key_name: str, *, shift: bool = False) -> None:
    """Send real browser key events instead of calling DOM handlers directly."""
    modifiers = 8 if shift else 0
    virtual_key = {"Tab": 9, "Escape": 27}.get(key_name)
    require(virtual_key is not None, f"browser proof does not define a native key code for {key_name}")
    for event_type in ("rawKeyDown", "keyUp"):
        devtools.command("Input.dispatchKeyEvent", {
            "type": event_type,
            "key": key_name,
            "code": key_name,
            "modifiers": modifiers,
            "windowsVirtualKeyCode": virtual_key,
            "nativeVirtualKeyCode": virtual_key,
        })


def main() -> int:
    if len(sys.argv) not in (4, 7):
        raise RuntimeError("usage: console_browser_test.py ORIGIN PASSWORD_FILE WORKSPACE [register CHARTER_JSON CURRENT_FLEET_JSON]")
    origin = sys.argv[1].rstrip("/")
    password_path = pathlib.Path(sys.argv[2])
    workspace = pathlib.Path(sys.argv[3])
    passwords = json.loads(password_path.read_text(encoding="utf-8"))
    initial_password = passwords.get("initial")
    replacement_password = passwords.get("replacement")
    registration_only = len(sys.argv) == 7 and sys.argv[4] == "register"
    if len(sys.argv) == 7 and not registration_only:
        raise RuntimeError("unsupported installed-console browser phase")
    require(isinstance(initial_password, str) and len(initial_password) >= 12, "browser proof received no initial password")
    require(isinstance(replacement_password, str) and len(replacement_password) >= 12, "browser proof received no replacement password")
    chrome_home = workspace / ("chrome-registration" if registration_only else "chrome-inspection")
    chrome_home.mkdir(mode=0o700)
    chrome_stderr_path = workspace / "chrome.stderr"
    chrome_stderr = chrome_stderr_path.open("w", encoding="utf-8")
    process = subprocess.Popen(
        [
            "/usr/bin/google-chrome",
            "--headless=new",
            "--incognito",
            "--disable-gpu",
            "--no-first-run",
            "--no-default-browser-check",
            "--remote-debugging-port=0",
            "--remote-allow-origins=http://localhost",
            f"--user-data-dir={chrome_home}",
            origin + "/console",
        ],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=chrome_stderr,
        text=True,
    )
    devtools: DevTools | None = None
    try:
        active_port = chrome_home / "DevToolsActivePort"
        startup_deadline = time.monotonic() + CHROME_START_TIMEOUT
        while time.monotonic() < startup_deadline and not active_port.exists():
            require(process.poll() is None, "Chrome exited before DevTools readiness")
            time.sleep(0.05)
        require(active_port.exists(), "Chrome did not become ready")
        require(process.poll() is None, "Chrome exited after DevTools readiness")
        port = int(active_port.read_text(encoding="utf-8").splitlines()[0])
        # DevToolsActivePort can appear before Chrome's first page target is
        # queryable. This stage needs a fresh budget, not startup's remainder.
        page_deadline = time.monotonic() + PAGE_TARGET_TIMEOUT
        devtools = DevTools(page_websocket(port, page_deadline, process))
        for domain in ("Page", "Runtime", "Log", "Network", "Audits"):
            devtools.command(domain + ".enable")

        navigate(devtools, origin + "/console")
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form')", "password login page")
        insert_text(devtools, "#password", initial_password)
        click(devtools, "#session-form button[type=submit]")
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#logout') && document.querySelector('#surface-title')?.textContent.trim() === 'Agent Registry'", "authenticated Agent Registry")
        time.sleep(0.5)

        if registration_only:
            charter_json = pathlib.Path(sys.argv[5]).read_text(encoding="utf-8")
            fixture_json = pathlib.Path(sys.argv[6]).read_text(encoding="utf-8")
            navigate(devtools, origin + "/console/agents/charter-import")
            wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#agent-registration-prepare')", "authenticated Agent registration form")
            insert_text(devtools, "#charter", charter_json)
            insert_text(devtools, "#fixture", fixture_json)
            insert_text(devtools, "#fleet-id", "proof-fleet")
            insert_text(devtools, "#source-id", "missing-proof-source")
            click(devtools, '#agent-registration-prepare button[type="submit"]')
            wait_for(devtools, "document.readyState === 'complete' && document.body.innerText.includes('Registration proposal denied') && !!document.querySelector('#agent-registration-prepare')", "invalid current-fleet source denied without registration")
            replace_text(devtools, "#source-id", "proof-source")
            click(devtools, '#agent-registration-prepare button[type="submit"]')
            wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#agent-registration-execute') && document.body.textContent.includes('Exact immutable registration proposal')", "exact Agent registration review")
            click(devtools, '#agent-registration-execute button[type="submit"]')
            wait_for(devtools, "document.readyState === 'complete' && document.body.innerText.includes('Registered Agent with authoritative exact revision readback') && !!document.querySelector('a[href*=\"record_key=proof-agent\"]')", "browser-authenticated Agent registration")
            print(json.dumps({"browser": "Google Chrome", "browser_authenticated_agent_registration": "pass", "invalid_source_denial": "pass", "credentials_used_for_fleet_workflow": False}, sort_keys=True))
            return 0

        expected = {
            "agents": "Agent Registry",
            "graphs": "Graphs",
            "loops": "Loops",
            "queue": "Execution Queue",
            "credentials": "Credentials",
        }
        for domain, title in expected.items():
            click(devtools, f'a[href="/console/{domain}#/{domain}"]')
            wait_for(devtools, "document.readyState === 'complete' && document.querySelector('#surface-title')?.textContent.trim() === " + json.dumps(title), title)
            # A native navigation fetches the document and retained assets.
            # Respect the deliberately coarse pre-auth source limiter so the
            # proof measures UI behavior instead of manufacturing a burst.
            time.sleep(1.0)

        click(devtools, 'a[href="/console/agents#/agents"]')
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#record-proof-agent')", "installed Agent Registry record")
        mutation_paths = ("/console/session", "/console/login", "/console/password")
        session_requests_before = sum(
            1
            for event in devtools.events
            if event.get("method") == "Network.requestWillBeSent"
            and event.get("params", {}).get("request", {}).get("url", "").endswith(mutation_paths)
        )
        click(devtools, 'a[href="/console/agents/charter-import"]')
        wait_for(
            devtools,
            "location.pathname === '/console/agents/charter-import' && document.title === 'Agent registration · Aegis Console' && document.querySelector('#charter-import-title')?.textContent.trim() === 'Charter-backed Agent registration' && document.querySelector('#charter-import-review')?.textContent.includes('aegis charter validate') && document.querySelector('#charter-import-review')?.textContent.includes('aegis charter import')",
            "dedicated Agent registration page",
        )
        time.sleep(1.5)
        session_requests_after = sum(
            1
            for event in devtools.events
            if event.get("method") == "Network.requestWillBeSent"
            and event.get("params", {}).get("request", {}).get("url", "").endswith(mutation_paths)
        )
        require(session_requests_after == session_requests_before, "charter import review link triggered a session mutation request")
        click(devtools, "#charter-import-back")
        wait_for(devtools, "location.pathname === '/console/agents' && document.readyState === 'complete' && !!document.querySelector('#record-proof-agent')", "native back link to Agent Registry")
        time.sleep(0.5)
        devtools.command("Page.navigate", {"url": origin + "/console/agents?record_key=proof-agent&revision=1#/agents"})
        wait_for(devtools, "document.readyState === 'complete' && !document.querySelector('#inspector').hidden && document.querySelector('#inspector-fields').textContent.includes('proof-agent') && location.search.includes('revision=1')", "exact Agent Registry revision detail")

        # Traverse the installed immutable fleet-control chain only through the
        # product's rendered related-record links.
        click(devtools, '.related-records a[href^="/console/loops?record_key=proof-loop%3A1"]')
        wait_for(devtools, "location.pathname === '/console/loops' && location.search.includes('record_key=proof-loop%3A1') && document.querySelector('#inspector-title')?.textContent.trim() === 'proof-loop'", "Agent to exact Loop related record")
        time.sleep(0.5)
        devtools.command("Page.reload", {"ignoreCache": True})
        wait_for(devtools, "document.readyState === 'complete' && document.querySelector('#inspector-title')?.textContent.trim() === 'proof-loop'", "reloaded exact Loop canonical URL")
        time.sleep(0.5)
        click(devtools, '.related-records a[href^="/console/graphs?record_key=proof-graph%3A1"]')
        wait_for(devtools, "location.pathname === '/console/graphs' && document.querySelector('#inspector-title')?.textContent.trim() === 'proof-graph'", "Loop to exact Graph related record")
        time.sleep(0.5)
        click(devtools, '.related-records a[href^="/console/queue?record_key=queue-accepted"]')
        wait_for(devtools, "location.pathname === '/console/queue' && document.querySelector('#inspector-title')?.textContent.trim() === 'queue-accepted' && document.body.innerText.includes('artifact-accepted') && document.body.innerText.includes('disposition-accepted') && document.body.innerText.includes('evidence_satisfied')", "Graph to Queue evidence, receipt, and disposition chain")
        time.sleep(0.5)

        devtools.evaluate("history.back()")
        wait_for(devtools, "location.pathname === '/console/graphs' && document.querySelector('#inspector-title')?.textContent.trim() === 'proof-graph'", "browser Back restored exact Graph")
        time.sleep(0.5)
        devtools.evaluate("history.forward()")
        wait_for(devtools, "location.pathname === '/console/queue' && document.querySelector('#inspector-title')?.textContent.trim() === 'queue-accepted'", "browser Forward restored exact Queue record")
        time.sleep(0.5)

        devtools.command("Page.navigate", {"url": origin + "/console/loops?q=loop&lifecycle=draft&limit=1#/loops"})
        wait_for(devtools, "document.readyState === 'complete' && location.search.includes('q=loop') && location.search.includes('lifecycle=draft') && document.querySelectorAll('#surface-list a').length === 1 && !!document.querySelector('.pagination a')", "installed Loop search, lifecycle filter, and first bounded page")
        time.sleep(0.5)
        click(devtools, '.pagination a')
        wait_for(devtools, "document.readyState === 'complete' && location.search.includes('page=2') && location.search.includes('q=loop') && location.search.includes('lifecycle=draft') && document.querySelectorAll('#surface-list a').length === 1", "bounded pagination preserving Loop filters")
        time.sleep(0.5)

        devtools.command("Page.navigate", {"url": origin + "/console/agents?record_key=proof-agent&revision=1#/agents"})
        wait_for(devtools, "document.readyState === 'complete' && location.search.includes('revision=1') && document.querySelector('#inspector-fields')?.textContent.includes('1 @ sha256:')", "canonical direct load of exact Agent revision")
        time.sleep(0.5)
        devtools.command("Page.reload", {"ignoreCache": True})
        wait_for(devtools, "document.readyState === 'complete' && location.search.includes('revision=1') && document.querySelector('#inspector-title')?.textContent.trim() === 'proof-agent'", "reloaded exact Agent revision")
        click(devtools, "#close-inspector")
        wait_for(devtools, "document.readyState === 'complete' && document.querySelector('#inspector').hidden", "closed Agent Registry detail")

        # Exercise native declarative modal commands through real Chrome input.
        # This fixture has no credential, authority selector, mutation endpoint,
        # or executable product behavior.
        injected = devtools.evaluate("""(() => {
          const host = document.createElement('section');
          host.id = 'foundation-browser-fixture';
          host.style.cssText = 'position: fixed; inset: 0 auto auto 0; z-index: 1000';
          host.innerHTML = `
            <button id="open-fixture-dialog" type="button" commandfor="fixture-dialog" command="show-modal">Open dialog</button>
            <button id="open-fixture-drawer" type="button" commandfor="fixture-drawer" command="show-modal">Open drawer</button>
            <dialog id="fixture-dialog" class="interaction-overlay" aria-labelledby="fixture-dialog-title" aria-describedby="fixture-dialog-description" data-overlay-kind="dialog">
              <div class="interaction-dialog" tabindex="-1" data-overlay-panel>
                <header><h2 id="fixture-dialog-title" tabindex="0" autofocus>Confirm exact operation</h2><button id="fixture-dialog-close" type="button" commandfor="fixture-dialog" command="close">Close</button></header>
                <p id="fixture-dialog-description">Synthetic unresolved operation.</p>
                <div class="interaction-body"><button id="fixture-dialog-cancel" type="button" commandfor="fixture-dialog" command="close">Cancel</button><button id="fixture-dialog-submit" type="button">Submit</button></div>
              </div>
            </dialog>
            <dialog id="fixture-drawer" class="interaction-overlay interaction-drawer" aria-labelledby="fixture-drawer-title" data-overlay-kind="drawer">
              <div class="interaction-drawer-panel" tabindex="-1" data-overlay-panel>
                <header><h2 id="fixture-drawer-title" tabindex="0" autofocus>Exact record</h2><button id="fixture-drawer-close" type="button" commandfor="fixture-drawer" command="close">Close</button></header>
                <div class="interaction-body">
                  <section class="collection-state" data-state="loading" role="status"><strong>Loading</strong><p>Loading authoritative state.</p></section>
                  <section class="collection-state" data-state="denied" role="status"><strong>Denied</strong><p>No count is asserted.</p><code>read_denied</code></section>
                  <button id="fixture-drawer-last" type="button">Last action</button>
                </div>
              </div>
            </dialog>`;
          document.body.append(host);
          return !!document.querySelector('#fixture-dialog');
        })()""")
        require(injected is True, "Chrome could not install the synthetic interaction fixture")
        click(devtools, "#open-fixture-dialog")
        wait_for(devtools, "document.querySelector('#fixture-dialog').matches(':modal') && document.activeElement?.id === 'fixture-dialog-title'", "dialog initial focus")
        dialog_state = devtools.evaluate("(() => { const logout = document.querySelector('#logout'); logout.focus(); return {modal: document.querySelector('#fixture-dialog').matches(':modal'), backgroundInert: document.activeElement !== logout, labelledBy: document.querySelector('#fixture-dialog').getAttribute('aria-labelledby')}; })()")
        require(dialog_state == {"modal": True, "backgroundInert": True, "labelledBy": "fixture-dialog-title"}, f"dialog accessibility state was incomplete: {dialog_state}")
        key(devtools, "Tab")
        wait_for(devtools, "document.activeElement?.id === 'fixture-dialog-close'", "forward Tab into dialog")
        key(devtools, "Tab", shift=True)
        wait_for(devtools, "document.activeElement?.id === 'fixture-dialog-title'", "reverse-Tab containment")
        key(devtools, "Tab")
        wait_for(devtools, "document.activeElement?.id === 'fixture-dialog-close'", "forward-Tab remains deterministic after reverse navigation")
        key(devtools, "Escape")
        wait_for(devtools, "!document.querySelector('#fixture-dialog').open && document.activeElement?.id === 'open-fixture-dialog'", "dialog Escape close and focus restoration")

        devtools.command("Emulation.setDeviceMetricsOverride", {"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": True})
        tap(devtools, "#open-fixture-drawer")
        wait_for(devtools, "document.querySelector('#fixture-drawer').matches(':modal') && document.activeElement?.id === 'fixture-drawer-title'", "drawer initial focus")
        responsive_state = devtools.evaluate("(() => { const panel = document.querySelector('.interaction-drawer-panel').getBoundingClientRect(); const loading = getComputedStyle(document.querySelector('[data-state=loading]')).borderColor; const denied = getComputedStyle(document.querySelector('[data-state=denied]')).borderColor; return {panelWidth: panel.width, viewport: innerWidth, overflow: document.documentElement.scrollWidth > innerWidth, statesDiffer: loading !== denied}; })()")
        require(isinstance(responsive_state, dict) and responsive_state["panelWidth"] <= responsive_state["viewport"] and not responsive_state["overflow"], f"drawer clipped the narrow viewport: {responsive_state}")
        require(bool(responsive_state["statesDiffer"]), f"denied and loading visual states were not distinguishable: {responsive_state}")
        key(devtools, "Escape")
        wait_for(devtools, "!document.querySelector('#fixture-drawer').open && document.activeElement?.id === 'open-fixture-drawer'", "drawer Escape close and focus restoration")
        devtools.command("Emulation.clearDeviceMetricsOverride")

        cookies = devtools.command("Network.getCookies", {"urls": [origin + "/console"]}).get("cookies", [])
        old_session = next((cookie for cookie in cookies if cookie.get("name") == "aegis-console"), None)
        if not isinstance(old_session, dict):
            raise RuntimeError("browser proof did not capture the pre-rotation session")
        old_session_value = old_session.get("value")
        require(isinstance(old_session_value, str) and bool(old_session_value), "browser proof captured an empty pre-rotation session")
        rotation_requests_before = sum(
            1
            for event in devtools.events
            if event.get("method") == "Network.requestWillBeSent"
            and event.get("params", {}).get("request", {}).get("url", "").endswith("/console/password")
        )
        click(devtools, "#open-password-rotation")
        wait_for(devtools, "document.querySelector('#principal-password-rotation')?.matches(':modal') && document.activeElement?.id === 'principal-password-rotation-title'", "open password rotation dialog with initial focus")
        desktop_rotation = devtools.evaluate("(() => { const panel = document.querySelector('#principal-password-rotation [data-overlay-panel]').getBoundingClientRect(); return {panelWidth: panel.width, panelHeight: panel.height, viewportWidth: innerWidth, viewportHeight: innerHeight, overflow: document.documentElement.scrollWidth > innerWidth}; })()")
        require(isinstance(desktop_rotation, dict) and desktop_rotation["panelWidth"] <= desktop_rotation["viewportWidth"] and desktop_rotation["panelHeight"] <= desktop_rotation["viewportHeight"] and not desktop_rotation["overflow"], f"password rotation dialog clipped the desktop viewport: {desktop_rotation}")
        key(devtools, "Tab")
        wait_for(devtools, "document.activeElement?.getAttribute('aria-label') === 'Cancel password rotation'", "password rotation deterministic focus order")
        click(devtools, '#principal-password-rotation button[command="close"]')
        wait_for(devtools, "!document.querySelector('#principal-password-rotation').open && document.activeElement?.id === 'open-password-rotation'", "cancel password rotation without submission")
        rotation_requests_after_cancel = sum(
            1
            for event in devtools.events
            if event.get("method") == "Network.requestWillBeSent"
            and event.get("params", {}).get("request", {}).get("url", "").endswith("/console/password")
        )
        require(rotation_requests_after_cancel == rotation_requests_before, "canceling password rotation submitted the form")

        require_authenticated_console(devtools, "before narrow password rotation")
        narrow_transition_start = len(devtools.events)
        click(devtools, "#open-password-rotation")
        wait_for(
            devtools,
            "document.querySelector('#principal-password-rotation')?.matches(':modal')",
            "reopen password rotation dialog before narrow viewport",
            diagnostic_event_start=narrow_transition_start,
        )
        # Keep the authenticated native dialog open while narrowing the viewport.
        # Dispatching synthetic mouse or touch input after mobile emulation can
        # replace the document in headless Chrome instead of running the command.
        devtools.command("Emulation.setDeviceMetricsOverride", {"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": True})
        wait_for(
            devtools,
            "document.querySelector('#principal-password-rotation')?.matches(':modal')",
            "retain open password rotation dialog at narrow viewport",
            diagnostic_event_start=narrow_transition_start,
        )
        narrow_rotation = devtools.evaluate("(() => { const panel = document.querySelector('#principal-password-rotation [data-overlay-panel]'); const rect = panel.getBoundingClientRect(); return {left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, viewportWidth: innerWidth, viewportHeight: innerHeight, overflow: document.documentElement.scrollWidth > innerWidth, scrollable: panel.scrollHeight <= panel.clientHeight || getComputedStyle(panel).overflowY === 'auto'}; })()")
        require(isinstance(narrow_rotation, dict) and narrow_rotation["left"] >= 0 and narrow_rotation["right"] <= narrow_rotation["viewportWidth"] and narrow_rotation["top"] >= 0 and narrow_rotation["bottom"] <= narrow_rotation["viewportHeight"] and not narrow_rotation["overflow"] and narrow_rotation["scrollable"], f"password rotation dialog clipped the narrow viewport: {narrow_rotation}")
        key(devtools, "Escape")
        wait_for(devtools, "!document.querySelector('#principal-password-rotation').open && document.activeElement?.id === 'open-password-rotation'", "narrow password rotation Escape close and focus restoration")
        devtools.command("Emulation.clearDeviceMetricsOverride")

        click(devtools, "#open-password-rotation")
        wait_for(devtools, "document.querySelector('#principal-password-rotation')?.matches(':modal')", "reopen password rotation dialog")
        insert_text(devtools, "#current-password", initial_password)
        insert_text(devtools, "#new-password", replacement_password)
        insert_text(devtools, "#confirm-password", replacement_password)
        click(devtools, '#principal-password-rotation input[name="approve"]')
        click(devtools, '#principal-password-rotation button[type="submit"]')
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form') && !document.querySelector('#logout')", "post-rotation sign-out")
        time.sleep(1)

        restored = devtools.command("Network.setCookie", {"name": "aegis-console", "value": old_session_value, "url": origin + "/console", "httpOnly": True, "sameSite": "Strict"})
        require(restored.get("success") is True, "browser proof could not restore the stale session for invalidation proof")
        devtools.command("Page.navigate", {"url": origin + "/console"})
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form') && !document.querySelector('#logout')", "old-session invalidation")
        time.sleep(1)

        insert_text(devtools, "#password", replacement_password)
        click(devtools, "#session-form button[type=submit]")
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#logout')", "replacement-password login")
        click(devtools, "#logout")
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form') && !document.querySelector('#logout')", "logged-out console")

        loaded_resources = devtools.evaluate("performance.getEntriesByType('resource').map(entry => entry.name)")
        require(isinstance(loaded_resources, list), "Chrome did not expose loaded resource evidence")
        require(
            not any(str(resource).endswith("/console/assets/datastar-v1.0.2.js") for resource in loaded_resources),
            "retained Datastar asset was loaded by the console document",
        )

        failures: list[str] = []
        for event in devtools.events:
            method = event.get("method", "")
            params = event.get("params", {})
            if method == "Runtime.exceptionThrown":
                failures.append("JavaScript exception")
            elif method == "Log.entryAdded":
                entry = params.get("entry", {})
                text = str(entry.get("text", ""))
                if entry.get("level") == "error" or "Content Security Policy" in text or "Refused to" in text:
                    failures.append("console/CSP error: " + text[:160])
            elif method == "Network.loadingFailed" and not params.get("canceled", False):
                failures.append("request failure")
            elif method == "Network.responseReceived" and params.get("response", {}).get("status", 0) >= 500:
                response = params.get("response", {})
                failures.append(f"unexpected HTTP {int(response.get('status', 0))}: {response.get('url', '')}")
            elif method == "Audits.issueAdded" and params.get("issue", {}).get("code") == "ContentSecurityPolicyIssue":
                failures.append("CSP violation")
        require(not failures, "real Chrome proof observed: " + ", ".join(sorted(set(failures))))
        print(json.dumps({
            "browser": "Google Chrome",
            "password_login": "pass",
            "password_rotation": "pass",
            "old_session_invalidation": "pass",
            "domains": list(expected.values()),
            "inspection": "pass",
            "interaction_foundation": {
                "dialog_initial_focus": "pass",
                "tab_containment": "pass",
                "reverse_tab_containment": "pass",
                "escape_and_focus_restore": "pass",
                "inert_background": "pass",
                "responsive_drawer_390px": "pass",
                "typed_state_distinction": "pass",
            },
            "charter_import_review": "pass",
            "logout": "pass",
            "csp_violations": 0,
            "javascript_errors": 0,
            "request_failures": 0,
            "unexpected_http_500": 0,
        }, sort_keys=True))
        return 0
    finally:
        if devtools is not None:
            devtools.close()
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)
        chrome_stderr.close()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (KeyError, OSError, RuntimeError, ValueError, json.JSONDecodeError) as error:
        print(json.dumps({"browser_proof": "failed", "error": str(error)}), file=sys.stderr)
        raise SystemExit(1)
