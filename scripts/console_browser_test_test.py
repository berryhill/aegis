import pathlib
import subprocess
import tempfile
import time
import unittest
from unittest import mock

from scripts import console_browser_test


class ProcessStub:
    def __init__(self, status):
        self.status = status

    def poll(self):
        return self.status


class PageWebsocketTest(unittest.TestCase):
    def test_denies_when_chrome_exits_after_active_port_publication(self):
        with self.assertRaisesRegex(
            RuntimeError,
            "Chrome exited before exposing a debuggable console page",
        ):
            console_browser_test.page_websocket(
                1,
                time.monotonic() + 1,
                ProcessStub(17),
            )

    def test_returns_first_page_target_from_live_chrome(self):
        response = mock.MagicMock()
        response.read.return_value = b'[{"type":"page","webSocketDebuggerUrl":"ws://127.0.0.1:1/devtools/page/1"}]'
        connection = mock.MagicMock()
        connection.getresponse.return_value = response
        with mock.patch.object(
            console_browser_test.http.client,
            "HTTPConnection",
            return_value=connection,
        ):
            websocket = console_browser_test.page_websocket(
                1,
                time.monotonic() + 1,
                ProcessStub(None),
            )
        self.assertEqual(websocket, "ws://127.0.0.1:1/devtools/page/1")
        connection.close.assert_called_once_with()

    def test_waits_for_page_target_after_devtools_port_is_ready(self):
        pending = mock.MagicMock()
        pending.read.return_value = b"[]"
        ready = mock.MagicMock()
        ready.read.return_value = b'[{"type":"page","webSocketDebuggerUrl":"ws://127.0.0.1:1/devtools/page/delayed"}]'
        first_connection = mock.MagicMock()
        first_connection.getresponse.return_value = pending
        second_connection = mock.MagicMock()
        second_connection.getresponse.return_value = ready
        with (
            mock.patch.object(
                console_browser_test.http.client,
                "HTTPConnection",
                side_effect=[first_connection, second_connection],
            ),
            mock.patch.object(console_browser_test.time, "sleep"),
        ):
            websocket = console_browser_test.page_websocket(
                1,
                time.monotonic() + 1,
                ProcessStub(None),
            )
        self.assertEqual(websocket, "ws://127.0.0.1:1/devtools/page/delayed")
        first_connection.close.assert_called_once_with()
        second_connection.close.assert_called_once_with()

class NativeKeyTest(unittest.TestCase):
    def test_escape_uses_trusted_native_key_codes(self):
        devtools = mock.MagicMock()

        console_browser_test.key(devtools, "Escape")

        self.assertEqual(devtools.command.call_count, 2)
        for call, event_type in zip(devtools.command.call_args_list, ("rawKeyDown", "keyUp")):
            method, params = call.args
            self.assertEqual(method, "Input.dispatchKeyEvent")
            self.assertEqual(params["type"], event_type)
            self.assertEqual(params["key"], "Escape")
            self.assertEqual(params["windowsVirtualKeyCode"], 27)
            self.assertEqual(params["nativeVirtualKeyCode"], 27)

    def test_shift_tab_retains_modifier_and_native_key_code(self):
        devtools = mock.MagicMock()

        console_browser_test.key(devtools, "Tab", shift=True)

        for call in devtools.command.call_args_list:
            params = call.args[1]
            self.assertEqual(params["modifiers"], 8)
            self.assertEqual(params["windowsVirtualKeyCode"], 9)

    def test_enter_uses_trusted_native_key_code(self):
        devtools = mock.MagicMock()

        console_browser_test.key(devtools, "Enter")

        for call in devtools.command.call_args_list:
            params = call.args[1]
            self.assertEqual(params["windowsVirtualKeyCode"], 13)
            self.assertEqual(params["nativeVirtualKeyCode"], 13)

    def test_unknown_key_fails_closed(self):
        with self.assertRaisesRegex(RuntimeError, "does not define a native key code"):
            console_browser_test.key(mock.MagicMock(), "Space")


