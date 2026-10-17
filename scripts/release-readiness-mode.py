#!/usr/bin/env python3
"""Classify an exact clean snapshot; prepared mode is not an empty-changelog bypass."""
import datetime
import importlib.util
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from typing import NoReturn

SEMVER = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"


def deny(message: str) -> NoReturn:
    raise SystemExit("release readiness denied: " + message)


def sections(text):
    headings = re.findall(r"^## .*", text, re.M)
    if not headings or headings[0] != "## Unreleased" or text.count("## Unreleased") != 1:
        deny("CHANGELOG.md must contain exactly one canonical first Unreleased heading")
    versions = []
    for heading in headings[1:]:
        match = re.fullmatch(r"## \[(" + SEMVER + r")\] - (\d{4}-\d{2}-\d{2})", heading)
        if not match:
            deny("CHANGELOG.md has a malformed release heading")
        try:
            datetime.date.fromisoformat(match[2])
        except ValueError:
            deny("CHANGELOG.md has an invalid release date")
        versions.append(match[1])
    if len(versions) != len(set(versions)):
        deny("CHANGELOG.md has duplicate release headings")
    before, after = text.split("## Unreleased", 1)
    boundary = after.find("\n## ")
    return before, after if boundary < 0 else after[:boundary], "" if boundary < 0 else after[boundary:]


def main():
    repo, mode, version, tag = sys.argv[1:]
    repo = Path(repo)
    if mode not in ("prepare", "prepared", "auto"):
        deny("unknown readiness mode")
    if tag:
        if mode != "auto" or not re.fullmatch("v" + SEMVER, tag):
            deny("release tag must be vMAJOR.MINOR.PATCH in auto mode")
        version, mode = tag[1:], "prepared"
    text = (repo / "CHANGELOG.md").read_text()
    before, pending, tail = sections(text)
    if mode == "auto":
        mode = "prepare" if pending.strip() else "prepared"
        if mode == "prepared" and tail:
            version = tail.splitlines()[1].split("[", 1)[1].split("]", 1)[0]
    if mode == "prepare":
        if not re.search(r"^- \S", pending, re.M):
            deny("CHANGELOG.md has no unreleased entries")
    else:
        if pending.strip():
            deny("prepared changelog must have empty Unreleased")
        if not tail or not tail.startswith("\n## [" + version + "] - "):
            deny("prepared version mismatch in CHANGELOG.md")
        heading = tail.splitlines()[1]
        release_date = heading.rsplit(" - ", 1)[1]
        release_body = tail.split("\n## ", 2)[1]
        if not re.search(r"^- \S", release_body, re.M):
            deny("CHANGELOG.md prepared release has no entries")

        def git(*args):
            return subprocess.check_output(["git", "-C", str(repo), *args], text=True).rstrip("\n")

        parents = git("show", "-s", "--format=%P", "HEAD").split()
        if len(parents) != 1:
            deny("prepared commit must have one available parent")
        if git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD") != "CHANGELOG.md":
            deny("prepared commit must change only CHANGELOG.md")
        if git("diff", "--summary", parents[0], "HEAD"):
            deny("prepared commit must not change file modes")
        if release_date != git("show", "-s", "--format=%cs", "HEAD"):
            deny("prepared release date does not match commit")
        parent = subprocess.check_output(
            ["git", "-C", str(repo), "show", parents[0] + ":CHANGELOG.md"], text=True
        )
        source = text.replace("\n\n" + heading, "", 1)
        pb, pp, pt = sections(parent)
        sb, sp, st = sections(source)
        remaining = iter(sp.splitlines())
        if pb != sb or pt != st or not all(any(candidate == line for candidate in remaining) for line in pp.splitlines()):
            deny("prepared CHANGELOG.md does not preserve parent outside additive Unreleased entries")
        spec = importlib.util.spec_from_file_location("prepare_changelog", repo / "scripts/prepare-release-changelog.py")
        assert spec is not None and spec.loader is not None
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory(dir=repo.parent) as scratch:
            source_path, output = Path(scratch) / "source", Path(scratch) / "output"
            source_path.write_text(source)
            module.prepare(source_path, output, version, release_date)
            if output.read_text() != text:
                deny("prepared CHANGELOG.md transformation mismatch")
    print(mode, version)


if __name__ == "__main__":
    main()
