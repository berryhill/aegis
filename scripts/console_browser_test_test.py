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


if __name__ == "__main__":
    unittest.main()
