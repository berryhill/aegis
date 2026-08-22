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
        require("exceptionDetails" not in result, "browser evaluation raised an exception")
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


def wait_for(devtools: DevTools, expression: str, description: str, timeout: float = 8) -> None:
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
        state = devtools.evaluate("({path: location.pathname + location.search, ready: document.readyState, title: document.querySelector('#surface-title')?.textContent || '', auth: document.querySelector('#authentication-status')?.textContent.trim() || '', body: document.body?.innerText.slice(0, 200) || ''})")
    except (OSError, RuntimeError):
        state = "unavailable"
    requests = []
    for event in devtools.events:
        if event.get("method") == "Network.requestWillBeSent":
            request = event.get("params", {}).get("request", {})
            if request.get("url", "").endswith("/console/session"):
                headers = request.get("headers", {})
                requests.append({"url": request.get("url"), "origin": headers.get("Origin", headers.get("origin", "")), "method": request.get("method")})
    raise RuntimeError(f"browser did not reach {description}; state={state}; requests={requests}")


def click(devtools: DevTools, selector: str) -> None:
    point = devtools.evaluate(
        "(() => { const node = document.querySelector(" + json.dumps(selector) + ");"
        "if (!node) return null; node.scrollIntoView({block: 'center', inline: 'center'}); const box = node.getBoundingClientRect();"
        "return {x: box.left + box.width / 2, y: box.top + box.height / 2}; })()"
    )
    require(isinstance(point, dict), f"browser control missing: {selector}")
    for event_type in ("mousePressed", "mouseReleased"):
        devtools.command("Input.dispatchMouseEvent", {
            "type": event_type,
            "x": point["x"],
            "y": point["y"],
            "button": "left",
            "clickCount": 1,
        })


def main() -> int:
    if len(sys.argv) != 4:
        raise RuntimeError("usage: console_browser_test.py ORIGIN PASSWORD_FILE WORKSPACE")
    origin = sys.argv[1].rstrip("/")
    password_path = pathlib.Path(sys.argv[2])
    workspace = pathlib.Path(sys.argv[3])
    passwords = json.loads(password_path.read_text(encoding="utf-8"))
    initial_password = passwords.get("initial")
    replacement_password = passwords.get("replacement")
    require(isinstance(initial_password, str) and len(initial_password) >= 12, "browser proof received no initial password")
    require(isinstance(replacement_password, str) and len(replacement_password) >= 12, "browser proof received no replacement password")
    chrome_home = workspace / "chrome"
    chrome_home.mkdir(mode=0o700)
    chrome_stderr_path = workspace / "chrome.stderr"
    chrome_stderr = chrome_stderr_path.open("w", encoding="utf-8")
    process = subprocess.Popen(
        [
            "/usr/bin/google-chrome",
            "--headless=new",
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

        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form')", "password login page")
        click(devtools, "#password")
        devtools.command("Input.insertText", {"text": initial_password})
        click(devtools, "#session-form button[type=submit]")
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#logout') && document.querySelector('#surface-title')?.textContent.trim() === 'Agent Registry'", "authenticated Agent Registry")
        time.sleep(0.5)

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
            time.sleep(0.5)

        click(devtools, 'a[href="/console/agents#/agents"]')
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#record-agent-alpha')", "seeded Agent Registry record")
        mutation_paths = ("/console/session", "/console/login", "/console/password")
        session_requests_before = sum(
            1
            for event in devtools.events
            if event.get("method") == "Network.requestWillBeSent"
            and event.get("params", {}).get("request", {}).get("url", "").endswith(mutation_paths)
        )
        click(devtools, 'a[href="#charter-import-review"]')
        wait_for(
            devtools,
            "location.hash === '#charter-import-review' && document.querySelector('#charter-import-review')?.textContent.includes('aegis charter validate') && document.querySelector('#charter-import-review')?.textContent.includes('aegis charter import')",
            "review-only charter import proposal",
        )
        session_requests_after = sum(
            1
            for event in devtools.events
            if event.get("method") == "Network.requestWillBeSent"
            and event.get("params", {}).get("request", {}).get("url", "").endswith(mutation_paths)
        )
        require(session_requests_after == session_requests_before, "charter import review link triggered a session mutation request")
        click(devtools, "#record-agent-alpha")
        wait_for(devtools, "document.readyState === 'complete' && !document.querySelector('#inspector').hidden && document.querySelector('#inspector-fields').textContent.includes('agent-alpha')", "Agent Registry detail")
        click(devtools, "#close-inspector")
        wait_for(devtools, "document.readyState === 'complete' && document.querySelector('#inspector').hidden", "closed Agent Registry detail")

        cookies = devtools.command("Network.getCookies", {"urls": [origin + "/console"]}).get("cookies", [])
        old_session = next((cookie for cookie in cookies if cookie.get("name") == "aegis-console"), None)
        if not isinstance(old_session, dict):
            raise RuntimeError("browser proof did not capture the pre-rotation session")
        old_session_value = old_session.get("value")
        require(isinstance(old_session_value, str) and bool(old_session_value), "browser proof captured an empty pre-rotation session")
        click(devtools, "#principal-password-rotation summary")
        wait_for(devtools, "document.querySelector('#principal-password-rotation')?.open === true", "open password rotation control")
        click(devtools, "#current-password")
        devtools.command("Input.insertText", {"text": initial_password})
        click(devtools, "#new-password")
        devtools.command("Input.insertText", {"text": replacement_password})
        click(devtools, "#confirm-password")
        devtools.command("Input.insertText", {"text": replacement_password})
        click(devtools, '#principal-password-rotation input[name="approve"]')
        click(devtools, '#principal-password-rotation button[type="submit"]')
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form') && !document.querySelector('#logout')", "post-rotation sign-out")
        time.sleep(1)

        restored = devtools.command("Network.setCookie", {"name": "aegis-console", "value": old_session_value, "url": origin + "/console", "httpOnly": True, "sameSite": "Strict"})
        require(restored.get("success") is True, "browser proof could not restore the stale session for invalidation proof")
        devtools.command("Page.navigate", {"url": origin + "/console"})
        wait_for(devtools, "document.readyState === 'complete' && !!document.querySelector('#session-form') && !document.querySelector('#logout')", "old-session invalidation")
        time.sleep(1)

        click(devtools, "#password")
        devtools.command("Input.insertText", {"text": replacement_password})
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
