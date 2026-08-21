#!/usr/bin/env python3
"""Render one release changelog from a source file without mutating it."""

from __future__ import annotations

import argparse
import datetime as dt
import pathlib
import re

SEMVER = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")


def prepare(source: pathlib.Path, destination: pathlib.Path, version: str, release_date: str) -> None:
    if not SEMVER.fullmatch(version):
        raise SystemExit("VERSION must be MAJOR.MINOR.PATCH")
    try:
        dt.date.fromisoformat(release_date)
    except ValueError as exc:
        raise SystemExit("RELEASE_DATE must be YYYY-MM-DD") from exc

    text = source.read_text(encoding="utf-8")
    marker = "## Unreleased"
    heading = f"## [{version}] - {release_date}"
    if text.count(marker) != 1:
        raise SystemExit("CHANGELOG.md must contain exactly one Unreleased heading")
    if any(line.startswith(f"## [{version}] - ") for line in text.splitlines()):
        raise SystemExit(f"CHANGELOG.md already contains a {version} release heading")

    before, after = text.split(marker, 1)
    next_release = after.find("\n## ")
    pending = after if next_release < 0 else after[:next_release]
    if not pending.strip():
        raise SystemExit("CHANGELOG.md has no unreleased entries")
    tail = "" if next_release < 0 else after[next_release:]
    pending_text = pending.strip("\n")
    destination.write_text(
        before + marker + "\n\n" + heading + "\n\n" + pending_text + "\n" + tail,
        encoding="utf-8",
    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=pathlib.Path)
    parser.add_argument("destination", type=pathlib.Path)
    parser.add_argument("version")
    parser.add_argument("release_date")
    args = parser.parse_args()
    prepare(args.source, args.destination, args.version, args.release_date)


if __name__ == "__main__":
    main()
