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

    def test_unknown_key_fails_closed(self):
        with self.assertRaisesRegex(RuntimeError, "does not define a native key code"):
            console_browser_test.key(mock.MagicMock(), "Enter")


class NativeFormInputTest(unittest.TestCase):
    def test_insert_text_requires_exact_browser_value_before_submission(self):
        devtools = mock.MagicMock()
        devtools.evaluate.side_effect = [
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


if __name__ == "__main__":
    unittest.main()
