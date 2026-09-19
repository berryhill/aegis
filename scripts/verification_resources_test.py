import importlib.util
import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

from scripts.console_browser_test import DevTools, MAX_FRAME_BYTES
from scripts import console_browser_test as browser
from scripts.verification_process import run_owned


class ResourceTests(unittest.TestCase):
    def client(self):
        client = DevTools.__new__(DevTools)
        client.sock = mock.Mock()
        client.next_id = 1
        client.events = []
        client.event_bytes = 0
        client.deadline = time.monotonic() + 1
        return client

    def test_oversized_frame_rejected_before_payload_read(self):
        client = self.client()
        client._recv_exact = mock.Mock(side_effect=[bytes([0x81, 127]), struct.pack('!Q', MAX_FRAME_BYTES+1)])
        with self.assertRaisesRegex(RuntimeError, 'frame exceeds'):
            client.receive()
        self.assertEqual(client._recv_exact.call_count, 2)

    def test_event_flood_is_not_silently_truncated(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'method': 'noise'})
        with mock.patch.object(browser, 'MAX_EVENTS', 2):
            with self.assertRaisesRegex(RuntimeError, 'event budget'):
                client.command('test')
        self.assertEqual(len(client.events), 2)

    def test_event_byte_budget(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'method': 'large event'})
        with mock.patch.object(browser, 'MAX_EVENT_BYTES', 1):
            with self.assertRaisesRegex(RuntimeError, 'event budget'):
                client.command('test')

    def test_absolute_deadline_even_when_messages_keep_arriving(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'method': 'noise'})
        with mock.patch.object(browser, 'CDP_TIMEOUT', 0.01):
            with self.assertRaisesRegex(RuntimeError, 'deadline'):
                client.command('test')

    def test_slow_frame_uses_remaining_deadline(self):
        client = self.client()
        def recv(size):
            time.sleep(.02)
            return b'x'
        client.sock.recv.side_effect = recv
        client.deadline = time.monotonic() + .01
        with self.assertRaisesRegex(RuntimeError, 'deadline'):
            client._recv_exact(2)

    def test_child_output_bounded(self):
        with self.assertRaisesRegex(RuntimeError, 'output exceeds'):
            run_owned([sys.executable, '-c', 'print("x"*100000)'], timeout=3, max_output=100)

    def test_timeout_kills_grandchild(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            marker = Path(directory)/'child'
            code = 'import subprocess,time,pathlib; p=subprocess.Popen(["sleep","30"]); pathlib.Path('+repr(str(marker))+').write_text(str(p.pid)); time.sleep(30)'
            with self.assertRaises(subprocess.TimeoutExpired):
                run_owned([sys.executable, '-c', code], timeout=.5)
            pid = int(marker.read_text())
            # A killed orphan may be a zombie until the system reaper collects it.
            for _ in range(50):
                status = Path(f'/proc/{pid}/stat')
                if not status.exists() or status.read_text().split()[2] == 'Z':
                    break
                time.sleep(.02)
            else:
                self.fail('owned grandchild survived timeout')

    def test_supervisor_no_unbounded_fallback(self):
        spec = importlib.util.spec_from_file_location('budget', Path(__file__).with_name('verify-budget.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with mock.patch.object(module.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'no unbounded fallback'):
                module.command(['true'])
        with (mock.patch.object(module.sys, 'platform', 'linux'),
              mock.patch.object(module.Path, 'exists', return_value=True),
              mock.patch.object(module.shutil, 'which', return_value='/fixture/systemd-run')):
            for environment, expected in (({}, '512MiB'), ({'GOMEMLIMIT': '384MiB'}, '384MiB')):
                with mock.patch.dict(module.os.environ, environment, clear=True):
                    unit, command = module.command(['true'])
                self.assertIn('--setenv=GOMEMLIMIT=' + expected, command)
                self.assertEqual(sum(arg.startswith('--setenv=GOMEMLIMIT=') for arg in command), 1)
        for prop in ('MemoryMax=6G', 'MemorySwapMax=0', 'TasksMax=256', 'RuntimeMaxSec=3600', 'KillMode=control-group'):
            self.assertIn('--property='+prop, command)


if __name__ == '__main__':
    unittest.main()
