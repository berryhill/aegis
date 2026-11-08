#!/usr/bin/env python3
"""Prepare exactly go.sum-pinned fixture inputs before offline test execution."""
import base64

from pathlib import Path
import re
import subprocess


MODULE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._~+!/-]*$")
VERSION = re.compile(r"^v[0-9][A-Za-z0-9.+-]*$")


def locked_inputs(data):
    archives, metadata, seen = set(), set(), {}
    for line in data.decode("utf-8").splitlines():
        fields = line.split()
        if len(fields) != 3:
            raise ValueError("invalid locked module record")
        module, revision, checksum = fields
        version = revision.removesuffix("/go.mod")
        if not MODULE.fullmatch(module) or not VERSION.fullmatch(version):
            raise ValueError("invalid locked module identity")
        if not checksum.startswith("h1:") or len(base64.b64decode(checksum[3:], validate=True)) != 32:
            raise ValueError("invalid locked module checksum")
        key = (module, revision)
        if key in seen and seen[key] != checksum:
            raise ValueError("conflicting locked module checksum")
        seen[key] = checksum
        target = module + "@" + version
        (metadata if revision.endswith("/go.mod") else archives).add(target)
    if not archives or len(seen) > 4096:
        raise ValueError("empty or oversized locked dependency set")
    return sorted(archives), sorted(metadata - archives)


def prepare(root):
    paths = [root / "go.mod", root / "go.sum"]
    if any(p.is_symlink() or not p.is_file() for p in paths):
        raise ValueError("regular module locks required")
    before = {p: p.read_bytes() for p in paths}
    archives, metadata = locked_inputs(before[paths[1]])
    # Download only archives already carrying a full checksum. `download all`
    # expands dependency test graphs to archives outside this admitted lock.
    subprocess.run(["go", "mod", "download", *archives], cwd=root, check=True)
    # Historical go.mod-only locks need metadata, never an unpinned ZIP. Go's
    # module query verifies the existing go.mod checksum without importing code.
    for target in metadata:
        subprocess.run(["go", "list", "-m", "-mod=readonly", "-json", target],
                       cwd=root, check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["go", "mod", "verify"], cwd=root, check=True)
    if any(p.read_bytes() != content for p, content in before.items()):
        raise ValueError("dependency preparation changed module locks")
    print("Locked offline fixture inputs prepared:", len(archives), "archives,",
          len(metadata), "metadata-only modules; locks unchanged")


if __name__ == "__main__":
    prepare(Path(__file__).resolve().parent.parent)