class ChromeShutdownTest(unittest.TestCase):
    def test_orderly_shutdown_waits_before_closing_devtools(self):
        calls = mock.MagicMock()
        process, devtools = calls.process, calls.devtools
        process.poll.return_value = None
        console_browser_test.stop_chrome(process, devtools)
        self.assertEqual(calls.mock_calls, [
            mock.call.process.poll(), mock.call.devtools.command("Browser.close"),
            mock.call.process.wait(timeout=5), mock.call.devtools.close(),
        ])

    def test_disconnected_browser_still_waits_for_exit(self):
        process, devtools = mock.MagicMock(), mock.MagicMock()
        process.poll.return_value = None
        devtools.command.side_effect = OSError("connection closed")
        console_browser_test.stop_chrome(process, devtools)
        process.wait.assert_called_once_with(timeout=5)
        process.terminate.assert_not_called()
        devtools.close.assert_called_once_with()

    def test_unresponsive_browser_has_bounded_terminate_kill_wait(self):
        calls = mock.MagicMock()
        process = calls.process
        process.wait.side_effect = [subprocess.TimeoutExpired("chrome", 5),
                                    subprocess.TimeoutExpired("chrome", 5), 0]
        console_browser_test.stop_chrome(process, None)
        self.assertEqual(calls.mock_calls, [
            mock.call.process.wait(timeout=5), mock.call.process.terminate(),
            mock.call.process.wait(timeout=5), mock.call.process.kill(),
            mock.call.process.wait(timeout=5),
        ])


class NativeTouchTest(unittest.TestCase):
    def test_tap_enables_emulation_before_synthesizing_complete_touch_gesture(self):
        devtools = mock.MagicMock()
        devtools.evaluate.side_effect = [
            True,
            {"x": 12, "y": 24, "width": 100, "height": 44, "target": True},
            True,
            True,
            {"x": 13, "y": 25, "navigated": False, "target": True},
            True,
        ]

        with mock.patch.object(console_browser_test.time, "sleep"):
            console_browser_test.tap(devtools, "#record-proof-agent")

        self.assertEqual(
            devtools.command.call_args_list,
            [
                mock.call("Emulation.setTouchEmulationEnabled", {
                    "enabled": True, "maxTouchPoints": 1, "configuration": "mobile",
                }),
                mock.call("Input.synthesizeTapGesture", {
                    "x": 12,
                    "y": 24,
                    "duration": 50,
                    "tapCount": 1,
                    "gestureSourceType": "touch",
                }),
                mock.call("Input.dispatchTouchEvent", {
                    "type": "touchStart",
                    "touchPoints": [{"x": 13, "y": 25, "radiusX": 1, "radiusY": 1, "force": 1}],
                }),
                mock.call("Input.dispatchTouchEvent", {"type": "touchEnd", "touchPoints": []}),
            ],
        )

    def test_tap_maps_visual_pan_without_rescaling_css_pixels(self):
        devtools = mock.MagicMock()
        devtools.evaluate.side_effect = [
            True,
            {"x": 42, "y": 176, "width": 100, "height": 44, "target": True,
             "visualOffsetX": 30, "visualOffsetY": 152, "visualScale": 2, "devicePixelRatio": 3},
            True, True, {"navigated": True},
        ]
        with mock.patch.object(console_browser_test.time, "sleep"):
            console_browser_test.tap(devtools, "#record-proof-agent")
        devtools.command.assert_called_with("Input.synthesizeTapGesture", {
            "x": 12, "y": 24, "duration": 50, "tapCount": 1, "gestureSourceType": "touch",
        })

    def test_real_chrome_touch_gesture_activates_anchor_navigation(self):
        self._assert_real_chrome_touch_navigation(page_scale=1)

    def test_real_chrome_touch_with_panned_visual_viewport(self):
        self._assert_real_chrome_touch_navigation(page_scale=2)

    def _assert_real_chrome_touch_navigation(self, page_scale):
        with tempfile.TemporaryDirectory(prefix="aegis-touch-browser-") as temporary:
            fixture_root = pathlib.Path(temporary)
            chrome_home = fixture_root / "chrome"
            fixture = fixture_root / "touch.html"
            fixture.write_text(
                '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1">'
                '<a id="record-proof-agent" href="#/agents/proof-agent" '
                'style="display:block;width:180px;height:48px;margin-top:1200px">Open Agent</a>',
                encoding="utf-8",
            )
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
                    fixture.as_uri(),
                ],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            devtools = None
            try:
                active_port = chrome_home / "DevToolsActivePort"
                deadline = time.monotonic() + console_browser_test.CHROME_START_TIMEOUT
                while time.monotonic() < deadline and not active_port.exists():
                    self.assertIsNone(process.poll(), "Chrome exited before DevTools readiness")
                    time.sleep(0.05)
                self.assertTrue(active_port.exists(), "Chrome did not become ready")
                port = int(active_port.read_text(encoding="utf-8").splitlines()[0])
                websocket = console_browser_test.page_websocket(
                    port, time.monotonic() + console_browser_test.PAGE_TARGET_TIMEOUT, process
                )
                devtools = console_browser_test.DevTools(websocket)
                for domain in ("Page", "Runtime"):
                    devtools.command(domain + ".enable")
                console_browser_test.wait_for(
                    devtools, "document.readyState === 'complete' && !!document.querySelector('#record-proof-agent')",
                    "focused touch fixture load", timeout=5,
                )
                devtools.command("Emulation.setDeviceMetricsOverride", {
                    "width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": True,
                })

                # Reproduce a panned visual viewport, not just document scroll.
                # The installed console reaches this state after responsive focus
                # restoration; zoom makes the coordinate distinction deterministic.
                devtools.command("Emulation.setPageScaleFactor", {"pageScaleFactor": page_scale})
                if page_scale > 1:
                    devtools.evaluate("document.querySelector('#record-proof-agent').scrollIntoView({block: 'center', inline: 'center'})")
                    console_browser_test.wait_for(devtools, "visualViewport.offsetTop > 0 && visualViewport.offsetLeft > 0", "panned visual viewport", timeout=5)
                    self.assertGreater(devtools.evaluate("visualViewport.offsetTop"), 0)
                console_browser_test.tap(devtools, "#record-proof-agent")
                console_browser_test.wait_for(
                    devtools,
                    "location.hash === '#/agents/proof-agent'",
                    "focused real-browser touch navigation fixture",
                    timeout=5,
                )
                proof = console_browser_test.touch_proof(devtools)
                self.assertEqual(proof["selector"], "#record-proof-agent")
                event_types = {event["type"] for event in proof["events"]}
                self.assertTrue({"touchstart", "touchend", "click"}.issubset(event_types))
                self.assertTrue(all(event["trusted"] for event in proof["events"]))
            finally:
                console_browser_test.stop_chrome(process, devtools)

    def test_touch_emulation_is_explicitly_enabled_for_native_gestures(self):
        devtools = mock.MagicMock()

        console_browser_test.set_touch_emulation(devtools, True)

        devtools.command.assert_called_once_with(
            "Emulation.setTouchEmulationEnabled",
            {"enabled": True, "maxTouchPoints": 1, "configuration": "mobile"},
        )

    def test_touch_emulation_can_be_disabled_after_navigation(self):
        devtools = mock.MagicMock()

        console_browser_test.set_touch_emulation(devtools, False)

        devtools.command.assert_called_once_with(
            "Emulation.setTouchEmulationEnabled",
            {"enabled": False},
        )


