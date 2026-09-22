#!/usr/bin/env python3
"""Bounded release preflight only; version acceptance is not runtime qualification."""
import pathlib
import re
import sys

IDENTITY = re.compile(
    r"Hermes Agent v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?: \((?:[0-9]+\.){2,3}[0-9]+\)"
    r"(?: · upstream [0-9a-f]{8,40} · local [0-9a-f]{8,40}"
    r" \(\+[1-9][0-9]* carried commits?\))?)?"
)


def accepted_version(output):
    # The release harness deliberately retains its narrower 4-KiB output bound.
    if not output or len(output) > 4096 or b"\0" in output:
        raise ValueError("invalid or oversized Hermes identity output")
    version = None
    installation = None
    for line in output.decode("utf-8", errors="strict").splitlines():
        line = line.strip()
        match = IDENTITY.fullmatch(line)
        if match:
            if version is not None:
                raise ValueError("ambiguous Hermes identity output")
            version = match.groups()[:3]
        elif line.startswith("Hermes Agent"):
            raise ValueError("malformed Hermes identity output")
        elif line.startswith("Install directory:"):
            path = line.removeprefix("Install directory:").strip()
            if installation is not None or not path.startswith("/"):
                raise ValueError("invalid or ambiguous Hermes installation output")
            installation = path
    if version is None:
        raise ValueError("missing Hermes identity output")
    # Compare normalized numeric components without an integer-size upper cap.
    if version[0] == "0" and (len(version[1]), version[1]) < (2, "18"):
        raise ValueError("Hermes version is below accepted minimum >=0.18.0")
    return ".".join(version)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: verify-hermes-version.py VERSION_OUTPUT_FILE")
    try:
        with pathlib.Path(sys.argv[1]).open("rb") as source:
            accepted_version(source.read(4097))
    except (OSError, ValueError) as exc:
        raise SystemExit(f"release candidate denied: {exc}")
