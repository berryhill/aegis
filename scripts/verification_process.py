"""Bounded, POSIX process-group custody for verification (not a sandbox)."""
import os
import selectors
import signal
import subprocess
import time


def run_owned(command, *, timeout, max_output=32 * 1024 * 1024, **kwargs):
    """Kill the owned group on success, failure, timeout and output overflow.

    Descendants must not detach. The outer verification cgroup is the hard
    boundary for descendants that start another session.
    """
    process = subprocess.Popen(command, start_new_session=True, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, **kwargs)
    assert process.stdout is not None
    output = bytearray()
    deadline = time.monotonic() + timeout
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while selector.get_map():
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise subprocess.TimeoutExpired(command, timeout)
                for key, _ in selector.select(min(remaining, 0.1)):
                    chunk = os.read(key.fd, 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    if len(output) + len(chunk) > max_output:
                        raise RuntimeError("verification child output exceeds limit")
                    output.extend(chunk)
            code = process.wait(timeout=max(0.001, deadline - time.monotonic()))
        return subprocess.CompletedProcess(command, code, bytes(output))
    finally:
        # Do this even after the leader exited: descendants may still be alive.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=5)
        process.stdout.close()
