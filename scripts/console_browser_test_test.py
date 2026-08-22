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


if __name__ == "__main__":
    unittest.main()
