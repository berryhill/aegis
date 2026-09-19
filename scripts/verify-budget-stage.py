#!/usr/bin/env python3
"""Stage timing and aggregate cgroup resource readback, never command output."""
import os
from pathlib import Path
import subprocess
import sys
import time


def group():
    unit = os.environ.get("AEGIS_VERIFY_UNIT", "")
    membership = Path("/proc/self/cgroup").read_text().strip().splitlines()
    path = next((line[3:] for line in membership if line.startswith("0::")), "")
    if not unit.startswith("aegis-verify-") or unit not in path.split("/"):
        raise RuntimeError("verify-stages must run inside its verification cgroup")
    return Path("/sys/fs/cgroup") / path.lstrip("/")


def main():
    directory = group()
    if sys.argv[1:] == ["--check"]:
        return 0
    started = time.monotonic()
    result = subprocess.run(["/bin/sh", *sys.argv[1:]], check=False)
    stats = {}
    for name in ("memory.current", "memory.peak", "pids.current", "cpu.stat"):
        path = directory / name
        if path.exists():
            stats[name] = path.read_text().strip().replace("\n", ";")
    print(f"verification stage pid={os.getpid()} exit={result.returncode} elapsed={time.monotonic()-started:.2f}s cgroup={stats}", file=sys.stderr, flush=True)
    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
