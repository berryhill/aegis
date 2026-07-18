#!/bin/sh
set -eu

version=${1:-}
tag=v$version

case "$version" in
    0|*[!0-9.]*) printf 'VERSION must be MAJOR.MINOR.PATCH\n' >&2; exit 2 ;;
esac
if ! printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
    printf 'VERSION must be MAJOR.MINOR.PATCH\n' >&2
    exit 2
fi
if [ "$(git branch --show-current)" != main ]; then
    printf 'release must run from main\n' >&2
    exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
    printf 'release requires a clean worktree; commit the release automation first\n' >&2
    exit 1
fi
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    printf '%s already exists locally\n' "$tag" >&2
    exit 1
fi
if [ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]; then
    printf '%s already exists on origin\n' "$tag" >&2
    exit 1
fi
remote_main=$(git ls-remote --heads origin main | cut -f1)
if [ -z "$remote_main" ] || [ "$remote_main" != "$(git rev-parse HEAD)" ]; then
    printf 'HEAD must already match origin/main before release preparation\n' >&2
    exit 1
fi

release_date=$(date +%F)
VERSION=$version RELEASE_DATE=$release_date python3 -c '
import os
from pathlib import Path

path = Path("CHANGELOG.md")
text = path.read_text()
version = os.environ["VERSION"]
date = os.environ["RELEASE_DATE"]
heading = f"## [{version}] - {date}"
marker = "## Unreleased"
if text.count(marker) != 1:
    raise SystemExit("CHANGELOG.md must contain exactly one Unreleased heading")
before, after = text.split(marker, 1)
next_release = after.find("\n## ")
pending = after if next_release < 0 else after[:next_release]
existing = [line for line in text.splitlines() if line.startswith(f"## [{version}] - ")]
if existing:
    if len(existing) != 1:
        raise SystemExit(f"CHANGELOG.md contains duplicate {version} release headings")
    if pending.strip():
        raise SystemExit(f"CHANGELOG.md has unreleased entries above existing {version} release")
else:
    if not pending.strip():
        raise SystemExit("CHANGELOG.md has no unreleased entries")
    text = before + marker + "\n\n" + heading + after
    path.write_text(text)
'

committed=false
cleanup() {
    if [ "$committed" = false ]; then
        git restore -- CHANGELOG.md
    fi
}
trap cleanup EXIT HUP INT TERM

make verify
make release-review VERSION="$version"
git diff --check
git diff -- CHANGELOG.md

if [ "${RELEASE_DRY_RUN:-0}" = 1 ]; then
    printf '%s dry run passed; no commit, tag, or push was performed.\n' "$tag"
    exit 0
fi

printf 'Type %s to commit, tag, and publish this release: ' "$tag" >/dev/tty
IFS= read -r confirmation </dev/tty
if [ "$confirmation" != "$tag" ]; then
    printf 'release cancelled\n' >&2
    exit 1
fi

if ! git diff --quiet -- CHANGELOG.md; then
    git add CHANGELOG.md
    git commit -m "Prepare $tag release"
fi
committed=true
git tag -s "$tag" -m "Aegis $tag"
git push --atomic origin main "$tag"

printf '%s pushed; GitHub Actions will publish the release after verification.\n' "$tag"