class NavigateTest(unittest.TestCase):
    def test_explicitly_navigates_selected_target(self):
        devtools = mock.MagicMock()
        devtools.command.return_value = {"frameId": "proof-frame"}

        console_browser_test.navigate(devtools, "http://127.0.0.1:8080/console")

        devtools.command.assert_called_once_with(
            "Page.navigate",
            {"url": "http://127.0.0.1:8080/console"},
        )

    def test_denies_navigation_error(self):
        devtools = mock.MagicMock()
        devtools.command.return_value = {"errorText": "net::ERR_CONNECTION_REFUSED"}

        with self.assertRaisesRegex(RuntimeError, "browser navigation failed"):
            console_browser_test.navigate(devtools, "http://127.0.0.1:8080/console")


class NativeFormInputTest(unittest.TestCase):
    def test_insert_text_requires_exact_browser_value_before_submission(self):
        devtools = mock.MagicMock()
        devtools.evaluate.side_effect = [
            True,
            {"x": 10, "y": 10, "width": 100, "height": 20, "hit": "password", "target": True},
            True,
            True,
        ]

        console_browser_test.insert_text(devtools, "#password", "candidate-password")

        self.assertEqual(devtools.command.call_args, mock.call("Input.insertText", {"text": "candidate-password"}))
        self.assertIn("candidate-password", devtools.evaluate.call_args_list[-1].args[0])

    def test_insert_text_denies_when_browser_did_not_retain_value(self):
        devtools = mock.MagicMock()
        devtools.evaluate.side_effect = [
            True,
            {"x": 10, "y": 10, "width": 100, "height": 20, "hit": "password", "target": True},
            True,
            False,
        ]

        with self.assertRaisesRegex(RuntimeError, "did not retain text"):
            console_browser_test.insert_text(devtools, "#password", "candidate-password")


