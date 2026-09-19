#!/usr/bin/env python3
"""Fail-closed Linux cgroup budget for all release verification descendants."""
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import uuid


def command(argv):
    if sys.platform != "linux" or not Path("/sys/fs/cgroup/cgroup.controllers").exists() or not shutil.which("systemd-run"):
        raise RuntimeError("verification requires Linux cgroup v2 and a working systemd user manager; no unbounded fallback")
    unit = "aegis-verify-" + uuid.uuid4().hex
    return unit, ["systemd-run", "--user", "--wait", "--pipe", "--collect", "--unit=" + unit,
        "--property=MemoryMax=6G", "--property=MemorySwapMax=0", "--property=TasksMax=256",
        "--property=CPUQuota=200%", "--property=RuntimeMaxSec=3600", "--property=TimeoutStopSec=5",
        "--property=KillMode=control-group", "--property=OOMPolicy=kill",
        "--property=MemoryAccounting=yes", "--property=CPUAccounting=yes",
        "--working-directory=" + os.getcwd(),
        *["--setenv=" + k for k in os.environ],
        "--setenv=AEGIS_VERIFY_UNIT=" + unit + ".service", "--", *argv]


def main():
    def interrupted(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    unit, args = command(sys.argv[1:] or ["make", "verify-stages"])
    print("verification budget: memory=6GiB swap=0 tasks=256 CPU=200% runtime=3600s; cgroup accounting follows", flush=True)
    try:
        return subprocess.run(args, timeout=3620, check=False).returncode
    finally:
        # Also stop the cgroup if the client is interrupted or times out.
        subprocess.run(["systemctl", "--user", "stop", unit], timeout=15, check=False,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, subprocess.TimeoutExpired) as error:
        print("verification supervision unavailable or failed: " + str(error), file=sys.stderr)
        sys.exit(1)
