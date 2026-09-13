#!/bin/sh
# Bind Git subprocesses to this source tree, then verify Go's own VCS stamp.
# Go versions whose root discovery ignores worktree .git files can otherwise
# stamp an enclosing repository. This does not synthesize build metadata.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
output=${1:-$repo/aegis}
if [ "$#" -gt 0 ]; then shift; fi
case "$output" in /*) ;; *) output=$PWD/$output ;; esac

deny() { printf 'source build provenance denied: %s\n' "$*" >&2; exit 2; }
# Discovery must not inherit another checkout's index or repository binding.
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE
[ -e "$repo/.git" ] || deny 'repository root has no Git marker'
[ "$(git -C "$repo" rev-parse --show-toplevel)" = "$repo" ] || deny 'repository root mismatch'
GIT_DIR=$(git -C "$repo" rev-parse --absolute-git-dir)
GIT_WORK_TREE=$repo
export GIT_DIR GIT_WORK_TREE
cd "$repo"
revision=$(git rev-parse --verify 'HEAD^{commit}')
commit_time=$(git show -s --format=%ct HEAD)
modified=false
[ -z "$(git status --porcelain)" ] || modified=true
stage=$(mktemp -d "$repo/.aegis-build-XXXXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
# The fixed package/output/stamping arguments cannot be overridden by callers.
go build "$@" -buildvcs=true -o "$stage/aegis" ./cmd/aegis
go version -m "$stage/aegis" > "$stage/metadata"
python3 - "$stage/metadata" "$revision" "$commit_time" "$modified" <<'PY'
import datetime
import pathlib
import sys
metadata, revision, timestamp, modified = sys.argv[1:]
settings = {}
for line in pathlib.Path(metadata).read_text().splitlines():
    fields = line.strip().split('\t')
    if len(fields) == 2 and fields[0] == 'build' and '=' in fields[1]:
        key, value = fields[1].split('=', 1)
        settings[key] = value
expected = {
    'vcs': 'git',
    'vcs.revision': revision,
    'vcs.time': datetime.datetime.fromtimestamp(int(timestamp), datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
    'vcs.modified': modified,
}
for key, value in expected.items():
    if settings.get(key) != value:
        sys.exit(f'source build provenance denied: {key} expected {value}, got {settings.get(key, "missing")}')
PY
[ "$(git rev-parse HEAD)" = "$revision" ] || deny 'HEAD changed during build'
# Publish the binary only after verification. Release clean-source guards are
# deliberately separate: truthful dirty development builds remain possible.
mv "$stage/aegis" "$output"
