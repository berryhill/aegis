#!/usr/bin/env python3
"""Linux foreground candidate-port custody. Coordination, not a sandbox."""
import argparse
import ctypes
import fcntl
import json
import os
from pathlib import Path
import select
import signal
import socket
import stat
import subprocess
import sys
import time

PORTS = tuple(range(8450, 8470))


class Denied(RuntimeError):
    pass


def open_pool(path):
    """Pin existing components; a private owned ancestor protects inner modes."""
    path = Path(path)
    if not path.is_absolute() or '..' in path.parts:
        raise Denied('pool must be an absolute existing path without traversal')
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    private = False
    try:
        for part in path.parts[1:]:
            nxt = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                          dir_fd=fd)
            os.close(fd)
            fd = nxt
            info = os.fstat(fd)
            if 'system.posix_acl_access' in os.listxattr(fd):
                raise Denied('pool ancestor access ACL unsupported')
            if info.st_uid not in (0, os.geteuid()):
                raise Denied('foreign pool ancestor')
            if not private and info.st_mode & 0o022:
                raise Denied('unprotected writable pool ancestor')
            if info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0:
                private = True
        if os.fstat(fd).st_uid != os.geteuid() or not private:
            raise Denied('pool requires invoking-user ownership and private ancestor')
        return fd
    except BaseException:
        os.close(fd)
        raise


def available(port):
    with socket.socket() as sock:
        # No SO_REUSEADDR: a listener or a conflicting wildcard bind is occupied.
        try:
            sock.bind(('127.0.0.1', port))
        except OSError:
            return False
    return True


def claim(pool_fd, ports=PORTS):
    if not ports or any(p not in PORTS for p in ports):
        raise Denied('only candidate ports 8450–8469 are permitted; 8448/8098 reserved')
    for port in ports:
        name = f'{port}.lock'
        # Existing inodes only, never create, truncate, write, unlink or replace.
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                     dir_fd=pool_fd)
        transferred = False
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                    or info.st_nlink != 1 or info.st_mode & 0o002
                    or 'system.posix_acl_access' in os.listxattr(fd)):
                raise Denied('unsafe lock identity/mode/ACL')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                continue
            current = os.stat(name, dir_fd=pool_fd, follow_symlinks=False)
            if (info.st_dev, info.st_ino) != (current.st_dev, current.st_ino):
                raise Denied('lock inode changed')
            if not available(port):
                continue
            transferred = True
            return port, fd
        finally:
            if not transferred:
                os.close(fd)
    raise Denied('candidate pool exhausted; no listener was killed')


def owned_group_exists(pgid):
    try:
        os.killpg(pgid, 0)
        return True
    except ProcessLookupError:
        return False


def reap():
    while True:
        try:
            if os.waitpid(-1, os.WNOHANG)[0] == 0:
                return
        except ChildProcessError:
            return


def supervise(args, owner_pipe):
    # Orphaned ordinary descendants are reaped here, not left as group zombies.
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
        raise Denied('subreaper unavailable')
    stopped = []
    def stop(signum, _frame):
        stopped.append(signum)
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, stop)
    pool_fd = open_pool(args.pool)
    lock_fd = None
    child = None
    try:
        port, lock_fd = claim(pool_fd, args.ports or PORTS)
        if stopped or select.select([owner_pipe], [], [], 0)[0]:
            return 130
        env = dict(os.environ, AEGIS_ACCEPTANCE_PORT=str(port))
        # Only this literal marker is replaced; no shell and no generic formatting.
        command = [a.replace('{AEGIS_ACCEPTANCE_PORT}', str(port)) for a in args.command]
        supervisor_pid = os.getpid()
        def child_setup():
            # Also kill the direct foreground child if the guardian itself crashes.
            if libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0:
                os._exit(125)
            if os.getppid() != supervisor_pid:
                os._exit(125)
        child = subprocess.Popen(command, env=env, start_new_session=True,
                                 pass_fds=(lock_fd,), preexec_fn=child_setup)
        print(json.dumps({'event': 'candidate_port_claimed', 'port': port,
                          'supervisor_pid': os.getpid(), 'child_pid': child.pid}),
              file=sys.stderr, flush=True)
        while child.poll() is None and not stopped:
            if select.select([owner_pipe], [], [], 0.05)[0]:
                stopped.append(signal.SIGTERM)
        code = child.poll()
        assert stopped or code is not None
        if stopped:
            return 128 + stopped[0]
        assert code is not None
        return code if code >= 0 else 128 - code
    finally:
        if child is not None:
            # Leader exit does not imply all its ordinary descendants exited.
            if owned_group_exists(child.pid):
                try:
                    os.killpg(child.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
            deadline = time.monotonic() + 3
            while owned_group_exists(child.pid):
                child.poll()
                reap()
                if time.monotonic() >= deadline:
                    try:
                        os.killpg(child.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                time.sleep(0.02)
            child.wait()
        if lock_fd is not None:
            os.close(lock_fd)
        os.close(pool_fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pool', default=os.environ.get('AEGIS_ACCEPTANCE_PORT_POOL'),
                        help='existing shared lock directory (required; no implicit pool)')
    parser.add_argument('--ports', type=int, nargs='+', help='test/diagnostic subset of candidate pool')
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.command[:1] == ['--']:
        args.command.pop(0)
    if sys.platform != 'linux' or not args.pool or not args.command:
        parser.error('Linux, explicit --pool/environment and foreground command required')
    if os.geteuid() == 0 or os.getuid() != os.geteuid():
        parser.error('refuse privileged invocation')
    read_fd, write_fd = os.pipe()
    # Guardian retains the claim when the invoking wrapper is SIGKILLed. EOF
    # triggers group shutdown; the launched command cannot inherit this pipe.
    pid = os.fork()
    if pid == 0:
        os.close(write_fd)
        try:
            code = supervise(args, read_fd)
        except (Denied, OSError, subprocess.SubprocessError) as exc:
            print('candidate port denied: ' + str(exc), file=sys.stderr, flush=True)
            code = 125
        finally:
            os.close(read_fd)
        os._exit(code)
    os.close(read_fd)
    def forward(sig, _frame):
        try:
            os.kill(pid, sig)
        except ProcessLookupError:
            pass
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, forward)
    try:
        _, status = os.waitpid(pid, 0)
        code = os.waitstatus_to_exitcode(status)
        return code if code >= 0 else 128 - code
    finally:
        os.close(write_fd)


if __name__ == '__main__':
    sys.exit(main())
