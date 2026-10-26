"""Real-process tests; private lock fixtures, never the operator's shared pool."""
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import select
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
HELPER = ROOT / 'scripts/acceptance_port.py'
spec = importlib.util.spec_from_file_location('acceptance_port', HELPER)
assert spec and spec.loader
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
SERVER = """
import json, os, socket, time
s = socket.socket()
s.bind(('127.0.0.1', int(os.environ['AEGIS_ACCEPTANCE_PORT'])))
s.listen()
print(json.dumps({'port': s.getsockname()[1], 'pid': os.getpid()}), flush=True)
while True: time.sleep(.05)
"""


@unittest.skipUnless(sys.platform == 'linux' and os.geteuid() != 0,
                     'Linux unprivileged process-custody fixtures required')
class PortTests(unittest.TestCase):
    def setUp(self):
        base = ROOT / '.aegis-e2e/port-isolation-proof/fixtures'
        base.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(dir=base)
        self.pool = Path(self.temp.name) / 'locks'
        self.pool.mkdir(mode=0o700)
        for port in helper.PORTS:
            (self.pool / f'{port}.lock').write_text('preserved history\n')
        self.before = self.inventory()
        self.processes = []

    def inventory(self):
        return {p.name: (p.stat().st_ino, p.read_bytes()) for p in self.pool.iterdir()}

    def tearDown(self):
        for p in self.processes:
            if p.poll() is None:
                p.terminate()
            p.wait(timeout=8)
            p.stdout.close()
            p.stderr.close()
        self.temp.cleanup()

    def launch(self, code=SERVER, ports=(), pool=None, command=None):
        args = [sys.executable, str(HELPER), '--pool', str(pool or self.pool)]
        if ports:
            args += ['--ports', *map(str, ports)]
        args += ['--', *(command or [sys.executable, '-c', code])]
        p = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             text=True)
        self.processes.append(p)
        return p

    def ready(self, p):
        if not select.select([p.stdout], [], [], 5)[0]:
            self.fail('fixture did not become ready')
        line = p.stdout.readline()
        self.assertTrue(line, p.stderr.read() if p.poll() is not None else 'early EOF')
        return json.loads(line)

    def gone(self, port):
        deadline = time.monotonic() + 8
        while not helper.available(port) and time.monotonic() < deadline:
            time.sleep(.03)
        self.assertTrue(helper.available(port), 'owned listener survived cleanup')

    def free_ports(self, count=1):
        ports = [p for p in helper.PORTS if helper.available(p)]
        self.assertGreaterEqual(len(ports), count, 'isolated fixture needs free pool ports')
        return ports[:count]

    def test_concurrent_claims_lifetime_and_preserved_history(self):
        self.free_ports(4)
        processes = [self.launch() for _ in range(4)]
        records = [self.ready(p) for p in processes]
        self.assertEqual(4, len({r['port'] for r in records}))
        for r in records:
            with (self.pool / f"{r['port']}.lock").open() as lock:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        for p, r in zip(processes, records):
            p.terminate()
            self.assertEqual(143, p.wait(timeout=8))
            self.gone(r['port'])
        self.assertEqual(self.before, self.inventory())

    def test_busy_lock_skipped_and_exhaustion(self):
        ports = self.free_ports(2)
        with (self.pool / f'{ports[0]}.lock').open() as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            p = self.launch(ports=ports)
            self.assertEqual(ports[1], self.ready(p)['port'])
            q = self.launch(ports=ports)
            self.assertEqual(125, q.wait(timeout=5))
            self.assertIn('exhausted', q.stderr.read())

    def test_unregistered_listener_not_killed(self):
        ports = self.free_ports(2)
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', ports[0]))
            listener.listen()
            p = self.launch(ports=ports)
            self.assertEqual(ports[1], self.ready(p)['port'])
            q = self.launch(ports=[ports[0]])
            self.assertEqual(125, q.wait(timeout=5))
            with socket.create_connection(('127.0.0.1', ports[0]), timeout=1):
                pass

    def test_reserved_out_of_range(self):
        for port in (8448, 8098, 8449, 8470, 0):
            p = self.launch(ports=[port])
            self.assertEqual(125, p.wait(timeout=5))
            self.assertIn('reserved', p.stderr.read())
        self.assertEqual(self.before, self.inventory())

    def test_privileged_invocation_denied_before_fork(self):
        argv = ['acceptance_port.py', '--pool', str(self.pool), '--', 'never-launch']
        with mock.patch.object(sys, 'argv', argv), mock.patch.object(os, 'geteuid', return_value=0):
            with mock.patch.object(os, 'fork') as fork:
                with self.assertRaises(SystemExit) as raised:
                    helper.main()
                self.assertEqual(2, raised.exception.code)
                fork.assert_not_called()

    def test_full_pool_exhaustion(self):
        locks = [(self.pool / f'{p}.lock').open() for p in helper.PORTS]
        try:
            for lock in locks:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            p = self.launch()
            self.assertEqual(125, p.wait(timeout=5))
            self.assertIn('exhausted', p.stderr.read())
        finally:
            for lock in locks:
                lock.close()

    def test_normal_exit_failed_spawn_and_child_crash_release(self):
        port = self.free_ports()[0]
        for command in ([sys.executable, '-c', 'raise SystemExit(7)'],
                        ['/nonexistent-aegis-acceptance-command'],
                        [sys.executable, '-c', 'import os,signal; os.kill(os.getpid(),signal.SIGKILL)']):
            p = self.launch(ports=[port], command=command)
            self.assertIn(p.wait(timeout=8), (7, 125, 137))
            fd = helper.open_pool(self.pool)
            try:
                chosen, claim = helper.claim(fd, [port])
                self.assertEqual(port, chosen)
                os.close(claim)
            finally:
                os.close(fd)
        self.assertEqual(self.before, self.inventory())

    def test_signals_and_wrapper_crash_clean_listener_before_release(self):
        port = self.free_ports()[0]
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP, signal.SIGKILL):
            p = self.launch(ports=[port])
            self.ready(p)
            p.send_signal(sig)
            p.wait(timeout=8)
            self.gone(port)
            deadline = time.monotonic() + 5
            while True:
                try:
                    fd = helper.open_pool(self.pool)
                    try:
                        _, lock = helper.claim(fd, [port])
                        os.close(lock)
                    finally:
                        os.close(fd)
                    break
                except helper.Denied:
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(.03)
        self.assertEqual(self.before, self.inventory())

    def test_guardian_crash_kills_direct_child(self):
        port = self.free_ports()[0]
        p = self.launch(ports=[port])
        self.ready(p)
        self.assertTrue(select.select([p.stderr], [], [], 5)[0])
        record = json.loads(p.stderr.readline())
        os.kill(record['supervisor_pid'], signal.SIGKILL)
        self.assertEqual(137, p.wait(timeout=8))
        self.gone(port)

    def test_guardian_crash_cleans_ordinary_grandchild_before_release(self):
        port = self.free_ports()[0]
        code = ('import subprocess,sys,time; subprocess.Popen([sys.executable,"-c",'
                + repr(SERVER) + ']); time.sleep(60)')
        p = self.launch(code=code, ports=[port])
        self.ready(p)
        assert p.stderr is not None
        self.assertTrue(select.select([p.stderr], [], [], 5)[0])
        record = json.loads(p.stderr.readline())
        try:
            os.kill(record['supervisor_pid'], signal.SIGKILL)
            self.assertEqual(137, p.wait(timeout=8))
            self.gone(port)
            fd = helper.open_pool(self.pool)
            try:
                _, claimed = helper.claim(fd, [port])
                os.close(claimed)
            finally:
                os.close(fd)
        finally:
            # This group belongs exclusively to this private test fixture.
            try:
                os.killpg(record['child_pid'], signal.SIGKILL)
            except ProcessLookupError:
                pass

    def test_setup_spawn_and_argument_errors_do_not_echo_input(self):
        canary = 'PORT_TEST_FAKE_SECRET_CANARY'
        for args in (
                ['--pool', str(self.pool), '--', '/nonexistent-' + canary],
                ['--pool', str(self.pool / canary), '--', 'never-launch'],
                ['--pool', str(self.pool), '--ports', canary, '--', 'never-launch']):
            result = subprocess.run([sys.executable, str(HELPER), *args],
                                    capture_output=True, text=True, timeout=5)
            self.assertIn(result.returncode, (2, 125))
            self.assertNotIn(canary, result.stdout + result.stderr)

    def test_missing_and_other_writable_lock_fail_closed(self):
        port = self.free_ports()[0]
        lock = self.pool / f'{port}.lock'
        lock.unlink()  # Private fixture only.
        p = self.launch(ports=[port])
        self.assertEqual(125, p.wait(timeout=5))
        self.assertFalse(lock.exists())
        lock.write_text('preserved')
        lock.chmod(0o666)
        p = self.launch(ports=[port])
        self.assertEqual(125, p.wait(timeout=5))
        self.assertEqual('preserved', lock.read_text())

    def test_descendant_after_leader_exit_is_cleaned(self):
        port = self.free_ports()[0]
        code = ('import subprocess,sys,time; subprocess.Popen([sys.executable,"-c",'
                + repr(SERVER) + ']); time.sleep(.2)')
        p = self.launch(code=code, ports=[port])
        self.ready(p)
        self.assertEqual(0, p.wait(timeout=8))
        self.gone(port)

    def test_termination_waits_for_stubborn_child(self):
        port = self.free_ports()[0]
        code = 'import signal; signal.signal(signal.SIGTERM,signal.SIG_IGN)\n' + SERVER
        p = self.launch(code=code, ports=[port])
        self.ready(p)
        p.terminate()
        time.sleep(.2)
        self.assertIsNone(p.poll())
        with (self.pool / f'{port}.lock').open() as lock:
            with self.assertRaises(BlockingIOError):
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.assertEqual(143, p.wait(timeout=8))
        self.gone(port)

    def test_identity_symlinks_hardlinks_fifo_and_separate_pool(self):
        port = self.free_ports()[0]
        alias = Path(self.temp.name) / 'alias'
        alias.symlink_to(self.pool, target_is_directory=True)
        p = self.launch(pool=alias)
        self.assertEqual(125, p.wait(timeout=5))
        lock = self.pool / f'{port}.lock'
        original = lock.read_bytes()
        lock.unlink()  # PRIVATE test fixture only, never the operator pool.
        lock.symlink_to(self.pool / f'{port + 1}.lock')
        p = self.launch(ports=[port])
        self.assertEqual(125, p.wait(timeout=5))
        lock.unlink()
        os.link(self.pool / f'{port + 1}.lock', lock)
        p = self.launch(ports=[port])
        self.assertEqual(125, p.wait(timeout=5))
        lock.unlink()
        os.mkfifo(lock)
        p = self.launch(ports=[port])
        self.assertEqual(125, p.wait(timeout=5))
        lock.unlink()
        lock.write_bytes(original)
        other = Path(self.temp.name) / 'separate'
        other.mkdir(mode=0o700)
        for candidate in helper.PORTS:
            (other / f'{candidate}.lock').write_text('separate fixture')
        p = self.launch(ports=[port])
        self.ready(p)
        q = self.launch(ports=[port], pool=other)
        self.assertEqual(125, q.wait(timeout=5))  # bind check, not shared-root exclusion

    def test_literal_argv_and_environment_no_shell(self):
        code = ('import json,os,sys; print(json.dumps([os.environ["AEGIS_ACCEPTANCE_PORT"],'
                'sys.argv[1:]]),flush=True)')
        p = self.launch(command=[sys.executable, '-c', code,
                                '{AEGIS_ACCEPTANCE_PORT}', '$(touch NO_SUCH_FILE); *'])
        record = self.ready(p)
        self.assertEqual(record[0], record[1][0])
        self.assertEqual('$(touch NO_SUCH_FILE); *', record[1][1])
        self.assertEqual(0, p.wait(timeout=5))


if __name__ == '__main__':
    unittest.main()
