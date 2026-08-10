#!/usr/bin/env python3
"""Fail closed unless a release archive contains exactly one safe executable."""

from __future__ import annotations

from pathlib import Path
import stat
import sys
import tarfile


def deny(message: str) -> int:
    print(f"release archive denied: {message}", file=sys.stderr)
    return 1


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: verify-release-archive.py ARCHIVE.tar.gz", file=sys.stderr)
        return 2

    archive = Path(sys.argv[1])
    if archive.is_symlink() or not archive.is_file():
        return deny("archive must be one regular non-symlink file")
    try:
        with tarfile.open(archive, mode="r:gz") as candidate:
            members = candidate.getmembers()
    except (OSError, tarfile.TarError) as exc:
        return deny(f"archive is unreadable: {exc}")

    if len(members) != 1:
        return deny("archive must contain exactly one member")
    member = members[0]
    if member.name != "aegis":
        return deny("archive member path must be exactly aegis")
    if not member.isfile():
        return deny("archive member must be a regular file")
    if stat.S_IMODE(member.mode) != 0o755:
        return deny("archive member mode must be exactly 0755")
    return 0


if __name__ == "__main__":
    sys.exit(main())
