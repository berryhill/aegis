#!/usr/bin/env python3
"""Remove only group/other write from the exact hosted setup-go distribution.

Never run against a workstation toolchain. All traversal/chmod is descriptor-relative
and no-follow; the complete inventory is validated before any permissions change.
"""
import argparse
import os
from pathlib import Path
import re
import stat


def prepare(root, cache, version, arch):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("exact Go version required")
    architectures = {"X64": "x64", "ARM64": "arm64"}
    if arch not in architectures:
        raise ValueError("unsupported hosted architecture")
    root, cache = Path(root), Path(cache)
    expected = cache / "go" / version / architectures[arch]
    if not cache.is_absolute() or not root.is_absolute() or root != expected:
        raise ValueError("GOROOT is not the exact setup-go cache distribution")
    for path in (cache, root):
        if path.resolve(strict=True) != path or any(p.is_symlink() for p in (path, *path.parents)):
            raise ValueError("symlink or path drift")
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC
    inventory = []

    def walk(fd, parts=()):
        info = os.fstat(fd)
        if info.st_uid != os.geteuid() or not (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)):
            raise ValueError("distribution must contain only job-owned regular files/directories")
        inventory.append((parts, info))
        if stat.S_ISDIR(info.st_mode):
            for name in sorted(os.listdir(fd)):
                before = os.stat(name, dir_fd=fd, follow_symlinks=False)
                if not (stat.S_ISDIR(before.st_mode) or stat.S_ISREG(before.st_mode)) or before.st_nlink > 1 and stat.S_ISREG(before.st_mode):
                    raise ValueError("symlink, special file or hardlink in distribution")
                child = os.open(name, flags | (os.O_DIRECTORY if stat.S_ISDIR(before.st_mode) else 0), dir_fd=fd)
                after = os.fstat(child)
                if (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
                    os.close(child)
                    raise ValueError("distribution path drift")
                try:
                    walk(child, parts + (name,))
                finally:
                    os.close(child)

    root_fd = os.open(root, flags | os.O_DIRECTORY)
    try:
        walk(root_fd)
        paths = {parts for parts, _ in inventory}
        if not all(tuple(p.split("/")) in paths for p in ("bin/go", "pkg/tool", "src", "VERSION")):
            raise ValueError("incomplete Go distribution")
        for parts, before in inventory:
            bound, named = os.fstat(root_fd), os.stat(root, follow_symlinks=False)
            if root.resolve(strict=True) != root or (named.st_dev, named.st_ino) != (bound.st_dev, bound.st_ino):
                raise ValueError("GOROOT path drift")
            fd = os.dup(root_fd)
            try:
                for name in parts:
                    child = os.open(name, flags, dir_fd=fd)
                    os.close(fd)
                    fd = child
                current = os.fstat(fd)
                if (current.st_dev, current.st_ino, current.st_mode) != (before.st_dev, before.st_ino, before.st_mode):
                    raise ValueError("distribution changed during preparation")
                os.fchmod(fd, stat.S_IMODE(before.st_mode) & ~0o022)
            finally:
                os.close(fd)
    finally:
        os.close(root_fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--goroot", required=True)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
        parser.error("only ephemeral GitHub-hosted setup-go installations are authorized")
    prepare(args.goroot, os.environ["RUNNER_TOOL_CACHE"], args.version, os.environ["RUNNER_ARCH"])


if __name__ == "__main__":
    main()