class WaitForDiagnosticsTest(unittest.TestCase):
    def test_wait_for_reports_elapsed_time_and_document_request_count_on_failure(self):
        devtools = mock.MagicMock()
        # evaluate returns None (predicate never true) for every loop iteration,
        # then the state dict once the timeout fires. Because the inner loop
        # evaluates the predicate many times, the side_effect must always return
        # a falsy value until the post-timeout state read.
        state_reads = 0

        def evaluate_side_effect(expression):
            nonlocal state_reads
            if expression.startswith("({path:"):
                state_reads += 1
                return {"path": "/console", "ready": "complete", "title": "", "auth": "required", "active": "BODY", "modal": "", "body": "auth"}
            return None

        devtools.evaluate.side_effect = evaluate_side_effect
        devtools.events = [
            {"method": "Network.requestWillBeSent", "params": {"request": {"url": "http://127.0.0.1:8000/console/", "headers": {}, "method": "GET"}, "type": "Document"}},
            {"method": "Network.requestWillBeSent", "params": {"request": {"url": "http://127.0.0.1:8000/console/assets/app.css", "headers": {}, "method": "GET"}}},
        ]
        # Pin monotonic so the loop iterates a few times (deadline expires) then
        # exit; the post-timeout state read happens once. The mock value must
        # stay below the real process monotonic so elapsed stays non-negative.
        REAL_START = console_browser_test._BROWSER_PROOF_START
        sequence = [REAL_START, REAL_START + 0.1, REAL_START + 0.2, REAL_START + 0.3, REAL_START + 0.4, REAL_START + 0.5, REAL_START + 1.5, REAL_START + 2.0, REAL_START + 2.1]
        monotonic_calls = iter(sequence)

        def monotonic():
            try:
                return next(monotonic_calls)
            except StopIteration:
                return sequence[-1]

        with mock.patch.object(console_browser_test.time, "monotonic", side_effect=monotonic), mock.patch.object(console_browser_test.time, "sleep"):
            with self.assertRaisesRegex(
                RuntimeError,
                r"browser did not reach reach authenticated console; elapsed=[\d.]+s timeout=1s document_requests=1",
            ):
                console_browser_test.wait_for(devtools, "document.readyState === 'complete'", "reach authenticated console", timeout=1)
        self.assertEqual(state_reads, 1, "wait_for should read state exactly once after timeout")

    def test_wait_for_reports_only_auth_transitions_after_action_boundary(self):
        devtools = mock.MagicMock()
        devtools.evaluate.side_effect = lambda expression: (
            {"path": "/console", "ready": "complete", "title": "", "auth": "required", "active": "BODY", "modal": "", "body": "auth"}
            if expression.startswith("({path:")
            else None
        )
        devtools.events = [
            {"method": "Network.requestWillBeSent", "params": {"request": {"url": "http://127.0.0.1:8000/console/login", "method": "POST"}, "type": "Document"}},
            {"method": "Network.requestWillBeSent", "params": {"request": {"url": "http://127.0.0.1:8000/console/logout", "method": "POST"}, "type": "Document"}},
        ]
        start = console_browser_test._BROWSER_PROOF_START
        monotonic = iter([start, start, start + 1, start + 1, start + 1])
        with mock.patch.object(console_browser_test.time, "monotonic", side_effect=lambda: next(monotonic)), mock.patch.object(console_browser_test.time, "sleep"):
            with self.assertRaisesRegex(RuntimeError, r"requests=\[\{'url': 'http://127.0.0.1:8000/console/logout', 'method': 'POST', 'type': 'Document'\}\]"):
                console_browser_test.wait_for(
                    devtools,
                    "false",
                    "open narrow password rotation dialog",
                    timeout=1,
                    diagnostic_event_start=1,
                )


class AuthenticationBoundaryTest(unittest.TestCase):
    def test_requires_authenticated_console_immediately_before_action(self):
        devtools = mock.MagicMock()
        devtools.evaluate.return_value = {"authenticated": False, "login": True, "path": "/console", "ready": "complete"}
        devtools.events = [
            {"method": "Network.requestWillBeSent", "params": {"request": {"url": "http://127.0.0.1:8000/console/logout", "method": "POST"}, "type": "Document"}},
        ]

        with self.assertRaisesRegex(RuntimeError, "browser lost authentication before narrow password rotation"):
            console_browser_test.require_authenticated_console(devtools, "before narrow password rotation")


if __name__ == "__main__":
    unittest.main()
